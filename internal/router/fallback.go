package router

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/free-model-gateway/fmg/internal/model"
	"github.com/free-model-gateway/fmg/internal/proxy"
)

// Route 是 FMG 的**核心路由循环**：在最多 maxRetries+1 次尝试中，
// 依次按策略选 backend、调 forwarder、按错误类型决定继续/终止/重试。
//
// ## 错误分类与重试语义（这是整段代码最重要的设计决策）
//
// 我们把上游错误分成 3 类，每类动作不同：
//
// ┌─────────────┬──────────────┬──────────────────────┬──────────────────────────┐
// │ 类别        │ 状态码       │ 动作                 │ 理由                     │
// ├─────────────┼──────────────┼──────────────────────┼──────────────────────────┤
// │ client      │ 4xx (除 429) │ 立即返回，不重试     │ 用户请求本身有问题，      │
// │ error       │              │ 标 MarkFailure       │ 换 backend 也是同样的错  │
// ├─────────────┼──────────────┼──────────────────────┼──────────────────────────┤
// │ rate limit  │ 429          │ EnterCooldown, 重试  │ 限流是 provider 端问题，  │
// │             │              │ 下个 backend         │ 换 backend 可解           │
// ├─────────────┼──────────────┼──────────────────────┼──────────────────────────┤
// │ upstream    │ 5xx, 网络    │ MarkFailure 累计，   │ 单次 5xx 可能是抖动，     │
// │ error       │ 错误         │ consec≥3 → cooldown  │ 但连续 3 次说明真坏了     │
// └─────────────┴──────────────┴──────────────────────┴──────────────────────────┘
//
// ## 不变量
//
//   - tried[] 累积已试 backend，**确保不重复选同一个**（避免在 cooldown 边界
//     反复撞同一个刚恢复的 backend）
//   - chain[] 累积链路，供 metadata.fallback_chain 返回
//   - ctx.Done() 期间重试 sleep 可立即跳出（不浪费 r.retryDelay）
func (r *Router) Route(ctx context.Context, req *Request) (*Result, error) {
	tried := make([]*model.BackendModel, 0, r.maxRetries+1)
	chain := make([]string, 0, r.maxRetries+1)

	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		// 每次重试都重新拉一次 candidates —— 上一轮可能触发了某个 backend 的
		// cooldown，本次循环会自然避开它
		candidates := r.pool.Available()
		if len(candidates) == 0 {
			break
		}

		// 过滤掉已经试过的 backend。包含 cooldown 中的也算"已试"，
		// 因为我们这一轮的目标是"换 provider"。
		filtered := make([]*model.BackendModel, 0, len(candidates))
		for _, c := range candidates {
			if !containsModel(tried, c) {
				filtered = append(filtered, c)
			}
		}
		if len(filtered) == 0 {
			break
		}

		var selected *model.BackendModel
		forcedKey := r.ForcedModelKey()
		if forcedKey != "" {
			for _, c := range filtered {
				if c.Key() == forcedKey {
					selected = c
					break
				}
			}
		}
		if selected == nil {
			var selErr error
			selected, selErr = r.strategy.Select(ctx, filtered, req)
			if selErr != nil {
				if errors.Is(selErr, ErrNoCandidate) {
					break
				}
				break
			}
		}
		tried = append(tried, selected)
		chain = append(chain, selected.ProviderName+"/"+selected.ModelID)

		start := time.Now()
		result, err := r.forwarder.Forward(ctx, selected, req.RawBody)
		latency := time.Since(start)
		if err == nil && result != nil {
			// 成功路径。FallbackChain 不含自己（语义是"我从哪 fallback 过来的"）
			r.RecordLastUsed(selected.ProviderID, selected.ModelID, selected.ModelName)
			var inTok, outTok int64
			if result.Usage != nil {
				inTok = int64(result.Usage.PromptTokens)
				outTok = int64(result.Usage.CompletionTokens)
			}
			r.stats.Record(selected.ProviderID, selected.ModelID, true, inTok, outTok, latency.Milliseconds(), "")
			return &Result{
				Success:       true,
				Response:      result.Body,
				Model:         selected,
				FallbackChain: chain[:len(chain)-1],
				Retries:       attempt,
				Latency:       latency,
				Usage:         result.Usage,
			}, nil
		}

		// 错误路径：先解析错误结构。
		// proxy.HTTPError 是有 status code 的"语义错误"，
		// 其他 err 是网络/DNS/超时等"系统错误"（status=0）
		herr, isHTTP := err.(*proxy.HTTPError)
		status := 0
		errMsg := ""
		if isHTTP {
			status = herr.StatusCode
			errMsg = herr.Msg
		} else if err != nil {
			errMsg = err.Error()
		}

		// [分支 1] 4xx 客户端错误：重试无意义，立即终止。
		// 标记 MarkFailure 是为了让该 backend 的 stats 反映"曾被拒绝"
		if proxy.IsClientError(status) {
			selected.MarkFailure(fmt.Sprintf("client_error_%d: %s", status, errMsg))
			r.stats.Record(selected.ProviderID, selected.ModelID, false, 0, 0, latency.Milliseconds(), errMsg)
			return &Result{
				Success:       false,
				Model:         selected,
				FallbackChain: chain,
				Retries:       attempt,
				Error:         fmt.Errorf("upstream client error %d: %s", status, errMsg),
				ErrorStatus:   status,
			}, nil
		}

		// [分支 2/3] 可重试错误：429 / 5xx / 网络错误
		// 429 → 立即冷却（上游明确在限流，再试一次也是 429）
		// 其他 → MarkFailure 累计，consec≥3 才冷却（容忍瞬时抖动）
		if status == http.StatusTooManyRequests {
			r.cooldownMgr.EnterCooldown(selected, "429 rate limited")
		} else {
			selected.MarkFailure(fmt.Sprintf("upstream_%d: %s", status, errMsg))
			if selected.ConsecErrors >= 3 {
				r.cooldownMgr.EnterCooldown(selected, "consecutive errors")
			}
		}
		r.stats.Record(selected.ProviderID, selected.ModelID, false, 0, 0, latency.Milliseconds(), errMsg)

		// 退避：等到 retryDelay 或 ctx 取消
		if attempt < r.maxRetries {
			select {
			case <-time.After(r.retryDelay):
			case <-ctx.Done():
				r.stats.Record(selected.ProviderID, selected.ModelID, false, 0, 0, latency.Milliseconds(), ctx.Err().Error())
				return &Result{Success: false, Error: ctx.Err()}, nil
			}
		}
	}

	// 所有 backend 都失败：返回 503（按 NFR，客户端应理解为"gateway 临时无法服务"）
	return &Result{
		Success:       false,
		FallbackChain: chain,
		Retries:       len(tried) - 1,
		Error:         fmt.Errorf("all %d backends failed after %d retries", len(tried), len(tried)),
		ErrorStatus:   http.StatusServiceUnavailable,
	}, nil
}

// containsModel 是 O(n) 线性查找。tried 长度上限是 maxRetries+1（默认 4），
// 性能上可以接受；如果未来 maxRetries 上调到几十，可换成 map[Key]struct{}。
func containsModel(list []*model.BackendModel, target *model.BackendModel) bool {
	for _, m := range list {
		if m.Key() == target.Key() {
			return true
		}
	}
	return false
}

// RouteStream 是流式版路由循环，与 Route 的区别：
//   - ForwardStream 接管 http.ResponseWriter，**第一次 WriteHeader 之后**才能
//     真正向客户端写状态码，所以失败回退只能"放弃当前 backend，假装这个 stream
//     还没开始"，下一个 backend 重新写 200
//   - 流式不能把上游错误"塞进 body 后再回 500"给客户端（chunked 已发出 header
//     就无法改 status），所以 stream 失败的语义是"整个 stream 失败，client 看到
//     是 502 Bad Gateway"（外部由 gin middleware 处理）
//   - 不会把每个 backend 都尝试一遍 —— 第一个成功的就保留；失败原因记录在 Result
func (r *Router) RouteStream(ctx context.Context, req *Request, w http.ResponseWriter, flusher proxy.Flusher) (*Result, error) {
	tried := make([]*model.BackendModel, 0, r.maxRetries+1)
	chain := make([]string, 0, r.maxRetries+1)

	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		candidates := r.pool.Available()
		if len(candidates) == 0 {
			return nil, ErrNoCandidate
		}

		filtered := make([]*model.BackendModel, 0, len(candidates))
		for _, c := range candidates {
			if !containsModel(tried, c) {
				filtered = append(filtered, c)
			}
		}
		if len(filtered) == 0 {
			return nil, ErrNoCandidate
		}

		var sel *model.BackendModel
		forcedKey := r.ForcedModelKey()
		if forcedKey != "" {
			for _, c := range filtered {
				if c.Key() == forcedKey {
					sel = c
					break
				}
			}
		}
		if sel == nil {
			var selErr error
			sel, selErr = r.strategy.Select(ctx, filtered, req)
			if selErr != nil {
				return nil, selErr
			}
		}
		selected := sel
		tried = append(tried, selected)
		chain = append(chain, selected.ProviderName+"/"+selected.ModelID)

		start := time.Now()
		streamErr := r.forwarder.ForwardStream(ctx, selected, req.RawBody, w, flusher)
		latency := time.Since(start)
		if streamErr == nil {
			r.RecordLastUsed(selected.ProviderID, selected.ModelID, selected.ModelName)
			r.stats.Record(selected.ProviderID, selected.ModelID, true, 0, 0, latency.Milliseconds(), "")
			return &Result{
				Success:       true,
				Model:         selected,
				FallbackChain: chain[:len(chain)-1],
				Retries:       attempt,
				Latency:       latency,
			}, nil
		}

		herr, isHTTP := streamErr.(*proxy.HTTPError)
		status := 0
		if isHTTP {
			status = herr.StatusCode
		}

		// [分支 1] 流式 4xx 同样不可重试（直接终止，让 gin 框架返回 502）
		if proxy.IsClientError(status) {
			selected.MarkFailure("client error during stream")
			r.stats.Record(selected.ProviderID, selected.ModelID, false, 0, 0, latency.Milliseconds(), streamErr.Error())
			return &Result{
				Success:       false,
				Model:         selected,
				FallbackChain: chain,
				Retries:       attempt,
				Error:         streamErr,
				ErrorStatus:   status,
			}, nil
		}

		// [分支 2/3] 冷却逻辑同 Route
		if status == http.StatusTooManyRequests {
			r.cooldownMgr.EnterCooldown(selected, "429 rate limited")
		} else {
			selected.MarkFailure(streamErr.Error())
			if selected.ConsecErrors >= 3 {
				r.cooldownMgr.EnterCooldown(selected, "consecutive errors")
			}
		}
		r.stats.Record(selected.ProviderID, selected.ModelID, false, 0, 0, latency.Milliseconds(), streamErr.Error())

		if attempt < r.maxRetries {
			select {
			case <-time.After(r.retryDelay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}

	// 流式全部失败 —— StatusBadGateway (502) 是"上游/网关失败"的标准码，
	// 配合 /admin/stats 的 "stream_failed_count" 字段定位问题
	return &Result{
		Success:       false,
		FallbackChain: chain,
		Retries:       len(tried) - 1,
		Error:         fmt.Errorf("stream failed after %d attempts", len(tried)),
		ErrorStatus:   http.StatusBadGateway,
	}, nil
}

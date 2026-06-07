package router

import (
	"context"
	stderrors "errors"
	"fmt"
	"net/http"
	"time"

	"github.com/free-model-gateway/fmg/internal/errors"
	"github.com/free-model-gateway/fmg/internal/model"
	"github.com/free-model-gateway/fmg/internal/provider"
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
	if req != nil && req.Model != "" && req.Model != "auto" {
		found := false
		for _, m := range r.pool.All() {
			if m.ModelID == req.Model {
				found = true
				break
			}
		}
		if !found {
			return &Result{
				Success:     false,
				Error:       fmt.Errorf("model not found: %s", req.Model),
				ErrorStatus: http.StatusNotFound,
			}, nil
		}
	}

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
		filtered := r.filterAvailable(candidates, tried)
		if len(filtered) == 0 {
			break
		}

		var selected *model.BackendModel
		forcedKey := r.ForcedModelKey()
		isManual := false
		if forcedKey != "" {
			for _, c := range filtered {
				if c.Key() == forcedKey {
					selected = c
					isManual = true
					break
				}
			}
			if selected == nil {
				return &Result{
					Success:     false,
					Error:       fmt.Errorf("manual model %s not available", forcedKey),
					ErrorStatus: http.StatusServiceUnavailable,
				}, nil
			}
		}
		if selected == nil {
			var selErr error
			selected, selErr = r.strategy.Select(ctx, filtered, req)
			if selErr != nil {
				if stderrors.Is(selErr, ErrNoCandidate) {
					break
				}
				break
			}
		}

		// Sticky session: check if we have a preferred model for this conversation
		if !isManual && r.sticky != nil && len(req.Messages) > 0 {
			stickyKey := sessionKey(req.Messages)
			if stickyKey != "" {
				preferred := r.sticky.Get(stickyKey)
				if preferred != "" && preferred != selected.Key() {
					// Try to find the sticky model in candidates
					for _, c := range filtered {
						if c.Key() == preferred && !containsModel(tried, c) {
							selected = c
							break
						}
					}
				}
			}
		}

		// Rate limit check
		if r.limiter != nil {
			if !r.limiter.AllowRequest(selected.ProviderID, selected.ModelID, 0, 0) {
				selected.MarkFailure("rate limit exceeded")
				if isManual {
					return &Result{
						Success:     false,
						Model:       selected,
						Error:       fmt.Errorf("manual model %s rate limited", selected.Key()),
						ErrorStatus: http.StatusTooManyRequests,
					}, nil
				}
				continue
			}
		}

		tried = append(tried, selected)
		chain = append(chain, selected.ProviderName+"/"+selected.ModelID)

		start := time.Now()
		result, err := r.forwarder.Forward(ctx, selected, req.RawBody)
		latency := time.Since(start)
		if err == nil && result != nil {
			// Success path
			r.RecordLastUsed(selected.ProviderID, selected.ModelID, selected.ModelName)
			// If we fell back to a different model than the forced one, clear the forced model
			// so the dashboard doesn't show an invalid/unavailable model as "in use".
			forcedKey := r.ForcedModelKey()
			if forcedKey != "" && forcedKey != selected.Key() {
				r.ClearForcedModel()
			}
			// Set sticky session for multi-turn conversations
			if r.sticky != nil && len(req.Messages) > 2 {
				stickyKey := sessionKey(req.Messages)
				if stickyKey != "" {
					r.sticky.Set(stickyKey, selected.Key())
				}
			}
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

		// 错误路径：用统一错误分类解析
		herr, isHTTP := err.(*provider.HTTPError)
		status := 0
		if isHTTP {
			status = herr.StatusCode
		}
		pe := errors.ParseProviderError(err, status, nil)
		if pe == nil {
			pe = errors.ParseProviderError(err, 0, nil)
		}

		if isManual {
			if pe.MarkKeyInvalid {
				selected.MarkKeyInvalid(fmt.Sprintf("invalid_key_%d: %s", pe.Code, pe.Message))
			} else {
				selected.MarkFailure(fmt.Sprintf("manual_%d: %s", pe.Code, pe.Message))
			}
			r.stats.Record(selected.ProviderID, selected.ModelID, false, 0, 0, latency.Milliseconds(), pe.Message)
			return &Result{
				Success:     false,
				Model:       selected,
				Error:       pe,
				ErrorStatus: pe.Code,
			}, nil
		}

		shouldContinue := r.handleError(selected, pe, latency)
		if !shouldContinue {
			return &Result{
				Success:       false,
				Model:         selected,
				FallbackChain: chain,
				Retries:       attempt,
				Error:         pe,
				ErrorStatus:   pe.Code,
			}, nil
		}

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

// SelectBackend selects the next available backend using strategy, sticky sessions,
// rate limiting, and health checks. The tried slice prevents re-selecting the same backend.
// It returns the selected backend or an error if no backend is available.
func (r *Router) SelectBackend(ctx context.Context, req *Request, tried []*model.BackendModel) (*model.BackendModel, error) {
	candidates := r.pool.Available()
	if len(candidates) == 0 {
		return nil, ErrNoCandidate
	}

	filtered := r.filterAvailable(candidates, tried)
	if len(filtered) == 0 {
		return nil, ErrNoCandidate
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
			return nil, selErr
		}
	}

	if r.sticky != nil && len(req.Messages) > 0 {
		stickyKey := sessionKey(req.Messages)
		if stickyKey != "" {
			preferred := r.sticky.Get(stickyKey)
			if preferred != "" && preferred != selected.Key() {
				for _, c := range filtered {
					if c.Key() == preferred && !containsModel(tried, c) {
						selected = c
						break
					}
				}
			}
		}
	}

	if r.limiter != nil {
		if !r.limiter.AllowRequest(selected.ProviderID, selected.ModelID, 0, 0) {
			selected.MarkFailure("rate limit exceeded")
			return selected, fmt.Errorf("rate limit exceeded")
		}
	}

	return selected, nil
}

// filterAvailable skips providers that are in provider-level cooldown due to 429s.
func (r *Router) filterAvailable(candidates []*model.BackendModel, tried []*model.BackendModel) []*model.BackendModel {
	filtered := make([]*model.BackendModel, 0, len(candidates))
	for _, c := range candidates {
		if containsModel(tried, c) {
			continue
		}
		if r.healthTracker != nil && r.healthTracker.ShouldSkip(c.ProviderID) {
			continue
		}
		filtered = append(filtered, c)
	}
	return filtered
}

// handleError 根据 ProviderError 统一处理错误，返回是否需要继续重试。
func (r *Router) handleError(selected *model.BackendModel, pe *errors.ProviderError, latency time.Duration) (shouldContinue bool) {
	errMsg := pe.Message

	r.log.WithFields(map[string]interface{}{
		"code":             pe.Code,
		"retryable":        pe.Retryable,
		"mark_key_invalid": pe.MarkKeyInvalid,
		"message":          errMsg,
	}).Debug("handleError called")

	if !pe.Retryable {
		if pe.MarkKeyInvalid {
			selected.MarkKeyInvalid(fmt.Sprintf("invalid_key_%d: %s", pe.Code, errMsg))
			// If the invalid model is the forced or last-used one, clear it so
			// the dashboard no longer shows it as "in use".
			if r.ForcedModelKey() == selected.Key() {
				r.ClearForcedModel()
			}
			lp, lm, _ := r.LastUsedModel()
			if lp == selected.ProviderID && lm == selected.ModelID {
				r.ClearLastUsed()
			}
		} else {
			selected.MarkFailure(fmt.Sprintf("non_retryable_%d: %s", pe.Code, errMsg))
		}
		r.stats.Record(selected.ProviderID, selected.ModelID, false, 0, 0, latency.Milliseconds(), errMsg)
		return false
	}

	// 429 → 分级冷却 + provider-level 追踪
	if pe.Code == http.StatusTooManyRequests {
		cooldown := pe.Cooldown
		reason := fmt.Sprintf("429 %s", pe.RateLimitType.String())
		if cooldown > 0 {
			r.cooldownMgr.EnterCooldownWithDuration(selected, cooldown, reason)
		} else {
			r.cooldownMgr.EnterCooldown(selected, reason)
		}
		// Provider-level tracking for provider-wide / daily quota 429s
		if r.healthTracker != nil && (pe.SkipProvider || pe.RateLimitType == errors.RateLimitProviderWide ||
			pe.RateLimitType == errors.RateLimitPerDay || pe.RateLimitType == errors.RateLimitPerDayTokens) {
			r.healthTracker.Record429(selected.ProviderID)
		}
	} else if pe.SkipProvider {
		// 402 or network errors that require skipping provider
		selected.MarkFailure(fmt.Sprintf("upstream_%d: %s", pe.Code, errMsg))
		if selected.ConsecErrors >= 3 {
			r.cooldownMgr.EnterCooldown(selected, "consecutive errors")
		}
		if r.healthTracker != nil {
			r.healthTracker.Record429(selected.ProviderID)
		}
	} else {
		// Standard retryable errors (5xx, timeout, etc.)
		selected.MarkFailure(fmt.Sprintf("upstream_%d: %s", pe.Code, errMsg))
		if selected.ConsecErrors >= 3 {
			r.cooldownMgr.EnterCooldown(selected, "consecutive errors")
		}
	}

	r.stats.Record(selected.ProviderID, selected.ModelID, false, 0, 0, latency.Milliseconds(), errMsg)
	return true
}

// RouteStream 是流式版路由循环，与 Route 的区别：
//   - ForwardStream 接管 http.ResponseWriter，**第一次 WriteHeader 之后**才能
//     真正向客户端写状态码，所以失败回退只能"放弃当前 backend，假装这个 stream
//     还没开始"，下一个 backend 重新写 200
//   - 流式不能把上游错误"塞进 body 后再回 500"给客户端（chunked 已发出 header
//     就无法改 status），所以 stream 失败的语义是"整个 stream 失败，client 看到
//     是 502 Bad Gateway"（外部由 gin middleware 处理）
//   - 不会把每个 backend 都尝试一遍 —— 第一个成功的就保留；失败原因记录在 Result
func (r *Router) RouteStream(ctx context.Context, req *Request, w http.ResponseWriter, flusher provider.Flusher) (*Result, error) {
	if req != nil && req.Model != "" && req.Model != "auto" {
		found := false
		for _, m := range r.pool.All() {
			if m.ModelID == req.Model {
				found = true
				break
			}
		}
		if !found {
			return &Result{
				Success:     false,
				Error:       fmt.Errorf("model not found: %s", req.Model),
				ErrorStatus: http.StatusNotFound,
			}, nil
		}
	}

	tried := make([]*model.BackendModel, 0, r.maxRetries+1)
	chain := make([]string, 0, r.maxRetries+1)

	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		candidates := r.pool.Available()
		if len(candidates) == 0 {
			return nil, ErrNoCandidate
		}

		filtered := r.filterAvailable(candidates, tried)
		if len(filtered) == 0 {
			return nil, ErrNoCandidate
		}

		var sel *model.BackendModel
		forcedKey := r.ForcedModelKey()
		isManual := false
		if forcedKey != "" {
			for _, c := range filtered {
				if c.Key() == forcedKey {
					sel = c
					isManual = true
					break
				}
			}
			if sel == nil {
				return &Result{
					Success:     false,
					Error:       fmt.Errorf("manual model %s not available", forcedKey),
					ErrorStatus: http.StatusServiceUnavailable,
				}, nil
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

		if !isManual && r.sticky != nil && len(req.Messages) > 0 {
			stickyKey := sessionKey(req.Messages)
			if stickyKey != "" {
				preferred := r.sticky.Get(stickyKey)
				if preferred != "" && preferred != selected.Key() {
					for _, c := range filtered {
						if c.Key() == preferred && !containsModel(tried, c) {
							selected = c
							break
						}
					}
				}
			}
		}

		if r.limiter != nil {
			if !r.limiter.AllowRequest(selected.ProviderID, selected.ModelID, 0, 0) {
				selected.MarkFailure("rate limit exceeded")
				if isManual {
					return &Result{
						Success:     false,
						Model:       selected,
						Error:       fmt.Errorf("manual model %s rate limited", selected.Key()),
						ErrorStatus: http.StatusTooManyRequests,
					}, nil
				}
				continue
			}
		}

		tried = append(tried, selected)
		chain = append(chain, selected.ProviderName+"/"+selected.ModelID)

		start := time.Now()
		streamResult, streamErr := r.forwarder.ForwardStream(ctx, selected, req.RawBody, w, flusher)
		latency := time.Since(start)
		if streamErr == nil {
			r.RecordLastUsed(selected.ProviderID, selected.ModelID, selected.ModelName)
			// Clear forced model if we fell back to a different backend.
			forcedKey := r.ForcedModelKey()
			if forcedKey != "" && forcedKey != selected.Key() {
				r.ClearForcedModel()
			}
			if r.sticky != nil && len(req.Messages) > 2 {
				stickyKey := sessionKey(req.Messages)
				if stickyKey != "" {
					r.sticky.Set(stickyKey, selected.Key())
				}
			}
			var inTok, outTok int64
			if streamResult != nil && streamResult.Usage != nil {
				inTok = int64(streamResult.Usage.PromptTokens)
				outTok = int64(streamResult.Usage.CompletionTokens)
			}
			r.stats.Record(selected.ProviderID, selected.ModelID, true, inTok, outTok, latency.Milliseconds(), "")
			return &Result{
				Success:       true,
				Model:         selected,
				FallbackChain: chain[:len(chain)-1],
				Retries:       attempt,
				Latency:       latency,
				Usage:         streamResult.Usage,
			}, nil
		}

		herr, isHTTP := streamErr.(*provider.HTTPError)
		status := 0
		if isHTTP {
			status = herr.StatusCode
		}

		pe := errors.ParseProviderError(streamErr, status, nil)
		if pe == nil {
			pe = errors.ParseProviderError(streamErr, 0, nil)
		}

		if isManual {
			if pe.MarkKeyInvalid {
				selected.MarkKeyInvalid(fmt.Sprintf("invalid_key_%d: %s", pe.Code, pe.Message))
			} else {
				selected.MarkFailure(fmt.Sprintf("manual_%d: %s", pe.Code, pe.Message))
			}
			r.stats.Record(selected.ProviderID, selected.ModelID, false, 0, 0, latency.Milliseconds(), pe.Message)
			return &Result{
				Success:     false,
				Model:       selected,
				Error:       pe,
				ErrorStatus: pe.Code,
			}, nil
		}

		if !pe.Retryable {
			if pe.MarkKeyInvalid {
				selected.MarkKeyInvalid(fmt.Sprintf("invalid_key_%d: %s", pe.Code, pe.Message))
				if r.ForcedModelKey() == selected.Key() {
					r.ClearForcedModel()
				}
				lp, lm, _ := r.LastUsedModel()
				if lp == selected.ProviderID && lm == selected.ModelID {
					r.ClearLastUsed()
				}
			} else {
				selected.MarkFailure(fmt.Sprintf("client_error_%d: %s", pe.Code, pe.Message))
			}
			r.stats.Record(selected.ProviderID, selected.ModelID, false, 0, 0, latency.Milliseconds(), pe.Message)
			return &Result{
				Success:       false,
				Model:         selected,
				FallbackChain: chain,
				Retries:       attempt,
				Error:         pe,
				ErrorStatus:   pe.Code,
			}, nil
		}

		_ = r.handleError(selected, pe, latency)

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

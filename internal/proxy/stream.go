package proxy

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/free-model-gateway/fmg/internal/model"
)

// Flusher 是 http.Flusher 的极简接口，便于测试时注入 mock。
// 真实 gin.ResponseWriter 自己实现了 http.Flusher，caller 传 nil 时
// ForwardStream 会通过 type-assert 拿到。
type Flusher interface {
	Flush()
}

// streamState 跟踪一个流式响应的内部状态。
// 当前实现里仅 hasData / finalized 真正被使用，其他字段为未来扩展预留（如
// 区分"已写出 header 但还没数据"vs"已写出数据但未 EOF"，便于把 stream
// 转为 SSE 事件而不是裸 chunk）。
type streamState struct {
	mu        sync.Mutex
	flushed   bool
	hasData   bool
	hadError  bool
	finalized bool
}

// ForwardStream 把 upstream 的 SSE 响应透传到下游 client。**流式语义**：
//   - 上游一有数据就 Flush 到客户端（不缓冲），降低 TTFT
//   - ctx 取消时立即中断 upstream 读取（http.Client.Do 会自动 cancel 请求）
//   - 错误路径已经"写出了 200 header"的，无法回退 —— 这是 SSE 与非流式的
//     关键区别（参见 router.fallback.go 的 RouteStream 注释）
//
// ## 并发模型（重要）
//
// 整个函数有一个 reader goroutine（持续读 upstream）+ 主函数（select 等待）。
// 协调用 `done` channel：goroutine 退出时 close(done)，主函数从 select 中
// 收到 done 信号后返回 nil。**不能用 ctx.Done 单独判断** —— 上游正常 EOF 时
// ctx 还没取消，但 reader goroutine 已退出，select 收 done 才是正确路径。
func (f *Forwarder) ForwardStream(ctx context.Context, backend *model.BackendModel, body []byte, w http.ResponseWriter, flusher Flusher) error {
	if flusher == nil {
		if rf, ok := w.(http.Flusher); ok {
			flusher = rf
		} else {
			return fmt.Errorf("response writer does not support flushing")
		}
	}

	// 把通用 body 改写成目标 backend 的 model_id 字段（gateway 透传 model="auto"）
	rewritten, err := RewriteRequestBody(body, backend.ModelID)
	if err != nil {
		return fmt.Errorf("rewrite body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, backend.BaseURL, bytes.NewReader(rewritten))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+backend.APIKey)
	req.Header.Set("User-Agent", "fmg/"+f.version)
	req.Header.Set("X-Forwarded-By", "fmg")
	for k, v := range backend.ExtraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := f.streamClient().Do(req)
	if err != nil {
		// 网络/DNS/超时错误 —— 没有 status code，包成 HTTPError{0}
		// 让上层 router.RouteStream 把它当作"系统错误"处理（按 consec 累计）
		return &HTTPError{StatusCode: 0, Msg: err.Error()}
	}
	defer resp.Body.Close()

	// 上游返回非 2xx：包成 HTTPError 让上层决定是否重试
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPError{StatusCode: resp.StatusCode, Msg: resp.Status}
	}

	// 关键：先锁住 SSE headers（Content-Type/Cache-Control/Connection）再 WriteHeader
	// 因为 gin.ResponseWriter 第一次 Write 后 header 就被冻结了
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	// X-Accel-Buffering=no 是 nginx 的 magic header，告诉 nginx 不要 buffer SSE
	// （很多用户会再前面加 nginx 反代）
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	state := &streamState{}
	// bufio 默认 4096 字节 buffer —— 够大降低 syscall 次数，又不会因为延迟
	// 而影响 SSE chunk 的及时性。注意我们用 Read(buf) 而不是 Scanner.Scanlines，
	// 因为 SSE 协议下 chunked transfer 边界不等于 SSE event 边界，按字节流读最简单
	reader := bufio.NewReader(resp.Body)

	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, rerr := reader.Read(buf)
			if n > 0 {
				state.mu.Lock()
				state.hasData = true
				state.mu.Unlock()
				// 写 + flush：每个 chunk 都立即推到客户端，TTFT 最低
				if _, werr := w.Write(buf[:n]); werr != nil {
					// 客户端断开（write 失败）也算 stream 正常结束
					return
				}
				flusher.Flush()
			}
			if rerr != nil {
				// io.EOF 或网络错误 —— 都视为 stream 终止
				return
			}
		}
	}()

	// 双路 select：ctx 取消（客户端主动断开）或 reader goroutine 自然结束（上游 EOF）
	select {
	case <-ctx.Done():
		// client 取消请求。http.NewRequestWithContext 绑定的 ctx 会让
		// 上游连接也自动 cancel（http.Transport 监听 ctx）
		return ctx.Err()
	case <-done:
		state.mu.Lock()
		state.finalized = true
		state.mu.Unlock()
		return nil
	}
}

// streamClient 返回流式 client。当前实现与普通 client 共用。
// 预留这个 wrapper 是为了未来可能要为流式单独配置超时/连接池（如禁用 keep-alive）。
func (f *Forwarder) streamClient() *http.Client {
	if c, ok := f.client.Transport.(*http.Transport); ok {
		_ = c
	}
	return f.client
}

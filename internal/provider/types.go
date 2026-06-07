package provider

import (
	"net/http"
	"time"
)

// ForwardResult is the outcome of a provider adapter forward call.
type ForwardResult struct {
	StatusCode int
	Body       []byte
	Headers    http.Header
	Latency    time.Duration
	Usage      *Usage
}

// Usage tracks token consumption.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// HTTPError wraps an upstream HTTP error with status code and body.
type HTTPError struct {
	StatusCode int
	Body       []byte
	Msg        string
}

func (e *HTTPError) Error() string {
	return e.Msg
}

// Flusher abstracts http.Flusher for SSE streaming.
type Flusher interface {
	Flush()
}

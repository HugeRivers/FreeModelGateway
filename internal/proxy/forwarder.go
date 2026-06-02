package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/free-model-gateway/fmg/internal/model"
	"github.com/sirupsen/logrus"
)

type ForwardResult struct {
	StatusCode int
	Body       []byte
	Headers    http.Header
	Latency    time.Duration
	Usage      *Usage
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type HTTPError struct {
	StatusCode int
	Body       []byte
	Msg        string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("upstream %d: %s", e.StatusCode, e.Msg)
}

type Forwarder struct {
	client  *http.Client
	log     *logrus.Logger
	version string
}

func NewForwarder(client *http.Client, log *logrus.Logger, version string) *Forwarder {
	return &Forwarder{client: client, log: log, version: version}
}

func (f *Forwarder) Forward(ctx context.Context, backend *model.BackendModel, body []byte) (*ForwardResult, error) {
	rewritten, err := RewriteRequestBody(body, backend.ModelID)
	if err != nil {
		return nil, fmt.Errorf("rewrite body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, backend.BaseURL, bytes.NewReader(rewritten))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+backend.APIKey)
	req.Header.Set("User-Agent", "fmg/"+f.version)
	req.Header.Set("X-Forwarded-By", "fmg")
	for k, v := range backend.ExtraHeaders {
		req.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := f.client.Do(req)
	latency := time.Since(start)
	if err != nil {
		return nil, fmt.Errorf("upstream call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read upstream body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &HTTPError{
			StatusCode: resp.StatusCode,
			Body:       respBody,
			Msg:        fmt.Sprintf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode)),
		}
	}

	result := &ForwardResult{
		StatusCode: resp.StatusCode,
		Body:       respBody,
		Headers:    resp.Header.Clone(),
		Latency:    latency,
	}
	if usage, ok := extractUsage(respBody); ok {
		result.Usage = usage
		backend.MarkSuccess(usage.PromptTokens, usage.CompletionTokens, latency)
	} else {
		backend.MarkSuccess(0, 0, latency)
	}
	return result, nil
}

func extractUsage(body []byte) (*Usage, bool) {
	var probe struct {
		Usage *Usage `json:"usage"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return nil, false
	}
	if probe.Usage == nil {
		return nil, false
	}
	return probe.Usage, true
}

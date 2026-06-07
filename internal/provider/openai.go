package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/free-model-gateway/fmg/internal/model"
)

// OpenAIAdapter forwards requests to OpenAI-compatible providers.
type OpenAIAdapter struct {
	client  *http.Client
	version string
}

func NewOpenAIAdapter(client *http.Client, version string) *OpenAIAdapter {
	return &OpenAIAdapter{client: client, version: version}
}

func (a *OpenAIAdapter) Forward(ctx context.Context, backend *model.BackendModel, body []byte) (*ForwardResult, error) {
	rewritten, err := rewriteModel(body, backend.ModelID)
	if err != nil {
		return nil, fmt.Errorf("rewrite body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, backend.BaseURL, bytes.NewReader(rewritten))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+backend.APIKey)
	req.Header.Set("User-Agent", "fmg/"+a.version)
	req.Header.Set("X-Forwarded-By", "fmg")
	for k, v := range backend.ExtraHeaders {
		req.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := a.client.Do(req)
	latency := time.Since(start)
	if err != nil {
		return nil, fmt.Errorf("call: %w", err)
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
	}
	return result, nil
}

func (a *OpenAIAdapter) ForwardStream(ctx context.Context, backend *model.BackendModel, body []byte, w http.ResponseWriter, flusher Flusher) (*ForwardResult, error) {
	rewritten, err := rewriteModel(body, backend.ModelID)
	if err != nil {
		return nil, fmt.Errorf("rewrite body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, backend.BaseURL, bytes.NewReader(rewritten))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+backend.APIKey)
	req.Header.Set("User-Agent", "fmg/"+a.version)
	req.Header.Set("X-Forwarded-By", "fmg")
	for k, v := range backend.ExtraHeaders {
		req.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, &HTTPError{StatusCode: 0, Msg: err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &HTTPError{StatusCode: resp.StatusCode, Msg: resp.Status}
	}

	result, err := copyStreamWithUsage(resp, w, flusher)
	if err != nil {
		return nil, err
	}
	result.Latency = time.Since(start)
	return result, nil
}

func (a *OpenAIAdapter) Probe(ctx context.Context, backend *model.BackendModel) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, backend.BaseURL, nil)
	if err != nil {
		return fmt.Errorf("probe build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+backend.APIKey)
	req.Header.Set("User-Agent", "fmg/"+a.version)
	for k, v := range backend.ExtraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("probe connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusMethodNotAllowed {
		return nil
	}
	if resp.StatusCode < 400 || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	return fmt.Errorf("probe returned status %d", resp.StatusCode)
}

func rewriteModel(body []byte, targetModel string) ([]byte, error) {
	if len(body) == 0 {
		return body, nil
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	raw["model"] = targetModel
	return json.Marshal(raw)
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

func extractUsageFromSSE(data []byte) (*Usage, bool) {
	lines := bytes.Split(data, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 || bytes.Equal(line, []byte("[DONE]")) {
			continue
		}
		if bytes.HasPrefix(line, []byte("data: ")) {
			payload := bytes.TrimPrefix(line, []byte("data: "))
			if usage, ok := extractUsage(payload); ok {
				return usage, true
			}
		}
	}
	return nil, false
}

func copyStreamWithUsage(resp *http.Response, w http.ResponseWriter, flusher Flusher) (*ForwardResult, error) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	reader := io.Reader(resp.Body)
	buf := make([]byte, 4096)
	var cached bytes.Buffer
	for {
		n, rerr := reader.Read(buf)
		if n > 0 {
			cached.Write(buf[:n])
			if _, werr := w.Write(buf[:n]); werr != nil {
				return nil, nil
			}
			flusher.Flush()
		}
		if rerr != nil {
			break
		}
	}

	result := &ForwardResult{}
	if usage, ok := extractUsageFromSSE(cached.Bytes()); ok {
		result.Usage = usage
	}
	return result, nil
}

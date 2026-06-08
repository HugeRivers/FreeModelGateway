package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/free-model-gateway/fmg/internal/model"
	"github.com/free-model-gateway/fmg/internal/protocol"
)

// OpenAIResponsesAdapter translates between OpenAI Chat Completions and OpenAI Responses API.
type OpenAIResponsesAdapter struct {
	client  *http.Client
	version string
}

func NewOpenAIResponsesAdapter(client *http.Client, version string) *OpenAIResponsesAdapter {
	return &OpenAIResponsesAdapter{client: client, version: version}
}

func resolveResponsesURL(baseURL string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(baseURL, "/responses") {
		return baseURL
	}
	return baseURL + "/responses"
}

func (a *OpenAIResponsesAdapter) Forward(ctx context.Context, backend *model.BackendModel, body []byte) (*ForwardResult, error) {
	respBody, err := openAIToResponsesRequest(body)
	if err != nil {
		return nil, fmt.Errorf("translate request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resolveResponsesURL(backend.BaseURL), bytes.NewReader(respBody))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+backend.APIKey)
	req.Header.Set("User-Agent", "fmg/"+a.version)
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

	respBodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read upstream body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &HTTPError{
			StatusCode: resp.StatusCode,
			Body:       respBodyBytes,
			Msg:        fmt.Sprintf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode)),
		}
	}

	openAIBody, err := responsesToOpenAIResponse(respBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("translate response: %w", err)
	}

	result := &ForwardResult{
		StatusCode: resp.StatusCode,
		Body:       openAIBody,
		Headers:    resp.Header.Clone(),
		Latency:    latency,
	}
	if usage, ok := extractUsage(openAIBody); ok {
		result.Usage = usage
	}
	return result, nil
}

func (a *OpenAIResponsesAdapter) ForwardStream(ctx context.Context, backend *model.BackendModel, body []byte, w http.ResponseWriter, flusher Flusher) (*ForwardResult, error) {
	rewritten, err := rewriteModel(body, backend.ModelID)
	if err != nil {
		return nil, fmt.Errorf("rewrite body: %w", err)
	}
	respBody, err := openAIToResponsesRequest(rewritten)
	if err != nil {
		return nil, fmt.Errorf("translate request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resolveResponsesURL(backend.BaseURL), bytes.NewReader(respBody))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+backend.APIKey)
	req.Header.Set("User-Agent", "fmg/"+a.version)
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

func (a *OpenAIResponsesAdapter) Probe(ctx context.Context, backend *model.BackendModel) error {
	return NewOpenAIAdapter(a.client, a.version).Probe(ctx, backend)
}

func openAIToResponsesRequest(body []byte) ([]byte, error) {
	var openAI struct {
		Model       string             `json:"model"`
		Messages    []protocol.Message `json:"messages"`
		MaxTokens   int                `json:"max_tokens,omitempty"`
		Temperature *float64           `json:"temperature,omitempty"`
		TopP        *float64           `json:"top_p,omitempty"`
		Stream      bool               `json:"stream,omitempty"`
	}
	if err := json.Unmarshal(body, &openAI); err != nil {
		return nil, err
	}

	input := make([]json.RawMessage, 0, len(openAI.Messages))
	var instructions string
	for _, m := range openAI.Messages {
		if m.Role == "system" {
			instructions = m.Content.String()
			continue
		}
		msg, _ := json.Marshal(map[string]interface{}{
			"type":    "message",
			"role":    m.Role,
			"content": m.Content.String(),
		})
		input = append(input, msg)
	}

	respReq := map[string]interface{}{
		"model":  openAI.Model,
		"input":  input,
		"stream": openAI.Stream,
	}
	if instructions != "" {
		respReq["instructions"] = instructions
	}
	if openAI.MaxTokens > 0 {
		respReq["max_output_tokens"] = openAI.MaxTokens
	}
	if openAI.Temperature != nil {
		respReq["temperature"] = *openAI.Temperature
	}
	if openAI.TopP != nil {
		respReq["top_p"] = *openAI.TopP
	}

	return json.Marshal(respReq)
}

func responsesToOpenAIResponse(body []byte) ([]byte, error) {
	var resp struct {
		ID     string          `json:"id"`
		Model  string          `json:"model"`
		Output json.RawMessage `json:"output"`
		Usage  map[string]int  `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	text := ""
	if len(resp.Output) > 0 {
		var items []protocol.OAIRespItem
		if err := json.Unmarshal(resp.Output, &items); err == nil {
			for _, it := range items {
				if it.Type == "message" {
					for _, b := range it.Content {
						if b.Type == "output_text" {
							text += b.Text
						}
					}
				}
			}
		}
	}

	openAI := map[string]interface{}{
		"id":     resp.ID,
		"object": "chat.completion",
		"model":  resp.Model,
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"message":       map[string]string{"role": "assistant", "content": text},
				"finish_reason": "stop",
			},
		},
	}

	if resp.Usage != nil {
		openAI["usage"] = resp.Usage
	}

	return json.Marshal(openAI)
}

package provider

import (
	"bufio"
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

// AnthropicAdapter translates between OpenAI and Anthropic formats.
type AnthropicAdapter struct {
	client  *http.Client
	version string
}

func NewAnthropicAdapter(client *http.Client, version string) *AnthropicAdapter {
	return &AnthropicAdapter{client: client, version: version}
}

func resolveAnthropicURL(baseURL string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if strings.Contains(baseURL, "/messages") {
		return baseURL
	}
	if strings.HasSuffix(baseURL, "/v1") {
		return baseURL + "/messages"
	}
	return baseURL + "/v1/messages"
}

func (a *AnthropicAdapter) Forward(ctx context.Context, backend *model.BackendModel, body []byte) (*ForwardResult, error) {
	rewritten, err := rewriteModel(body, backend.ModelID)
	if err != nil {
		return nil, fmt.Errorf("rewrite body: %w", err)
	}
	anthroBody, err := openAIToAnthropicRequest(rewritten)
	if err != nil {
		return nil, fmt.Errorf("translate request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resolveAnthropicURL(backend.BaseURL), bytes.NewReader(anthroBody))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", backend.APIKey)
	req.Header.Set("Anthropic-Version", "2023-06-01")
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

	openAIBody, err := anthropicToOpenAIResponse(respBody)
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

func (a *AnthropicAdapter) ForwardStream(ctx context.Context, backend *model.BackendModel, body []byte, w http.ResponseWriter, flusher Flusher) (*ForwardResult, error) {
	rewritten, err := rewriteModel(body, backend.ModelID)
	if err != nil {
		return nil, fmt.Errorf("rewrite body: %w", err)
	}
	anthroBody, err := openAIToAnthropicRequest(rewritten)
	if err != nil {
		return nil, fmt.Errorf("translate request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resolveAnthropicURL(backend.BaseURL), bytes.NewReader(anthroBody))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("X-API-Key", backend.APIKey)
	req.Header.Set("Anthropic-Version", "2023-06-01")
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

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	reader := bufio.NewReader(resp.Body)
	var messageID, messageModel string
	var totalUsage *Usage

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "event:") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimPrefix(line, "data:")
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			break
		}

		var event struct {
			Type    string `json:"type"`
			Message struct {
				ID    string `json:"id"`
				Model string `json:"model"`
				Role  string `json:"role"`
			} `json:"message,omitempty"`
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage,omitempty"`
		}
		if json.Unmarshal([]byte(data), &event) != nil {
			continue
		}

		if event.Type == "message_start" {
			messageID = event.Message.ID
			messageModel = event.Message.Model
			chunk := map[string]interface{}{
				"id":     messageID,
				"object": "chat.completion.chunk",
				"model":  messageModel,
				"choices": []map[string]interface{}{
					{
						"index": 0,
						"delta": map[string]string{"role": "assistant"},
					},
				},
			}
			b, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
			continue
		}

		chunk := protocol.AnthropicSSEToOpenAIStreamChunk([]byte(data))
		if chunk != nil {
			var chunkMap map[string]interface{}
			json.Unmarshal(chunk, &chunkMap)
			chunkMap["id"] = messageID
			chunkMap["model"] = messageModel
			b, _ := json.Marshal(chunkMap)
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		}

		if event.Type == "message_delta" && event.Usage.OutputTokens > 0 {
			totalUsage = &Usage{
				PromptTokens:     event.Usage.InputTokens,
				CompletionTokens: event.Usage.OutputTokens,
				TotalTokens:      event.Usage.InputTokens + event.Usage.OutputTokens,
			}
		}
	}

	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()

	result := &ForwardResult{Latency: time.Since(start)}
	if totalUsage != nil {
		result.Usage = totalUsage
	}
	return result, nil
}

func (a *AnthropicAdapter) Probe(ctx context.Context, backend *model.BackendModel) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, backend.BaseURL, nil)
	if err != nil {
		return fmt.Errorf("probe build request: %w", err)
	}
	req.Header.Set("X-API-Key", backend.APIKey)
	req.Header.Set("Anthropic-Version", "2023-06-01")
	req.Header.Set("User-Agent", "fmg/"+a.version)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("probe connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 400 || resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		return nil
	}
	return fmt.Errorf("probe returned status %d", resp.StatusCode)
}

func openAIToAnthropicRequest(body []byte) ([]byte, error) {
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

	anthro := map[string]interface{}{
		"model":    openAI.Model,
		"messages": make([]map[string]interface{}, 0, len(openAI.Messages)),
		"stream":   openAI.Stream,
	}

	for _, m := range openAI.Messages {
		role := m.Role
		if role == "assistant" {
			role = "assistant"
		}
		anthro["messages"] = append(anthro["messages"].([]map[string]interface{}), map[string]interface{}{
			"role":    role,
			"content": m.Content.String(),
		})
	}

	if openAI.MaxTokens > 0 {
		anthro["max_tokens"] = openAI.MaxTokens
	} else {
		anthro["max_tokens"] = 4096
	}
	if openAI.Temperature != nil {
		anthro["temperature"] = *openAI.Temperature
	}
	if openAI.TopP != nil {
		anthro["top_p"] = *openAI.TopP
	}

	return json.Marshal(anthro)
}

func anthropicToOpenAIResponse(body []byte) ([]byte, error) {
	var anthro protocol.AnthroResponse
	if err := json.Unmarshal(body, &anthro); err != nil {
		return nil, err
	}

	text := ""
	for _, c := range anthro.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}

	finishReason := "stop"
	switch anthro.StopReason {
	case "end_turn":
		finishReason = "stop"
	case "max_tokens":
		finishReason = "length"
	case "content_filter":
		finishReason = "content_filter"
	}

	openAI := map[string]interface{}{
		"id":     anthro.ID,
		"object": "chat.completion",
		"model":  anthro.Model,
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"message":       map[string]string{"role": "assistant", "content": text},
				"finish_reason": finishReason,
			},
		},
	}

	if anthro.Usage.InputTokens > 0 || anthro.Usage.OutputTokens > 0 {
		openAI["usage"] = map[string]int{
			"prompt_tokens":     anthro.Usage.InputTokens,
			"completion_tokens": anthro.Usage.OutputTokens,
			"total_tokens":      anthro.Usage.InputTokens + anthro.Usage.OutputTokens,
		}
	}

	return json.Marshal(openAI)
}

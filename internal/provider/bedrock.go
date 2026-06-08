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

// BedrockAdapter translates between OpenAI and AWS Bedrock formats.
type BedrockAdapter struct {
	client  *http.Client
	version string
}

func NewBedrockAdapter(client *http.Client, version string) *BedrockAdapter {
	return &BedrockAdapter{client: client, version: version}
}

func resolveBedrockURL(baseURL, modelID string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if strings.Contains(baseURL, "/model/") {
		return baseURL
	}
	return baseURL + "/model/" + modelID + "/converse"
}

func (a *BedrockAdapter) Forward(ctx context.Context, backend *model.BackendModel, body []byte) (*ForwardResult, error) {
	bedrockBody, err := openAIToBedrockRequest(body)
	if err != nil {
		return nil, fmt.Errorf("translate request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resolveBedrockURL(backend.BaseURL, backend.ModelID), bytes.NewReader(bedrockBody))
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

	openAIBody, err := bedrockToOpenAIResponse(respBody)
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

func (a *BedrockAdapter) ForwardStream(ctx context.Context, backend *model.BackendModel, body []byte, w http.ResponseWriter, flusher Flusher) (*ForwardResult, error) {
	rewritten, err := rewriteModel(body, backend.ModelID)
	if err != nil {
		return nil, fmt.Errorf("rewrite body: %w", err)
	}
	bedrockBody, err := openAIToBedrockRequest(rewritten)
	if err != nil {
		return nil, fmt.Errorf("translate request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resolveBedrockURL(backend.BaseURL, backend.ModelID), bytes.NewReader(bedrockBody))
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

func (a *BedrockAdapter) Probe(ctx context.Context, backend *model.BackendModel) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, resolveBedrockURL(backend.BaseURL, backend.ModelID), nil)
	if err != nil {
		return fmt.Errorf("probe build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+backend.APIKey)
	req.Header.Set("User-Agent", "fmg/"+a.version)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("probe connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 400 || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	return fmt.Errorf("probe returned status %d", resp.StatusCode)
}

func openAIToBedrockRequest(body []byte) ([]byte, error) {
	var openAI struct {
		Model       string             `json:"model"`
		Messages    []protocol.Message `json:"messages"`
		MaxTokens   int                `json:"max_tokens,omitempty"`
		Temperature *float64           `json:"temperature,omitempty"`
		TopP        *float64           `json:"top_p,omitempty"`
	}
	if err := json.Unmarshal(body, &openAI); err != nil {
		return nil, err
	}

	bedrock := map[string]interface{}{
		"modelId":  openAI.Model,
		"messages": make([]map[string]interface{}, 0, len(openAI.Messages)),
	}

	for _, m := range openAI.Messages {
		role := m.Role
		if role == "system" {
			if _, ok := bedrock["system"]; !ok {
				bedrock["system"] = []map[string]string{}
			}
			bedrock["system"] = append(bedrock["system"].([]map[string]string), map[string]string{"text": m.Content})
			continue
		}
		bedrock["messages"] = append(bedrock["messages"].([]map[string]interface{}), map[string]interface{}{
			"role":    role,
			"content": []map[string]string{{"text": m.Content}},
		})
	}

	if openAI.MaxTokens > 0 || openAI.Temperature != nil || openAI.TopP != nil {
		infCfg := map[string]interface{}{}
		if openAI.MaxTokens > 0 {
			infCfg["maxTokens"] = openAI.MaxTokens
		}
		if openAI.Temperature != nil {
			infCfg["temperature"] = *openAI.Temperature
		}
		if openAI.TopP != nil {
			infCfg["topP"] = *openAI.TopP
		}
		bedrock["inferenceConfig"] = infCfg
	}

	return json.Marshal(bedrock)
}

func bedrockToOpenAIResponse(body []byte) ([]byte, error) {
	var bedrock protocol.BedrockResponse
	if err := json.Unmarshal(body, &bedrock); err != nil {
		return nil, err
	}

	text := ""
	finishReason := "stop"
	if bedrock.Output != nil {
		for _, c := range bedrock.Output.Message.Content {
			text += c.Text
		}
	}
	switch bedrock.StopReason {
	case "end_turn":
		finishReason = "stop"
	case "max_tokens":
		finishReason = "length"
	case "content_filtered":
		finishReason = "content_filter"
	}

	openAI := map[string]interface{}{
		"id":     "bedrock-" + fmt.Sprintf("%d", time.Now().Unix()),
		"object": "chat.completion",
		"model":  "bedrock",
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"message":       map[string]string{"role": "assistant", "content": text},
				"finish_reason": finishReason,
			},
		},
	}

	if bedrock.Usage != nil {
		openAI["usage"] = map[string]int{
			"prompt_tokens":     bedrock.Usage.InputTokens,
			"completion_tokens": bedrock.Usage.OutputTokens,
			"total_tokens":      bedrock.Usage.TotalTokens,
		}
	}

	return json.Marshal(openAI)
}

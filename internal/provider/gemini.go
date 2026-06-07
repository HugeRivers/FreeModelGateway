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

// GeminiAdapter translates between OpenAI and Google Gemini formats.
type GeminiAdapter struct {
	client  *http.Client
	version string
}

func NewGeminiAdapter(client *http.Client, version string) *GeminiAdapter {
	return &GeminiAdapter{client: client, version: version}
}

func (a *GeminiAdapter) Forward(ctx context.Context, backend *model.BackendModel, body []byte) (*ForwardResult, error) {
	geminiBody, err := openAIToGeminiRequest(body)
	if err != nil {
		return nil, fmt.Errorf("translate request: %w", err)
	}

	url := buildGeminiURL(backend.BaseURL, backend.ModelID, false)
	if !bytes.Contains([]byte(url), []byte("?")) {
		url += "?key=" + backend.APIKey
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(geminiBody))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
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

	openAIBody, err := geminiToOpenAIResponse(respBody)
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

func (a *GeminiAdapter) ForwardStream(ctx context.Context, backend *model.BackendModel, body []byte, w http.ResponseWriter, flusher Flusher) (*ForwardResult, error) {
	rewritten, err := rewriteModel(body, backend.ModelID)
	if err != nil {
		return nil, fmt.Errorf("rewrite body: %w", err)
	}
	geminiBody, err := openAIToGeminiRequest(rewritten)
	if err != nil {
		return nil, fmt.Errorf("translate request: %w", err)
	}

	url := buildGeminiURL(backend.BaseURL, backend.ModelID, true)
	if !bytes.Contains([]byte(url), []byte("?")) {
		url += "?key=" + backend.APIKey
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(geminiBody))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
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

func (a *GeminiAdapter) Probe(ctx context.Context, backend *model.BackendModel) error {
	url := buildGeminiURL(backend.BaseURL, backend.ModelID, false)
	if !bytes.Contains([]byte(url), []byte("?")) {
		url += "?key=" + backend.APIKey
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return fmt.Errorf("probe build request: %w", err)
	}
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

func openAIToGeminiRequest(body []byte) ([]byte, error) {
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

	gemini := map[string]interface{}{
		"contents": make([]map[string]interface{}, 0, len(openAI.Messages)),
	}
	if openAI.Temperature != nil {
		gemini["generationConfig"] = map[string]interface{}{"temperature": *openAI.Temperature}
	}
	if openAI.TopP != nil {
		if gc, ok := gemini["generationConfig"].(map[string]interface{}); ok {
			gc["topP"] = *openAI.TopP
		}
	}
	if openAI.MaxTokens > 0 {
		if gc, ok := gemini["generationConfig"].(map[string]interface{}); ok {
			gc["maxOutputTokens"] = openAI.MaxTokens
		} else {
			gemini["generationConfig"] = map[string]interface{}{"maxOutputTokens": openAI.MaxTokens}
		}
	}

	for _, m := range openAI.Messages {
		role := m.Role
		switch role {
		case "assistant":
			role = "model"
		case "system":
			gemini["systemInstruction"] = map[string]interface{}{
				"parts": []map[string]string{{"text": m.Content}},
			}
			continue
		}
		gemini["contents"] = append(gemini["contents"].([]map[string]interface{}), map[string]interface{}{
			"role":  role,
			"parts": []map[string]string{{"text": m.Content}},
		})
	}

	return json.Marshal(gemini)
}

func geminiToOpenAIResponse(body []byte) ([]byte, error) {
	var gemini protocol.GeminiResponse
	if err := json.Unmarshal(body, &gemini); err != nil {
		return nil, err
	}

	text := ""
	finishReason := "stop"
	if len(gemini.Candidates) > 0 {
		c := gemini.Candidates[0]
		for _, p := range c.Content.Parts {
			text += p.Text
		}
		switch c.FinishReason {
		case "STOP":
			finishReason = "stop"
		case "MAX_TOKENS":
			finishReason = "length"
		case "SAFETY":
			finishReason = "content_filter"
		}
	}

	openAI := map[string]interface{}{
		"id":     "gemini-" + fmt.Sprintf("%d", time.Now().Unix()),
		"object": "chat.completion",
		"model":  "gemini",
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"message":       map[string]string{"role": "assistant", "content": text},
				"finish_reason": finishReason,
			},
		},
	}

	if gemini.UsageMeta != nil {
		openAI["usage"] = map[string]int{
			"prompt_tokens":     gemini.UsageMeta.PromptTokenCount,
			"completion_tokens": gemini.UsageMeta.CandidatesTokenCount,
			"total_tokens":      gemini.UsageMeta.TotalTokenCount,
		}
	}

	return json.Marshal(openAI)
}

// buildGeminiURL constructs the full Gemini API URL from base URL and model ID.
// If baseURL already contains a method path (e.g. :generateContent), it's used as-is.
// If baseURL ends with /v1 or /v1beta, the model path is appended dynamically.
func buildGeminiURL(baseURL, modelID string, stream bool) string {
	if baseURL == "" {
		return ""
	}
	// Already contains method path — use as-is for backward compatibility
	if strings.Contains(baseURL, ":generateContent") || strings.Contains(baseURL, ":streamGenerateContent") {
		if stream && strings.Contains(baseURL, ":generateContent") && !strings.Contains(baseURL, ":streamGenerateContent") {
			return strings.Replace(baseURL, ":generateContent", ":streamGenerateContent", 1)
		}
		return baseURL
	}
	// Trim trailing slash
	baseURL = strings.TrimRight(baseURL, "/")
	method := ":generateContent"
	if stream {
		method = ":streamGenerateContent"
	}
	return baseURL + "/models/" + modelID + method
}

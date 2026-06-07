package protocol

import (
	"encoding/json"
	"fmt"
)

type GeminiContent struct {
	Role  string       `json:"role"`
	Parts []GeminiPart `json:"parts"`
}

type GeminiPart struct {
	Text string `json:"text"`
}

type GeminiRequest struct {
	Contents          []GeminiContent    `json:"contents"`
	SystemInstruction *GeminiInstruction `json:"systemInstruction,omitempty"`
	GenerationConfig  *GeminiGenConfig   `json:"generationConfig,omitempty"`
}

type GeminiInstruction struct {
	Parts []GeminiPart `json:"parts"`
}

type GeminiGenConfig struct {
	MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"topP,omitempty"`
}

type GeminiResponse struct {
	Candidates []GeminiCandidate `json:"candidates"`
	UsageMeta  *GeminiUsageMeta  `json:"usageMetadata,omitempty"`
}

type GeminiCandidate struct {
	Content      GeminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
}

type GeminiUsageMeta struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

func geminiMessagesToOpenAI(req *GeminiRequest) []Message {
	msgs := make([]Message, 0, len(req.Contents)+1)
	if req.SystemInstruction != nil {
		text := ""
		for _, p := range req.SystemInstruction.Parts {
			text += p.Text
		}
		msgs = append(msgs, Message{Role: "system", Content: text})
	}
	for _, c := range req.Contents {
		role := c.Role
		switch role {
		case "model":
			role = "assistant"
		case "user":
			role = "user"
		default:
			role = "user"
		}
		text := ""
		for _, p := range c.Parts {
			text += p.Text
		}
		msgs = append(msgs, Message{Role: role, Content: text})
	}
	return msgs
}

func GeminiRequestToOpenAI(body []byte) ([]byte, error) {
	var req GeminiRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("parse gemini request: %w", err)
	}
	openAI := map[string]interface{}{
		"model":    "auto",
		"messages": geminiMessagesToOpenAI(&req),
	}
	if req.GenerationConfig != nil {
		gc := req.GenerationConfig
		if gc.MaxOutputTokens > 0 {
			openAI["max_tokens"] = gc.MaxOutputTokens
		}
		if gc.Temperature != nil {
			openAI["temperature"] = *gc.Temperature
		}
		if gc.TopP != nil {
			openAI["top_p"] = *gc.TopP
		}
	}
	return json.Marshal(openAI)
}

func OpenAIResponseToGemini(openAIResp []byte) ([]byte, error) {
	var raw struct {
		ID      string   `json:"id"`
		Model   string   `json:"model"`
		Choices []Choice `json:"choices"`
		Usage   *Usage   `json:"usage,omitempty"`
	}
	if err := json.Unmarshal(openAIResp, &raw); err != nil {
		return nil, fmt.Errorf("parse openai response: %w", err)
	}

	gemini := GeminiResponse{
		Candidates: make([]GeminiCandidate, 0),
	}
	for _, c := range raw.Choices {
		role := "model"
		finish := "STOP"
		switch c.FinishReason {
		case "stop":
			finish = "STOP"
		case "length":
			finish = "MAX_TOKENS"
		case "content_filter":
			finish = "SAFETY"
		default:
			finish = "STOP"
		}
		gemini.Candidates = append(gemini.Candidates, GeminiCandidate{
			Content: GeminiContent{
				Role:  role,
				Parts: []GeminiPart{{Text: c.Message.Content}},
			},
			FinishReason: finish,
		})
	}
	if raw.Usage != nil {
		gemini.UsageMeta = &GeminiUsageMeta{
			PromptTokenCount:     raw.Usage.PromptTokens,
			CandidatesTokenCount: raw.Usage.CompletionTokens,
			TotalTokenCount:      raw.Usage.TotalTokens,
		}
	}
	return json.Marshal(gemini)
}

func OpenAIStreamChunkToGeminiSSE(data []byte) (events []string) {
	var chunk struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Choices []struct {
			Index        int     `json:"index"`
			Delta        Message `json:"delta"`
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &chunk); err != nil || len(chunk.Choices) == 0 {
		return nil
	}

	c := chunk.Choices[0]
	candidate := map[string]interface{}{
		"index": c.Index,
		"content": map[string]interface{}{
			"role":  "model",
			"parts": []map[string]string{{"text": c.Delta.Content}},
		},
	}
	if c.FinishReason != nil {
		finish := "STOP"
		switch *c.FinishReason {
		case "stop":
			finish = "STOP"
		case "length":
			finish = "MAX_TOKENS"
		}
		candidate["finishReason"] = finish
	}
	chunkData, _ := json.Marshal(map[string]interface{}{
		"candidates": []interface{}{candidate},
	})
	events = append(events, "data: "+string(chunkData))
	return events
}

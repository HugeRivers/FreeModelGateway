package protocol

import (
	"encoding/json"
	"fmt"
)

type BedrockContent struct {
	Text string `json:"text"`
}

type BedrockMessage struct {
	Role    string           `json:"role"`
	Content []BedrockContent `json:"content"`
}

type BedrockSystem struct {
	Text string `json:"text"`
}

type BedrockInferenceConfig struct {
	MaxTokens   int      `json:"maxTokens,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"topP,omitempty"`
}

type BedrockRequest struct {
	ModelID         string                  `json:"modelId,omitempty"`
	Messages        []BedrockMessage        `json:"messages"`
	System          []BedrockSystem         `json:"system,omitempty"`
	InferenceConfig *BedrockInferenceConfig `json:"inferenceConfig,omitempty"`
}

type BedrockResponse struct {
	Output     *BedrockOutput `json:"output,omitempty"`
	StopReason string         `json:"stopReason"`
	Usage      *BedrockUsage  `json:"usage,omitempty"`
	Metrics    *struct{}      `json:"metrics,omitempty"`
}

type BedrockOutput struct {
	Message BedrockMessage `json:"message"`
}

type BedrockUsage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
	TotalTokens  int `json:"totalTokens"`
}

func bedrockMessagesToOpenAI(req *BedrockRequest) []Message {
	msgs := make([]Message, 0, len(req.Messages)+1)
	for _, s := range req.System {
		msgs = append(msgs, Message{Role: "system", Content: StringContent(s.Text)})
	}
	for _, m := range req.Messages {
		text := ""
		for _, c := range m.Content {
			text += c.Text
		}
		role := m.Role
		switch role {
		case "assistant":
			role = "assistant"
		default:
			role = "user"
		}
		msgs = append(msgs, Message{Role: role, Content: StringContent(text)})
	}
	return msgs
}

func BedrockRequestToOpenAI(body []byte) ([]byte, error) {
	var req BedrockRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("parse bedrock request: %w", err)
	}
	model := req.ModelID
	if model == "" {
		model = "auto"
	}
	openAI := map[string]interface{}{
		"model":    model,
		"messages": bedrockMessagesToOpenAI(&req),
	}
	if req.InferenceConfig != nil {
		ic := req.InferenceConfig
		if ic.MaxTokens > 0 {
			openAI["max_tokens"] = ic.MaxTokens
		}
		if ic.Temperature != nil {
			openAI["temperature"] = *ic.Temperature
		}
		if ic.TopP != nil {
			openAI["top_p"] = *ic.TopP
		}
	}
	return json.Marshal(openAI)
}

func OpenAIResponseToBedrock(openAIResp []byte) ([]byte, error) {
	var raw struct {
		ID      string   `json:"id"`
		Model   string   `json:"model"`
		Choices []Choice `json:"choices"`
		Usage   *Usage   `json:"usage,omitempty"`
	}
	if err := json.Unmarshal(openAIResp, &raw); err != nil {
		return nil, fmt.Errorf("parse openai response: %w", err)
	}

	resp := BedrockResponse{}

	if len(raw.Choices) > 0 {
		c := raw.Choices[0]
		resp.Output = &BedrockOutput{
			Message: BedrockMessage{
				Role:    "assistant",
				Content: []BedrockContent{{Text: c.Message.Content.String()}},
			},
		}
		switch c.FinishReason {
		case "stop":
			resp.StopReason = "end_turn"
		case "length":
			resp.StopReason = "max_tokens"
		case "content_filter":
			resp.StopReason = "content_filtered"
		default:
			resp.StopReason = "end_turn"
		}
	}

	if raw.Usage != nil {
		resp.Usage = &BedrockUsage{
			InputTokens:  raw.Usage.PromptTokens,
			OutputTokens: raw.Usage.CompletionTokens,
			TotalTokens:  raw.Usage.TotalTokens,
		}
	}

	return json.Marshal(resp)
}

func OpenAIStreamChunkToBedrockSSE(data []byte) (events []string) {
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
	chunkData := map[string]interface{}{
		"contentBlockIndex": c.Index,
	}

	if c.Delta.Content.String() != "" {
		chunkData["contentBlockDelta"] = map[string]interface{}{
			"delta": map[string]string{"text": c.Delta.Content.String()},
		}
	}

	if c.FinishReason != nil {
		stop := "end_turn"
		switch *c.FinishReason {
		case "stop":
			stop = "end_turn"
		case "length":
			stop = "max_tokens"
		}
		chunkData["stopReason"] = stop
	}

	d, _ := json.Marshal(chunkData)
	events = append(events, "data: "+string(d))
	return events
}

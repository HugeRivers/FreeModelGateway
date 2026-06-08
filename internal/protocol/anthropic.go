package protocol

import (
	"encoding/json"
	"fmt"
)

type Content json.RawMessage

func StringContent(s string) Content {
	b, _ := json.Marshal(s)
	return Content(b)
}

func (c Content) String() string {
	if len(c) == 0 {
		return ""
	}
	if c[0] == '"' {
		var s string
		_ = json.Unmarshal([]byte(c), &s)
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(c), &blocks); err != nil {
		return string(c)
	}
	var out string
	for _, b := range blocks {
		if b.Type == "text" {
			out += b.Text
		}
	}
	return out
}

type Message struct {
	Role    string  `json:"role"`
	Content Content `json:"content"`
}

type Choice struct {
	Index        int      `json:"index"`
	Message      Message  `json:"message"`
	FinishReason string   `json:"finish_reason"`
	LogProbs     *float64 `json:"logprobs,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type AnthroContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type AnthroMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type AnthroRequest struct {
	Model       string          `json:"model"`
	Messages    []AnthroMessage `json:"messages"`
	System      string          `json:"system,omitempty"`
	MaxTokens   int             `json:"max_tokens"`
	Stream      bool            `json:"stream,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	TopP        *float64        `json:"top_p,omitempty"`
	Stop        []string        `json:"stop_sequences,omitempty"`
}

type AnthroResponse struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Role       string          `json:"role"`
	Content    []AnthroContent `json:"content"`
	Model      string          `json:"model"`
	StopReason string          `json:"stop_reason"`
	StopSeq    *string         `json:"stop_sequence"`
	Usage      AnthroUsage     `json:"usage"`
}

type AnthroUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

func anthropicMessagesToOpenAI(anthro *AnthroRequest) []Message {
	msgs := make([]Message, 0, len(anthro.Messages)+1)
	if anthro.System != "" {
		msgs = append(msgs, Message{Role: "system", Content: StringContent(anthro.System)})
	}
	for _, m := range anthro.Messages {
		msgs = append(msgs, Message{Role: m.Role, Content: StringContent(extractAnthroContent(m.Content))})
	}
	return msgs
}

func extractAnthroContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var s string
		_ = json.Unmarshal(raw, &s)
		return s
	}
	var blocks []AnthroContent
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return string(raw)
	}
	var out string
	for _, b := range blocks {
		if b.Type == "text" {
			out += b.Text
		}
	}
	return out
}

func ParseAnthropicRequest(body []byte) (*AnthroRequest, error) {
	var req AnthroRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("parse anthropic request: %w", err)
	}
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("anthropic: messages is required")
	}
	return &req, nil
}

func AnthropicRequestToOpenAI(anthro *AnthroRequest) ([]byte, error) {
	model := anthro.Model
	if model == "" {
		model = "auto"
	}
	openAI := map[string]interface{}{
		"model":    model,
		"messages": anthropicMessagesToOpenAI(anthro),
		"stream":   anthro.Stream,
	}
	if anthro.MaxTokens > 0 {
		openAI["max_tokens"] = anthro.MaxTokens
	}
	if anthro.Temperature != nil {
		openAI["temperature"] = *anthro.Temperature
	}
	if anthro.TopP != nil {
		openAI["top_p"] = *anthro.TopP
	}
	body, err := json.Marshal(openAI)
	if err != nil {
		return nil, fmt.Errorf("marshal openai request: %w", err)
	}
	return body, nil
}

func OpenAIResponseToAnthropic(openAIResp []byte) ([]byte, error) {
	var raw struct {
		ID      string   `json:"id"`
		Model   string   `json:"model"`
		Choices []Choice `json:"choices"`
		Usage   *Usage   `json:"usage,omitempty"`
	}
	if err := json.Unmarshal(openAIResp, &raw); err != nil {
		return nil, fmt.Errorf("parse openai response: %w", err)
	}

	anthro := AnthroResponse{
		ID:      "msg_" + raw.ID,
		Type:    "message",
		Role:    "assistant",
		Content: make([]AnthroContent, 0),
		Model:   raw.Model,
	}

	if len(raw.Choices) > 0 {
		c := raw.Choices[0]
		anthro.Content = append(anthro.Content, AnthroContent{Type: "text", Text: c.Message.Content.String()})
		switch c.FinishReason {
		case "stop":
			anthro.StopReason = "end_turn"
		case "length":
			anthro.StopReason = "max_tokens"
		case "content_filter":
			anthro.StopReason = "content_filter"
		default:
			anthro.StopReason = c.FinishReason
		}
	}

	if raw.Usage != nil {
		anthro.Usage = AnthroUsage{
			InputTokens:  raw.Usage.PromptTokens,
			OutputTokens: raw.Usage.CompletionTokens,
		}
	}

	return json.Marshal(anthro)
}

func AnthropicSSEToOpenAIStreamChunk(anthroData []byte) []byte {
	var anthro struct {
		Type  string `json:"type"`
		Index int    `json:"index"`
		Delta struct {
			Type       string `json:"type"`
			Text       string `json:"text,omitempty"`
			StopReason string `json:"stop_reason,omitempty"`
		} `json:"delta,omitempty"`
		Message struct {
			ID      string          `json:"id"`
			Model   string          `json:"model"`
			Role    string          `json:"role"`
			Content []AnthroContent `json:"content"`
		} `json:"message,omitempty"`
		ContentBlock AnthroContent `json:"content_block,omitempty"`
	}
	if err := json.Unmarshal(anthroData, &anthro); err != nil {
		return nil
	}

	// Only process content_block_delta with text
	if anthro.Type == "content_block_delta" && anthro.Delta.Text != "" {
		chunk := map[string]interface{}{
			"object": "chat.completion.chunk",
			"choices": []map[string]interface{}{
				{
					"index": anthro.Index,
					"delta": map[string]string{"content": anthro.Delta.Text},
				},
			},
		}
		b, _ := json.Marshal(chunk)
		return b
	}

	// message_delta for finish_reason
	if anthro.Type == "message_delta" && anthro.Delta.StopReason != "" {
		finishReason := "stop"
		switch anthro.Delta.StopReason {
		case "end_turn":
			finishReason = "stop"
		case "max_tokens":
			finishReason = "length"
		case "content_filter":
			finishReason = "content_filter"
		}
		chunk := map[string]interface{}{
			"object": "chat.completion.chunk",
			"choices": []map[string]interface{}{
				{
					"index":         0,
					"delta":         map[string]string{},
					"finish_reason": finishReason,
				},
			},
		}
		b, _ := json.Marshal(chunk)
		return b
	}

	return nil
}

func OpenAIStreamChunkToAnthropicSSE(data []byte) (events []string) {
	var chunk struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Object  string `json:"object"`
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

	if c.Delta.Role == "assistant" {
		msgData, _ := json.Marshal(map[string]interface{}{
			"id":            "msg_" + chunk.ID,
			"type":          "message",
			"role":          "assistant",
			"content":       []AnthroContent{},
			"model":         chunk.Model,
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         map[string]int{"input_tokens": 0, "output_tokens": 0},
		})
		events = append(events, "event: message_start\ndata: "+string(msgData))
		cbData, _ := json.Marshal(AnthroContent{Type: "text", Text: ""})
		blockData, _ := json.Marshal(map[string]interface{}{
			"type":          "content_block_start",
			"index":         c.Index,
			"content_block": json.RawMessage(cbData),
		})
		events = append(events, "event: content_block_start\ndata: "+string(blockData))
	}

	if c.Delta.Content.String() != "" {
		deltaData, _ := json.Marshal(map[string]interface{}{
			"type":  "content_block_delta",
			"index": c.Index,
			"delta": map[string]string{"type": "text_delta", "text": c.Delta.Content.String()},
		})
		events = append(events, "event: content_block_delta\ndata: "+string(deltaData))
	}

	if c.FinishReason != nil {
		events = append(events, "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":"+fmt.Sprint(c.Index)+"}")
		stopReason := "end_turn"
		switch *c.FinishReason {
		case "stop":
			stopReason = "end_turn"
		case "length":
			stopReason = "max_tokens"
		default:
			stopReason = *c.FinishReason
		}
		msgDelta, _ := json.Marshal(map[string]interface{}{
			"type":  "message_delta",
			"delta": map[string]interface{}{"stop_reason": stopReason, "stop_sequence": nil},
			"usage": map[string]int{"output_tokens": 0},
		})
		events = append(events, "event: message_delta\ndata: "+string(msgDelta))
		events = append(events, "event: message_stop\ndata: {\"type\":\"message_stop\"}")
	}

	return events
}

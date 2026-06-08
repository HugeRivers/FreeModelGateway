package protocol

import (
	"encoding/json"
	"fmt"
)

type OAIRespRequest struct {
	Model           string          `json:"model"`
	Input           json.RawMessage `json:"input"`
	Instructions    string          `json:"instructions,omitempty"`
	MaxOutputTokens int             `json:"max_output_tokens,omitempty"`
	Stream          bool            `json:"stream,omitempty"`
	Temperature     *float64        `json:"temperature,omitempty"`
	TopP            *float64        `json:"top_p,omitempty"`
}

type OAIRespItem struct {
	ID      string         `json:"id"`
	Type    string         `json:"type"`
	Role    string         `json:"role,omitempty"`
	Content []OAIRespBlock `json:"content,omitempty"`
	Status  string         `json:"status,omitempty"`
}

type OAIRespBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type OAIRespUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

func parseOAIRespInput(raw json.RawMessage) []Message {
	if len(raw) == 0 {
		return nil
	}
	if raw[0] == '"' {
		var s string
		_ = json.Unmarshal(raw, &s)
		return []Message{{Role: "user", Content: StringContent(s)}}
	}
	var msgs []Message
	if err := json.Unmarshal(raw, &msgs); err != nil {
		var items []OAIRespItem
		if err2 := json.Unmarshal(raw, &items); err2 == nil {
			for _, it := range items {
				if it.Type == "message" {
					text := ""
					for _, b := range it.Content {
						if b.Type == "input_text" {
							text += b.Text
						}
					}
					role := it.Role
					if role == "" {
						role = "user"
					}
					msgs = append(msgs, Message{Role: role, Content: StringContent(text)})
				}
			}
		}
	}
	return msgs
}

func OpenAIResponsesRequestToOpenAI(body []byte) ([]byte, error) {
	var req OAIRespRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("parse openai responses request: %w", err)
	}
	msgs := parseOAIRespInput(req.Input)
	if req.Instructions != "" {
		msgs = append([]Message{{Role: "system", Content: StringContent(req.Instructions)}}, msgs...)
	}
	model := req.Model
	if model == "" {
		model = "auto"
	}
	openAI := map[string]interface{}{
		"model":    model,
		"messages": msgs,
		"stream":   req.Stream,
	}
	if req.MaxOutputTokens > 0 {
		openAI["max_tokens"] = req.MaxOutputTokens
	}
	if req.Temperature != nil {
		openAI["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		openAI["top_p"] = *req.TopP
	}
	return json.Marshal(openAI)
}

func OpenAIResponseToOpenAIResponses(openAIResp []byte) ([]byte, error) {
	var raw struct {
		ID      string   `json:"id"`
		Model   string   `json:"model"`
		Choices []Choice `json:"choices"`
		Usage   *Usage   `json:"usage,omitempty"`
	}
	if err := json.Unmarshal(openAIResp, &raw); err != nil {
		return nil, fmt.Errorf("parse openai response: %w", err)
	}

	resp := map[string]interface{}{
		"id":     "resp_" + raw.ID,
		"object": "response",
		"model":  raw.Model,
		"output": []OAIRespItem{},
		"usage":  map[string]int{"input_tokens": 0, "output_tokens": 0, "total_tokens": 0},
	}

	if len(raw.Choices) > 0 {
		c := raw.Choices[0]
		item := OAIRespItem{
			ID:      "msg_" + raw.ID,
			Type:    "message",
			Role:    "assistant",
			Content: []OAIRespBlock{{Type: "output_text", Text: c.Message.Content.String()}},
			Status:  "completed",
		}
		resp["output"] = []OAIRespItem{item}
	}

	if raw.Usage != nil {
		resp["usage"] = map[string]int{
			"input_tokens":  raw.Usage.PromptTokens,
			"output_tokens": raw.Usage.CompletionTokens,
			"total_tokens":  raw.Usage.TotalTokens,
		}
	}

	return json.Marshal(resp)
}

func OpenAIStreamChunkToOAIRespSSE(data []byte) (events []string) {
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
		sd, _ := json.Marshal(map[string]interface{}{
			"type": "response.output_item.added",
			"item": map[string]interface{}{
				"id":      "msg_" + chunk.ID,
				"type":    "message",
				"role":    "assistant",
				"content": []map[string]string{{"type": "output_text", "text": ""}},
			},
		})
		events = append(events, "event: response.output_item.added\ndata: "+string(sd))

		cd, _ := json.Marshal(map[string]interface{}{
			"type": "response.content_part.added",
			"part": map[string]string{"type": "output_text", "text": ""},
		})
		events = append(events, "event: response.content_part.added\ndata: "+string(cd))
	}

	if c.Delta.Content.String() != "" {
		td, _ := json.Marshal(map[string]interface{}{
			"type":  "response.output_text.delta",
			"delta": c.Delta.Content.String(),
		})
		events = append(events, "event: response.output_text.delta\ndata: "+string(td))
	}

	if c.FinishReason != nil {
		sd, _ := json.Marshal(map[string]interface{}{
			"type":   "response.output_text.done",
			"status": "completed",
		})
		events = append(events, "event: response.output_text.done\ndata: "+string(sd))

		rd, _ := json.Marshal(map[string]interface{}{
			"type":   "response.done",
			"status": "completed",
		})
		events = append(events, "event: response.done\ndata: "+string(rd))
	}

	return events
}

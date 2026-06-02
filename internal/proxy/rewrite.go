package proxy

import (
	"encoding/json"
)

type Metadata struct {
	Gateway         string   `json:"gateway"`
	Version         string   `json:"version"`
	ActualProvider  string   `json:"actual_provider"`
	ActualModelID   string   `json:"actual_model_id"`
	ActualModelName string   `json:"actual_model_name"`
	FallbackCount   int      `json:"fallback_count"`
	FallbackChain   []string `json:"fallback_chain"`
	RouteStrategy   string   `json:"route_strategy"`
	LatencyMs       int64    `json:"latency_ms"`
}

func RewriteRequestBody(body []byte, targetModel string) ([]byte, error) {
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

func InjectMetadata(response []byte, meta Metadata) ([]byte, error) {
	if len(response) == 0 {
		return response, nil
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(response, &raw); err != nil {
		return response, nil
	}
	raw["metadata"] = meta
	return json.Marshal(raw)
}

func IsClientError(status int) bool {
	switch status {
	case 400, 401, 403, 404, 422:
		return true
	}
	return false
}

func IsRetryableError(status int) bool {
	switch status {
	case 408, 409, 425, 429, 500, 502, 503, 504:
		return true
	}
	return false
}

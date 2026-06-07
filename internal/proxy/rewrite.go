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

package provider

import (
	"context"
	"net/http"

	"github.com/free-model-gateway/fmg/internal/model"
)

// Adapter handles request/response translation for a specific API format.
type Adapter interface {
	Forward(ctx context.Context, backend *model.BackendModel, body []byte) (*ForwardResult, error)
	ForwardStream(ctx context.Context, backend *model.BackendModel, body []byte, w http.ResponseWriter, flusher Flusher) (*ForwardResult, error)
	Probe(ctx context.Context, backend *model.BackendModel) error
}

// Registry maps api_format values to their adapters.
type Registry struct {
	adapters map[string]Adapter
	fallback Adapter
}

func NewRegistry(client *http.Client, version string) *Registry {
	openai := NewOpenAIAdapter(client, version)
	r := &Registry{
		adapters: make(map[string]Adapter),
		fallback: openai,
	}
	r.Register("openai-compatible", openai)
	r.Register("openai-responses", NewOpenAIResponsesAdapter(client, version))
	r.Register("anthropic", NewAnthropicAdapter(client, version))
	r.Register("gemini", NewGeminiAdapter(client, version))
	r.Register("bedrock", NewBedrockAdapter(client, version))
	return r
}

func (r *Registry) Register(format string, adapter Adapter) {
	r.adapters[format] = adapter
}

func (r *Registry) Get(format string) Adapter {
	if a, ok := r.adapters[format]; ok {
		return a
	}
	return r.fallback
}

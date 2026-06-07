package proxy

import (
	"context"
	"net/http"

	"github.com/free-model-gateway/fmg/internal/model"
	"github.com/free-model-gateway/fmg/internal/provider"
	"github.com/sirupsen/logrus"
)

type Forwarder struct {
	client   *http.Client
	log      *logrus.Logger
	version  string
	registry *provider.Registry
}

func NewForwarder(client *http.Client, log *logrus.Logger, version string) *Forwarder {
	return &Forwarder{client: client, log: log, version: version, registry: provider.NewRegistry(client, version)}
}

func (f *Forwarder) SetRegistry(r *provider.Registry) {
	f.registry = r
}

func (f *Forwarder) GetRegistry() *provider.Registry {
	return f.registry
}

func (f *Forwarder) Forward(ctx context.Context, backend *model.BackendModel, body []byte) (*provider.ForwardResult, error) {
	adapter := f.registry.Get(backend.APIFormat)
	result, err := adapter.Forward(ctx, backend, body)
	if err == nil && result != nil {
		if result.Usage != nil {
			backend.MarkSuccess(result.Usage.PromptTokens, result.Usage.CompletionTokens, result.Latency)
		} else {
			backend.MarkSuccess(0, 0, result.Latency)
		}
	}
	return result, err
}

func (f *Forwarder) ForwardStream(ctx context.Context, backend *model.BackendModel, body []byte, w http.ResponseWriter, flusher provider.Flusher) (*provider.ForwardResult, error) {
	adapter := f.registry.Get(backend.APIFormat)
	return adapter.ForwardStream(ctx, backend, body, w, flusher)
}

func (f *Forwarder) Probe(ctx context.Context, backend *model.BackendModel) error {
	adapter := f.registry.Get(backend.APIFormat)
	return adapter.Probe(ctx, backend)
}

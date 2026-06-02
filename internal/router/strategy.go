package router

import (
	"context"

	"github.com/free-model-gateway/fmg/internal/model"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Request struct {
	Messages    []Message
	Model       string
	Stream      bool
	Temperature float32
	MaxTokens   int
	TopP        float32
	RawBody     []byte
}

type Strategy interface {
	Select(ctx context.Context, candidates []*model.BackendModel, req *Request) (*model.BackendModel, error)
	Name() string
}

package router

import (
	"context"
	"errors"

	"github.com/free-model-gateway/fmg/internal/model"
)

var ErrNoCandidate = errors.New("no candidate model available")

type Message struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
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

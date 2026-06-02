package router

import (
	"context"

	"github.com/free-model-gateway/fmg/internal/model"
)

type PriorityStrategy struct{}

func NewPriorityStrategy() *PriorityStrategy { return &PriorityStrategy{} }

func (s *PriorityStrategy) Name() string { return "priority" }

func (s *PriorityStrategy) Select(ctx context.Context, candidates []*model.BackendModel, req *Request) (*model.BackendModel, error) {
	if len(candidates) == 0 {
		return nil, ErrNoCandidate
	}
	if req != nil && req.Model != "" && req.Model != "auto" {
		for _, m := range candidates {
			if m.ModelID == req.Model {
				return m, nil
			}
		}
	}
	return candidates[0], nil
}

package router

import (
	"context"
	"errors"
	"sync"

	"github.com/free-model-gateway/fmg/internal/model"
)

var ErrNoCandidate = errors.New("no candidate model available")

type RoundRobinStrategy struct {
	mu    sync.Mutex
	index int
}

func NewRoundRobinStrategy() *RoundRobinStrategy { return &RoundRobinStrategy{} }

func (s *RoundRobinStrategy) Name() string { return "round-robin" }

func (s *RoundRobinStrategy) Select(ctx context.Context, candidates []*model.BackendModel, req *Request) (*model.BackendModel, error) {
	if len(candidates) == 0 {
		return nil, ErrNoCandidate
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.index % len(candidates)
	s.index++
	return candidates[idx], nil
}

package router

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/free-model-gateway/fmg/internal/model"
)

type RandomStrategy struct {
	mu  sync.Mutex
	rng *rand.Rand
}

func NewRandomStrategy() *RandomStrategy {
	return &RandomStrategy{rng: rand.New(rand.NewSource(time.Now().UnixNano()))}
}

func (s *RandomStrategy) Name() string { return "random" }

func (s *RandomStrategy) Select(ctx context.Context, candidates []*model.BackendModel, req *Request) (*model.BackendModel, error) {
	if len(candidates) == 0 {
		return nil, ErrNoCandidate
	}
	s.mu.Lock()
	idx := s.rng.Intn(len(candidates))
	s.mu.Unlock()
	return candidates[idx], nil
}

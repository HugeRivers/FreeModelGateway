package router

import (
	"context"

	"github.com/free-model-gateway/fmg/internal/model"
	"github.com/free-model-gateway/fmg/internal/scoring"
)

// ThompsonStrategy uses Thompson Sampling to balance exploration and exploitation.
type ThompsonStrategy struct {
	scorer *scoring.Scorer
	name   string
}

func (s *ThompsonStrategy) Name() string { return s.name }

func (s *ThompsonStrategy) Select(ctx context.Context, candidates []*model.BackendModel, req *Request) (*model.BackendModel, error) {
	if len(candidates) == 0 {
		return nil, ErrNoCandidate
	}

	// For explicit model selection, respect it but still score across all
	// provider instances offering that model so multi-key setups load-balance.
	if req != nil && req.Model != "" && req.Model != "auto" {
		matched := candidates[:0]
		for _, m := range candidates {
			if m.ModelID == req.Model {
				matched = append(matched, m)
			}
		}
		if len(matched) > 0 {
			best := matched[0]
			bestScore := s.scorer.SampleScore(best)
			for i := 1; i < len(matched); i++ {
				score := s.scorer.SampleScore(matched[i])
				if score > bestScore {
					bestScore = score
					best = matched[i]
				}
			}
			return best, nil
		}
	}

	// Sample scores for all candidates and pick the highest
	best := candidates[0]
	bestScore := s.scorer.SampleScore(best)
	for i := 1; i < len(candidates); i++ {
		score := s.scorer.SampleScore(candidates[i])
		if score > bestScore {
			bestScore = score
			best = candidates[i]
		}
	}
	return best, nil
}

func NewThompsonStrategy(weights scoring.StrategyWeights) *ThompsonStrategy {
	return &ThompsonStrategy{
		scorer: scoring.NewScorer(weights),
		name:   "thompson",
	}
}

// SmartestStrategy is a convenience constructor for the "smartest" preset.
func NewSmartestStrategy() *ThompsonStrategy {
	s := NewThompsonStrategy(scoring.Smartest)
	s.name = "smartest"
	return s
}

// FastestStrategy is a convenience constructor for the "fastest" preset.
func NewFastestStrategy() *ThompsonStrategy {
	s := NewThompsonStrategy(scoring.Fastest)
	s.name = "fastest"
	return s
}

// ReliableStrategy is a convenience constructor for the "reliable" preset.
func NewReliableStrategy() *ThompsonStrategy {
	s := NewThompsonStrategy(scoring.Reliable)
	s.name = "reliable"
	return s
}

// BalancedStrategy is a convenience constructor for the "balanced" preset.
func NewBalancedStrategy() *ThompsonStrategy {
	s := NewThompsonStrategy(scoring.Balanced)
	s.name = "balanced"
	return s
}

package scoring

import (
	"math"
	"math/rand"
	"time"

	"github.com/free-model-gateway/fmg/internal/model"
)

// StrategyWeights defines how much each dimension contributes to the score.
type StrategyWeights struct {
	Reliability  float64
	Speed        float64
	Intelligence float64
}

var (
	// Balanced is the default strategy.
	Balanced = StrategyWeights{Reliability: 0.5, Speed: 0.25, Intelligence: 0.25}
	Smartest = StrategyWeights{Reliability: 0.35, Speed: 0.1, Intelligence: 0.55}
	Fastest  = StrategyWeights{Reliability: 0.35, Speed: 0.55, Intelligence: 0.1}
	Reliable = StrategyWeights{Reliability: 0.7, Speed: 0.15, Intelligence: 0.15}
)

// Scorer computes scores for backends using multi-dimensional metrics.
type Scorer struct {
	weights StrategyWeights
	rng     *rand.Rand
}

func NewScorer(weights StrategyWeights) *Scorer {
	return &Scorer{
		weights: weights,
		rng:     rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// SampleScore uses Thompson Sampling for exploration vs exploitation.
// It returns a score drawn from Beta(reliability) + deterministic speed/intelligence.
func (s *Scorer) SampleScore(b *model.BackendModel) float64 {
	// Thompson Sampling on reliability dimension
	alpha, beta := s.betaParams(b)
	sampledReliability := s.rng.Float64()
	if alpha > 0 && beta > 0 {
		sampledReliability = sampleBeta(s.rng, alpha, beta)
	}

	speed := s.speedScore(b)
	intelligence := s.intelligenceScore(b)

	return s.weights.Reliability*sampledReliability +
		s.weights.Speed*speed +
		s.weights.Intelligence*intelligence
}

// betaParams returns Beta distribution parameters with Laplace smoothing.
func (s *Scorer) betaParams(b *model.BackendModel) (alpha, beta float64) {
	// Laplace smoothing: add 1 to both success and failure
	success := float64(b.SuccessCount) + 1
	failure := float64(b.ErrorCount) + 1
	return success, failure
}

// speedScore combines throughput and TTFB.
func (s *Scorer) speedScore(b *model.BackendModel) float64 {
	avgLatency := b.AvgLatencyMs()
	if avgLatency <= 0 {
		return 0.5 // unknown, use neutral
	}

	// throughput score: saturating curve
	// assume 60 tok/sec is "good"
	throughputScore := 1.0 - math.Exp(-float64(avgLatency)/60.0)

	// TTFB score: linear mapping 300ms-5000ms → 1-0
	// (lower latency = higher score)
	ttfbScore := 1.0 - float64(avgLatency)/5000.0
	if ttfbScore < 0 {
		ttfbScore = 0
	}
	if ttfbScore > 1 {
		ttfbScore = 1
	}

	return 0.6*throughputScore + 0.4*ttfbScore
}

func (s *Scorer) intelligenceScore(b *model.BackendModel) float64 {
	return 0.5
}

// sampleBeta generates a Beta-distributed random variable using Gamma distribution.
// If alpha or beta is very small, falls back to uniform.
func sampleBeta(rng *rand.Rand, alpha, beta float64) float64 {
	if alpha <= 0 || beta <= 0 {
		return rng.Float64()
	}
	// Beta(α,β) = Gamma(α,1) / (Gamma(α,1) + Gamma(β,1))
	ga := sampleGamma(rng, alpha, 1.0)
	gb := sampleGamma(rng, beta, 1.0)
	if ga+gb == 0 {
		return 0.5
	}
	return ga / (ga + gb)
}

// sampleGamma generates a Gamma-distributed random variable.
func sampleGamma(rng *rand.Rand, shape, scale float64) float64 {
	if shape < 1 {
		// Use rejection sampling for shape < 1
		return sampleGamma(rng, shape+1, scale) * math.Pow(rng.Float64(), 1.0/shape)
	}

	// Marsaglia-Tsang method for shape >= 1
	d := shape - 1.0/3.0
	c := 1.0 / math.Sqrt(9.0*d)
	for {
		x := rng.NormFloat64()
		v := 1.0 + c*x
		if v <= 0 {
			continue
		}
		v = v * v * v
		u := rng.Float64()
		if u < 1.0-0.0331*(x*x)*(x*x) {
			return d * v * scale
		}
		if math.Log(u) < 0.5*x*x+d*(1.0-v+math.Log(v)) {
			return d * v * scale
		}
	}
}

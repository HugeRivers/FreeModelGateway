package stats

import (
	"sync"
	"time"

	"github.com/free-model-gateway/fmg/internal/model"
)

type ModelStats struct {
	ProviderID    string  `json:"provider_id"`
	ProviderName  string  `json:"provider_name"`
	ModelID       string  `json:"model_id"`
	ModelName     string  `json:"model_name"`
	Priority      int     `json:"priority"`
	Status        string  `json:"status"`
	TotalRequests int64   `json:"total_requests"`
	SuccessCount  int64   `json:"success_count"`
	ErrorCount    int64   `json:"error_count"`
	InputTokens   int64   `json:"input_tokens"`
	OutputTokens  int64   `json:"output_tokens"`
	AvgLatencyMs  int64   `json:"avg_latency_ms"`
	SuccessRate   float64 `json:"success_rate"`
	LastError     string  `json:"last_error,omitempty"`
}

type Stats struct {
	Models            []ModelStats `json:"models"`
	TotalRequests     int64        `json:"total_requests"`
	TotalInputTokens  int64        `json:"total_input_tokens"`
	TotalOutputTokens int64        `json:"total_output_tokens"`
	HealthyCount      int          `json:"healthy_count"`
	CooldownCount     int          `json:"cooldown_count"`
	ProviderCount     int          `json:"provider_count"`
	Timestamp         time.Time    `json:"timestamp"`
}

type Collector struct {
	mu   sync.RWMutex
	pool *model.Pool
}

func NewCollector(pool *model.Pool) *Collector {
	return &Collector{pool: pool}
}

func (c *Collector) Snapshot() Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	models := c.pool.All()
	out := Stats{
		Models:    make([]ModelStats, 0, len(models)),
		Timestamp: time.Now(),
	}
	providers := make(map[string]struct{})
	for _, m := range models {
		providers[m.ProviderID] = struct{}{}
		switch m.Status {
		case model.StatusHealthy:
			out.HealthyCount++
		case model.StatusCooldown:
			out.CooldownCount++
		}
		out.TotalRequests += m.TotalRequests
		out.TotalInputTokens += m.InputTokens
		out.TotalOutputTokens += m.OutputTokens

		var rate float64
		if m.TotalRequests > 0 {
			rate = float64(m.SuccessCount) / float64(m.TotalRequests)
		} else {
			rate = 1.0
		}
		out.Models = append(out.Models, ModelStats{
			ProviderID:    m.ProviderID,
			ProviderName:  m.ProviderName,
			ModelID:       m.ModelID,
			ModelName:     m.ModelName,
			Priority:      m.Priority,
			Status:        string(m.Status),
			TotalRequests: m.TotalRequests,
			SuccessCount:  m.SuccessCount,
			ErrorCount:    m.ErrorCount,
			InputTokens:   m.InputTokens,
			OutputTokens:  m.OutputTokens,
			AvgLatencyMs:  m.AvgLatencyMs(),
			SuccessRate:   rate,
			LastError:     m.LastError,
		})
	}
	out.ProviderCount = len(providers)
	return out
}

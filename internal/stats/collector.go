package stats

import (
	"context"
	"sync"
	"time"

	"github.com/free-model-gateway/fmg/internal/model"
	"github.com/free-model-gateway/fmg/internal/store"
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
	mu    sync.RWMutex
	pool  *model.Pool
	store *store.Store
}

func NewCollector(pool *model.Pool, st *store.Store) *Collector {
	return &Collector{pool: pool, store: st}
}

func (c *Collector) Record(providerID, modelID string, success bool, inputTokens, outputTokens, latencyMs int64, lastError string) {
	if c.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = c.store.RecordRequest(ctx, providerID, modelID, success, inputTokens, outputTokens, latencyMs, lastError)
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

	dbStats := make(map[string]*store.ModelStatRow)
	if c.store != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		rows, err := c.store.GetStatsSince(ctx, time.Now().Add(-24*time.Hour))
		if err == nil {
			for i := range rows {
				key := rows[i].ProviderID + "/" + rows[i].ModelID
				dbStats[key] = &rows[i]
			}
		}
	}

	for _, m := range models {
		providers[m.ProviderID] = struct{}{}
		switch m.Status {
		case model.StatusHealthy:
			out.HealthyCount++
		case model.StatusCooldown:
			out.CooldownCount++
		}

		key := m.ProviderID + "/" + m.ModelID
		var db *store.ModelStatRow
		if v, ok := dbStats[key]; ok {
			db = v
		}

		totalReq := m.TotalRequests
		succCount := m.SuccessCount
		errCount := m.ErrorCount
		inTok := m.InputTokens
		outTok := m.OutputTokens
		avgLat := m.AvgLatencyMs()

		if db != nil {
			totalReq += db.SuccessCount + db.ErrorCount
			succCount += db.SuccessCount
			errCount += db.ErrorCount
			inTok += db.InputTokens
			outTok += db.OutputTokens
			if totalReq > 0 {
				avgLat = ((m.AvgLatencyMs() * m.TotalRequests) + (db.AvgLatencyMs * (db.SuccessCount + db.ErrorCount))) / totalReq
			} else {
				avgLat = db.AvgLatencyMs
			}
		}

		out.TotalRequests += totalReq
		out.TotalInputTokens += inTok
		out.TotalOutputTokens += outTok

		var rate float64
		if totalReq > 0 {
			rate = float64(succCount) / float64(totalReq)
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
			TotalRequests: totalReq,
			SuccessCount:  succCount,
			ErrorCount:    errCount,
			InputTokens:   inTok,
			OutputTokens:  outTok,
			AvgLatencyMs:  avgLat,
			SuccessRate:   rate,
			LastError:     m.LastError,
		})
	}
	out.ProviderCount = len(providers)
	return out
}

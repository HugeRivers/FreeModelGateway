package model

import (
	"sync"
	"time"

	"github.com/free-model-gateway/fmg/internal/config"
)

type RingBuffer struct {
	mu    sync.Mutex
	data  []time.Duration
	cap   int
	index int
	full  bool
}

func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = 100
	}
	return &RingBuffer{data: make([]time.Duration, capacity), cap: capacity}
}

func (r *RingBuffer) Add(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[r.index] = d
	r.index = (r.index + 1) % r.cap
	if r.index == 0 {
		r.full = true
	}
}

func (r *RingBuffer) Avg() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.index
	if r.full {
		n = r.cap
	}
	if n == 0 {
		return 0
	}
	var sum time.Duration
	for i := 0; i < n; i++ {
		sum += r.data[i]
	}
	return sum / time.Duration(n)
}

type BackendModel struct {
	ProviderID    string
	ProviderName  string
	ModelID       string
	ModelName     string
	BaseURL       string
	APIKey        string
	Priority      int
	Weight        int
	ContextWindow int
	OutputLimit   int
	ExtraHeaders  map[string]string

	mu             sync.RWMutex
	Status         Status
	CooldownUntil  time.Time
	SuccessCount   int64
	ErrorCount     int64
	ConsecErrors   int64
	LastError      string
	LastUsedAt     time.Time
	CreatedAt      time.Time
	InputTokens    int64
	OutputTokens   int64
	TotalRequests  int64
	LatencyHistory *RingBuffer
}

func NewBackendModel(p config.ProviderConfig, m config.ModelConfig) *BackendModel {
	return &BackendModel{
		ProviderID:     p.ID,
		ProviderName:   p.Name,
		ModelID:        m.ID,
		ModelName:      m.Name,
		BaseURL:        p.BaseURL,
		APIKey:         p.APIKey,
		Priority:       p.Priority,
		Weight:         p.Weight,
		ContextWindow:  m.ContextWindow,
		OutputLimit:    m.OutputLimit,
		ExtraHeaders:   p.Headers,
		Status:         StatusHealthy,
		CreatedAt:      time.Now(),
		LatencyHistory: NewRingBuffer(1000),
	}
}

func (m *BackendModel) Key() string {
	return m.ProviderID + ":" + m.ModelID
}

func (m *BackendModel) IsAvailable() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.Status != StatusHealthy {
		return false
	}
	if m.CooldownUntil.After(time.Now()) {
		return false
	}
	return true
}

func (m *BackendModel) MarkSuccess(inputTokens, outputTokens int, latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SuccessCount++
	m.TotalRequests++
	m.InputTokens += int64(inputTokens)
	m.OutputTokens += int64(outputTokens)
	m.ConsecErrors = 0
	m.LastUsedAt = time.Now()
	m.LastError = ""
	if latency > 0 {
		m.LatencyHistory.Add(latency)
	}
}

func (m *BackendModel) MarkFailure(err string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ErrorCount++
	m.TotalRequests++
	m.ConsecErrors++
	m.LastError = err
	m.LastUsedAt = time.Now()
}

func (m *BackendModel) EnterCooldown(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Status = StatusCooldown
	m.CooldownUntil = time.Now().Add(d)
}

func (m *BackendModel) Recover() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Status = StatusHealthy
	m.CooldownUntil = time.Time{}
	m.ConsecErrors = 0
}

func (m *BackendModel) AvgLatencyMs() int64 {
	avg := m.LatencyHistory.Avg()
	if avg <= 0 {
		return 0
	}
	return avg.Milliseconds()
}

func (m *BackendModel) ToAPIModel() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	stats := map[string]interface{}{
		"total_requests": m.TotalRequests,
		"success_count":  m.SuccessCount,
		"error_count":    m.ErrorCount,
		"input_tokens":   m.InputTokens,
		"output_tokens":  m.OutputTokens,
		"avg_latency_ms": m.AvgLatencyMs(),
	}
	if m.TotalRequests > 0 {
		stats["success_rate"] = float64(m.SuccessCount) / float64(m.TotalRequests)
	} else {
		stats["success_rate"] = 1.0
	}
	return map[string]interface{}{
		"id":             m.ModelID,
		"object":         "model",
		"owned_by":       m.ProviderName,
		"status":         string(m.Status),
		"priority":       m.Priority,
		"context_window": m.ContextWindow,
		"output_limit":   m.OutputLimit,
		"statistics":     stats,
	}
}

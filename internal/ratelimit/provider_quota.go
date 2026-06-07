package ratelimit

import (
	"sync"
	"time"
)

// ProviderQuota tracks daily usage per provider.
type ProviderQuota struct {
	mu        sync.RWMutex
	dailyReq  map[string]int64 // provider -> daily request count
	dailyTok  map[string]int64 // provider -> daily token count
	lastReset time.Time
}

func NewProviderQuota() *ProviderQuota {
	return &ProviderQuota{
		dailyReq:  make(map[string]int64),
		dailyTok:  make(map[string]int64),
		lastReset: time.Now(),
	}
}

// RecordRequest records a request for the provider.
func (p *ProviderQuota) RecordRequest(provider string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dailyReq[provider]++
}

// RecordTokens records tokens for the provider.
func (p *ProviderQuota) RecordTokens(provider string, tokens int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dailyTok[provider] += tokens
}

// CanUseProvider checks if provider is under its daily cap.
func (p *ProviderQuota) CanUseProvider(provider string, dailyCap int) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if dailyCap <= 0 {
		return true
	}
	return p.dailyReq[provider] < int64(dailyCap)
}

// DailyUsage returns current daily request count for provider.
func (p *ProviderQuota) DailyUsage(provider string) int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.dailyReq[provider]
}

// ResetIfNeeded resets counters if day has changed.
func (p *ProviderQuota) ResetIfNeeded() {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	if now.Day() != p.lastReset.Day() || now.Month() != p.lastReset.Month() || now.Year() != p.lastReset.Year() {
		p.dailyReq = make(map[string]int64)
		p.dailyTok = make(map[string]int64)
		p.lastReset = now
	}
}

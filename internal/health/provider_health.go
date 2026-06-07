package health

import (
	"sync"
	"time"
)

// ProviderHealth tracks provider-level health state, especially 429 patterns.
type ProviderHealth struct {
	Platform        string
	Last429At       time.Time
	Consecutive429s int
	Total429s       int
	IsInCooldown    bool
	CooldownUntil   time.Time
}

func (h *ProviderHealth) ShouldSkip(now time.Time) bool {
	if h.IsInCooldown && now.Before(h.CooldownUntil) {
		return true
	}
	// Progressive skip: if this provider has had 2+ consecutive 429s within last 5min,
	// skip it to avoid wasting retries.
	if h.Consecutive429s >= 2 && now.Sub(h.Last429At) < 5*time.Minute {
		return true
	}
	return false
}

func (h *ProviderHealth) Record429() {
	h.Total429s++
	// Only increment consecutive if within a 5-minute window; otherwise reset.
	if time.Since(h.Last429At) < 5*time.Minute {
		h.Consecutive429s++
	} else {
		h.Consecutive429s = 1
	}
	h.Last429At = time.Now()
}

func (h *ProviderHealth) SetCooldown(d time.Duration) {
	h.IsInCooldown = true
	h.CooldownUntil = time.Now().Add(d)
}

func (h *ProviderHealth) ClearCooldown() {
	h.IsInCooldown = false
	h.CooldownUntil = time.Time{}
	h.Consecutive429s = 0
}

// Tracker manages health state for all providers.
type Tracker struct {
	mu        sync.RWMutex
	providers map[string]*ProviderHealth
	// Threshold to trigger provider-level cooldown after N consecutive 429s.
	consecutiveThreshold int
	// Cooldown duration applied at provider level.
	cooldownDuration time.Duration
}

func NewTracker(threshold int, cooldownDuration time.Duration) *Tracker {
	if threshold <= 0 {
		threshold = 2
	}
	if cooldownDuration <= 0 {
		cooldownDuration = 5 * time.Minute
	}
	return &Tracker{
		providers:            make(map[string]*ProviderHealth),
		consecutiveThreshold: threshold,
		cooldownDuration:     cooldownDuration,
	}
}

func (t *Tracker) getOrCreate(platform string) *ProviderHealth {
	h, ok := t.providers[platform]
	if !ok {
		h = &ProviderHealth{Platform: platform}
		t.providers[platform] = h
	}
	return h
}

// Record429 records a 429 for the given provider and returns whether
// the provider should be skipped going forward (entered cooldown).
func (t *Tracker) Record429(platform string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	h := t.getOrCreate(platform)
	h.Record429()
	// If threshold reached, set provider-level cooldown.
	if h.Consecutive429s >= t.consecutiveThreshold {
		h.SetCooldown(t.cooldownDuration)
		return true
	}
	return false
}

// ShouldSkip returns true if the provider is currently in cooldown
// or has recently had too many consecutive 429s.
func (t *Tracker) ShouldSkip(platform string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	h, ok := t.providers[platform]
	if !ok {
		return false
	}
	return h.ShouldSkip(time.Now())
}

// Get returns the health state for a provider (or nil).
func (t *Tracker) Get(platform string) *ProviderHealth {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.providers[platform]
}

// ClearCooldown explicitly clears cooldown for a provider (e.g. via admin API).
func (t *Tracker) ClearCooldown(platform string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if h, ok := t.providers[platform]; ok {
		h.ClearCooldown()
	}
}

// All returns a snapshot of all tracked provider health states.
func (t *Tracker) All() map[string]*ProviderHealth {
	t.mu.RLock()
	defer t.mu.RUnlock()

	out := make(map[string]*ProviderHealth, len(t.providers))
	for k, v := range t.providers {
		// shallow copy
		out[k] = &ProviderHealth{
			Platform:        v.Platform,
			Last429At:       v.Last429At,
			Consecutive429s: v.Consecutive429s,
			Total429s:       v.Total429s,
			IsInCooldown:    v.IsInCooldown,
			CooldownUntil:   v.CooldownUntil,
		}
	}
	return out
}

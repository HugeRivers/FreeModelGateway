package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/free-model-gateway/fmg/internal/store"
)

// LimiterKind 限流维度
type LimiterKind string

const (
	KindRPM LimiterKind = "rpm"
	KindRPD LimiterKind = "rpd"
	KindTPM LimiterKind = "tpm"
	KindTPD LimiterKind = "tpd"
)

// Limiter 提供内存滑动窗口限流 + SQLite 持久化备份。
type Limiter struct {
	mu      sync.RWMutex
	windows map[string]*Window // key: "platform:modelId:kind"
	quota   *ProviderQuota
	store   *store.Store
	logFunc func(string, map[string]interface{})
}

func NewLimiter(st *store.Store) *Limiter {
	return &Limiter{
		windows: make(map[string]*Window),
		quota:   NewProviderQuota(),
		store:   st,
	}
}

// SetLogFunc sets an optional logging function.
func (l *Limiter) SetLogFunc(f func(string, map[string]interface{})) {
	l.logFunc = f
}

// windowKey generates the key for a window.
func windowKey(platform, modelID string, kind LimiterKind) string {
	return fmt.Sprintf("%s:%s:%s", platform, modelID, kind)
}

// getOrCreateWindow returns the window for the given key.
func (l *Limiter) getOrCreateWindow(key string) *Window {
	l.mu.Lock()
	defer l.mu.Unlock()
	w, ok := l.windows[key]
	if !ok {
		w = newWindow()
		l.windows[key] = w
	}
	return w
}

// AllowRequest checks if a request is allowed under rate limits.
// Returns true if allowed, false if rate limited.
func (l *Limiter) AllowRequest(platform, modelID string, rpmLimit, rpdLimit int) bool {
	l.quota.ResetIfNeeded()

	now := time.Now()
	w := l.getOrCreateWindow(windowKey(platform, modelID, KindRPM))
	if rpmLimit > 0 && w.countRequests(now, time.Minute) >= rpmLimit {
		return false
	}

	w = l.getOrCreateWindow(windowKey(platform, modelID, KindRPD))
	if rpdLimit > 0 && w.countRequests(now, 24*time.Hour) >= rpdLimit {
		return false
	}

	return true
}

// AllowTokens checks if tokens are allowed under rate limits.
func (l *Limiter) AllowTokens(platform, modelID string, tokens int, tpmLimit, tpdLimit int) bool {
	now := time.Now()
	w := l.getOrCreateWindow(windowKey(platform, modelID, KindTPM))
	if tpmLimit > 0 {
		current := w.countTokens(now, time.Minute)
		if current+int64(tokens) > int64(tpmLimit) {
			return false
		}
	}

	w = l.getOrCreateWindow(windowKey(platform, modelID, KindTPD))
	if tpdLimit > 0 {
		current := w.countTokens(now, 24*time.Hour)
		if current+int64(tokens) > int64(tpdLimit) {
			return false
		}
	}

	return true
}

// RecordRequest records a request in both memory and SQLite.
func (l *Limiter) RecordRequest(ctx context.Context, platform, modelID string, tokens int) {
	now := time.Now()
	w := l.getOrCreateWindow(windowKey(platform, modelID, KindRPM))
	w.recordRequest(now)
	w = l.getOrCreateWindow(windowKey(platform, modelID, KindRPD))
	w.recordRequest(now)

	l.quota.RecordRequest(platform)

	if tokens > 0 {
		w = l.getOrCreateWindow(windowKey(platform, modelID, KindTPM))
		w.recordTokens(now, int64(tokens))
		w = l.getOrCreateWindow(windowKey(platform, modelID, KindTPD))
		w.recordTokens(now, int64(tokens))
	}

	// Persist to SQLite
	if l.store != nil {
		_ = l.store.RecordRateLimitUsage(ctx, platform, modelID, "request", 0)
		if tokens > 0 {
			_ = l.store.RecordRateLimitUsage(ctx, platform, modelID, "tokens", int64(tokens))
		}
	}
}

// CanUseProvider checks provider-wide daily quota.
func (l *Limiter) CanUseProvider(provider string, dailyCap int) bool {
	l.quota.ResetIfNeeded()
	return l.quota.CanUseProvider(provider, dailyCap)
}

// ProviderDailyUsage returns current daily request count.
func (l *Limiter) ProviderDailyUsage(provider string) int64 {
	return l.quota.DailyUsage(provider)
}

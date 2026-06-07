package ratelimit

import (
	"sync"
	"time"
)

// Window tracks requests within a sliding time window.
type Window struct {
	mu         sync.RWMutex
	timestamps []int64 // request timestamps in ms
	tokenCount int64   // token count in current window
	tokenTs    []tokenEntry
}

type tokenEntry struct {
	ts     int64
	tokens int64
}

func newWindow() *Window {
	return &Window{
		timestamps: make([]int64, 0, 100),
		tokenTs:    make([]tokenEntry, 0, 100),
	}
}

// recordRequest adds a request to the window.
func (w *Window) recordRequest(now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.timestamps = append(w.timestamps, now.UnixMilli())
}

// recordTokens adds tokens to the window.
func (w *Window) recordTokens(now time.Time, tokens int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.tokenTs = append(w.tokenTs, tokenEntry{ts: now.UnixMilli(), tokens: tokens})
}

// countRequests returns the number of requests in the last duration.
func (w *Window) countRequests(now time.Time, duration time.Duration) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	cutoff := now.Add(-duration).UnixMilli()
	w.timestamps = filterInt64(w.timestamps, cutoff)
	return len(w.timestamps)
}

// countTokens returns the total tokens in the last duration.
func (w *Window) countTokens(now time.Time, duration time.Duration) int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	cutoff := now.Add(-duration).UnixMilli()
	w.tokenTs = filterTokenEntries(w.tokenTs, cutoff)
	var total int64
	for _, e := range w.tokenTs {
		total += e.tokens
	}
	return total
}

func filterInt64(slice []int64, cutoff int64) []int64 {
	idx := 0
	for i, v := range slice {
		if v >= cutoff {
			idx = i
			break
		}
	}
	if idx == 0 && len(slice) > 0 && slice[0] < cutoff {
		return slice[:0]
	}
	return slice[idx:]
}

func filterTokenEntries(slice []tokenEntry, cutoff int64) []tokenEntry {
	idx := 0
	for i, v := range slice {
		if v.ts >= cutoff {
			idx = i
			break
		}
	}
	if idx == 0 && len(slice) > 0 && slice[0].ts < cutoff {
		return slice[:0]
	}
	return slice[idx:]
}

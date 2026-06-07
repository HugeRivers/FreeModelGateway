package router

import (
	"crypto/sha1"
	"encoding/hex"
	"sync"
	"time"
)

const stickyTTL = 30 * time.Minute

// StickySession tracks preferred models for conversation continuity.
type StickySession struct {
	mu       sync.RWMutex
	sessions map[string]*stickyEntry
}

type stickyEntry struct {
	modelKey  string
	expiresAt time.Time
}

func NewStickySession() *StickySession {
	return &StickySession{
		sessions: make(map[string]*stickyEntry),
	}
}

// Get returns the preferred model key for a session (or empty string if none / expired).
func (s *StickySession) Get(sessionKey string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.sessions[sessionKey]
	if !ok {
		return ""
	}
	if time.Now().After(entry.expiresAt) {
		return ""
	}
	return entry.modelKey
}

// Set records the preferred model for a session.
func (s *StickySession) Set(sessionKey, modelKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sessionKey] = &stickyEntry{
		modelKey:  modelKey,
		expiresAt: time.Now().Add(stickyTTL),
	}
}

// Prune removes expired sessions.
func (s *StickySession) Prune() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, v := range s.sessions {
		if now.After(v.expiresAt) {
			delete(s.sessions, k)
		}
	}
}

// sessionKey generates a session key from messages.
// Returns empty string if no user message found.
func sessionKey(messages []Message) string {
	if len(messages) == 0 {
		return ""
	}
	var firstUser string
	for _, m := range messages {
		if m.Role == "user" {
			firstUser = contentToString(m.Content)
			break
		}
	}
	if firstUser == "" {
		return ""
	}

	// Hash first user message + conversation length indicator
	isMulti := len(messages) > 2
	suffix := "single"
	if isMulti {
		suffix = "multi"
	}

	h := sha1.New()
	_, _ = h.Write([]byte(firstUser + ":" + suffix))
	return hex.EncodeToString(h.Sum(nil))
}

// contentToString converts interface{} content to string.
func contentToString(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var result string
		for _, item := range v {
			result += contentToString(item)
		}
		return result
	default:
		return ""
	}
}

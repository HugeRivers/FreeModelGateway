package config

import (
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/sirupsen/logrus"
)

// Reloader holds the current configuration and supports hot-reload.
type Reloader struct {
	mu       sync.RWMutex
	current  *Config
	path     string
	changeCh chan *Config
}

// NewReloader builds a Reloader with an initial config.
func NewReloader(initial *Config) *Reloader {
	return &Reloader{current: initial, changeCh: make(chan *Config, 8)}
}

// Current returns the current config snapshot (safe for concurrent reads).
func (r *Reloader) Current() *Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.current
}

// Reload swaps the current config atomically.
func (r *Reloader) Reload(cfg *Config) {
	r.mu.Lock()
	r.current = cfg
	r.mu.Unlock()
	select {
	case r.changeCh <- cfg:
	default:
	}
}

func (r *Reloader) WatchSIGHUP(log *logrus.Logger) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	go func() {
		for range ch {
			if log != nil {
				cfg := r.Current()
				log.WithFields(logrus.Fields{
					"providers": len(cfg.Providers),
					"port":      cfg.Gateway.Port,
					"mode":      cfg.Strategy.Mode,
				}).Info("SIGHUP received")
			}
		}
	}()
}

func (r *Reloader) Changes() <-chan *Config { return r.changeCh }

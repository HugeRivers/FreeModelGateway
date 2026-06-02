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

// Reload re-reads the config from disk, validates, and swaps atomically.
func (r *Reloader) Reload(path string) error {
	cfg, err := Load(path)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.current = cfg
	r.path = path
	r.mu.Unlock()
	select {
	case r.changeCh <- cfg:
	default:
	}
	return nil
}

// WatchSIGHUP installs a SIGHUP handler that re-reads the config from path.
// On successful reload, the new config is logged at info level; on error,
// the failure is logged and the previous config is preserved.
func (r *Reloader) WatchSIGHUP(path string, log *logrus.Logger) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	go func() {
		for range ch {
			if err := r.Reload(path); err != nil {
				if log != nil {
					log.WithError(err).Error("config reload failed via SIGHUP; keeping previous config")
				}
				continue
			}
			if log != nil {
				cfg := r.Current()
				log.WithFields(logrus.Fields{
					"providers": len(cfg.Providers),
					"port":      cfg.Gateway.Port,
					"mode":      cfg.Strategy.Mode,
				}).Info("config reloaded via SIGHUP")
			}
		}
	}()
}

func (r *Reloader) Changes() <-chan *Config { return r.changeCh }

package health

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/free-model-gateway/fmg/internal/model"
	"github.com/free-model-gateway/fmg/internal/provider"
	"github.com/sirupsen/logrus"
)

const (
	checkInterval                = 5 * time.Minute
	consecutiveFailuresToDisable = 3
)

// Checker performs periodic health checks on all backends.
type Checker struct {
	pool     *model.Pool
	registry *provider.Registry
	log      *logrus.Logger
	mu       sync.RWMutex
	checking map[string]bool
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

func NewChecker(pool *model.Pool, registry *provider.Registry, log *logrus.Logger) *Checker {
	return &Checker{
		pool:     pool,
		registry: registry,
		log:      log,
		checking: make(map[string]bool),
		stopCh:   make(chan struct{}),
	}
}

// Start begins the periodic health check loop.
func (c *Checker) Start() {
	c.wg.Add(1)
	go c.loop()
}

// Stop gracefully shuts down the checker.
func (c *Checker) Stop() {
	close(c.stopCh)
	c.wg.Wait()
}

func (c *Checker) loop() {
	defer c.wg.Done()

	// Initial check
	c.checkAll()

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.checkAll()
		}
	}
}

func (c *Checker) checkAll() {
	models := c.pool.All()
	for _, m := range models {
		if c.isChecking(m.Key()) {
			continue
		}
		c.wg.Add(1)
		go func(backend *model.BackendModel) {
			defer c.wg.Done()
			c.checkOne(backend)
		}(m)
	}
}

func (c *Checker) isChecking(key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.checking[key]
}

func (c *Checker) setChecking(key string, v bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checking[key] = v
}

func (c *Checker) checkOne(backend *model.BackendModel) {
	key := backend.Key()
	c.setChecking(key, true)
	defer c.setChecking(key, false)

	if backend.Status == model.StatusCooldown || backend.Status == model.StatusInvalid {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	adapter := c.registry.Get(backend.APIFormat)
	err := adapter.Probe(ctx, backend)

	if err == nil {
		backend.MarkSuccess(0, 0, 0)
		if backend.Status != model.StatusHealthy {
			backend.Recover()
		}
		return
	}

	// Classify error
	status := classifyError(err)
	switch status {
	case model.StatusInvalid:
		backend.Status = model.StatusInvalid
		c.log.WithFields(logrus.Fields{
			"provider": backend.ProviderID,
			"model":    backend.ModelID,
			"error":    err.Error(),
		}).Warn("[HEALTH] backend marked invalid")
	case model.StatusExhausted:
		backend.MarkFailure(err.Error())
		if backend.ConsecErrors >= consecutiveFailuresToDisable {
			backend.Status = model.StatusExhausted
			c.log.WithFields(logrus.Fields{
				"provider":      backend.ProviderID,
				"model":         backend.ModelID,
				"consec_errors": backend.ConsecErrors,
			}).Warn("[HEALTH] backend auto-disabled after consecutive failures")
		}
	}
}

func classifyError(err error) model.Status {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "401"), strings.Contains(msg, "403"):
		return model.StatusInvalid
	case strings.Contains(msg, "429"):
		return model.StatusExhausted
	case strings.Contains(msg, "tls"), strings.Contains(msg, "certificate"):
		return model.StatusExhausted
	case strings.Contains(msg, "dns"), strings.Contains(msg, "notfound"):
		return model.StatusExhausted
	default:
		return model.StatusHealthy
	}
}

// CheckAllOnce performs a one-off health check (used by admin API).
func (c *Checker) CheckAllOnce() {
	c.checkAll()
}

// CheckOneOnce checks a single backend (used by admin API).
func (c *Checker) CheckOneOnce(providerID, modelID string) error {
	backend, err := c.pool.Get(providerID, modelID)
	if err != nil {
		return err
	}
	c.checkOne(backend)
	return nil
}

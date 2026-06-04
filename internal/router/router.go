package router

import (
	"context"
	"sync"
	"time"

	"github.com/free-model-gateway/fmg/internal/config"
	"github.com/free-model-gateway/fmg/internal/cooldown"
	"github.com/free-model-gateway/fmg/internal/model"
	"github.com/free-model-gateway/fmg/internal/proxy"
	"github.com/free-model-gateway/fmg/internal/stats"
	"github.com/sirupsen/logrus"
)

type Result struct {
	Success       bool
	Response      []byte
	Model         *model.BackendModel
	FallbackChain []string
	Retries       int
	Latency       time.Duration
	Usage         *proxy.Usage
	Error         error
	ErrorStatus   int
}

type Router struct {
	pool        *model.Pool
	strategy    Strategy
	cooldownMgr *cooldown.Manager
	stats       *stats.Collector
	forwarder   *proxy.Forwarder
	log         *logrus.Logger
	maxRetries  int
	retryDelay  time.Duration

	forcedMu       sync.RWMutex
	forcedModelKey string // "providerID:modelID" or empty = auto
	forcedModelID  string
	forcedProvID   string

	lastUsedMu    sync.RWMutex
	lastUsedProv  string
	lastUsedModel string
	lastUsedName  string
}

func NewRouter(pool *model.Pool, mgr *cooldown.Manager, sc *stats.Collector, fw *proxy.Forwarder, stratCfg config.StrategyConfig, gwCfg config.GatewayConfig, log *logrus.Logger) *Router {
	r := &Router{
		pool:        pool,
		cooldownMgr: mgr,
		stats:       sc,
		forwarder:   fw,
		log:         log,
		maxRetries:  gwCfg.MaxRetries,
		retryDelay:  time.Duration(gwCfg.RetryDelayMs) * time.Millisecond,
	}
	r.SetStrategy(stratCfg.Mode)
	return r
}

func (r *Router) SetStrategy(mode string) {
	switch mode {
	case "round-robin":
		r.strategy = NewRoundRobinStrategy()
	case "weighted-rr":
		r.strategy = NewWeightedRRStrategy()
	case "random":
		r.strategy = NewRandomStrategy()
	default:
		r.strategy = NewPriorityStrategy()
	}
}

func (r *Router) StrategyName() string {
	if r.strategy == nil {
		return "priority"
	}
	return r.strategy.Name()
}

// ForceModel forces all subsequent Route/RouteStream calls to use the given model.
// Pass empty strings to clear (back to auto/strategy-based routing).
func (r *Router) ForceModel(providerID, modelID string) {
	r.forcedMu.Lock()
	defer r.forcedMu.Unlock()
	r.forcedProvID = providerID
	r.forcedModelID = modelID
	r.forcedModelKey = providerID + ":" + modelID
}

func (r *Router) ClearForcedModel() {
	r.forcedMu.Lock()
	defer r.forcedMu.Unlock()
	r.forcedProvID = ""
	r.forcedModelID = ""
	r.forcedModelKey = ""
}

func (r *Router) ForcedModelKey() string {
	r.forcedMu.RLock()
	defer r.forcedMu.RUnlock()
	return r.forcedModelKey
}

func (r *Router) ForcedModelIDs() (string, string) {
	r.forcedMu.RLock()
	defer r.forcedMu.RUnlock()
	return r.forcedProvID, r.forcedModelID
}

// ProbeModel checks if the given backend is reachable within the given timeout.
func (r *Router) ProbeModel(providerID, modelID string, timeout time.Duration) error {
	backend, err := r.pool.Get(providerID, modelID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return r.forwarder.Probe(ctx, backend)
}

func (r *Router) RecordLastUsed(providerID, modelID, modelName string) {
	r.lastUsedMu.Lock()
	defer r.lastUsedMu.Unlock()
	r.lastUsedProv = providerID
	r.lastUsedModel = modelID
	r.lastUsedName = modelName
}

func (r *Router) LastUsedModel() (string, string, string) {
	r.lastUsedMu.RLock()
	defer r.lastUsedMu.RUnlock()
	return r.lastUsedProv, r.lastUsedModel, r.lastUsedName
}

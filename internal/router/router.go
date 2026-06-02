package router

import (
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

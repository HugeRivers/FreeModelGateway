package api

import (
	"net/http"
	"time"

	"github.com/free-model-gateway/fmg/internal/config"
	"github.com/free-model-gateway/fmg/internal/health"
	"github.com/free-model-gateway/fmg/internal/model"
	"github.com/free-model-gateway/fmg/internal/router"
	"github.com/free-model-gateway/fmg/internal/stats"
	"github.com/free-model-gateway/fmg/internal/store"
	"github.com/gin-gonic/gin"
)

type DashboardHandler struct {
	router        *router.Router
	pool          *model.Pool
	stats         *stats.Collector
	healthTracker *health.Tracker
	reloader      *config.Reloader
	store         *store.Store
	version       string
}

func NewDashboard(r *router.Router, p *model.Pool, s *stats.Collector, ht *health.Tracker, reloader *config.Reloader, st *store.Store, version string) *DashboardHandler {
	return &DashboardHandler{
		router:        r,
		pool:          p,
		stats:         s,
		healthTracker: ht,
		reloader:      reloader,
		store:         st,
		version:       version,
	}
}

func (d *DashboardHandler) DashboardAPI(c *gin.Context) {
	all := d.pool.All()
	healthy, cooldown := 0, 0
	for _, m := range all {
		switch m.Status {
		case model.StatusHealthy:
			healthy++
		case model.StatusCooldown:
			cooldown++
		}
	}

	var forcedModel *ForcedModelInfo
	provID, modelID := d.router.ForcedModelIDs()
	if provID != "" && modelID != "" {
		if m, err := d.pool.Get(provID, modelID); err == nil {
			forcedModel = &ForcedModelInfo{
				ProviderID:   provID,
				ProviderName: m.ProviderName,
				ModelID:      modelID,
				ModelName:    m.ModelName,
			}
		}
	}

	var lastUsed *LastUsedInfo
	lProv, lModel, lName := d.router.LastUsedModel()
	if lProv != "" && lModel != "" {
		lastUsed = &LastUsedInfo{
			ProviderID:   lProv,
			ProviderName: lProv,
			ModelID:      lModel,
			ModelName:    lName,
		}
	}

	c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
	c.JSON(http.StatusOK, gin.H{
		"health": gin.H{
			"status":         "ok",
			"version":        d.version,
			"models_total":   len(all),
			"healthy":        healthy,
			"cooldown":       cooldown,
			"strategy":       d.router.StrategyName(),
			"providers":      len(d.pool.ProviderSummary()),
			"uptime_seconds": int64(time.Since(startTime).Seconds()),
		},
		"stats":        d.stats.Snapshot(),
		"strategy":     d.router.StrategyName(),
		"version":      d.version,
		"forced_model": forcedModel,
		"last_used":    lastUsed,
	})
}

type ForcedModelInfo struct {
	ProviderID   string `json:"provider_id"`
	ProviderName string `json:"provider_name"`
	ModelID      string `json:"model_id"`
	ModelName    string `json:"model_name"`
}

type LastUsedInfo struct {
	ProviderID   string `json:"provider_id"`
	ProviderName string `json:"provider_name"`
	ModelID      string `json:"model_id"`
	ModelName    string `json:"model_name"`
}

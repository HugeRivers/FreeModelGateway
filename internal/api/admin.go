package api

import (
	"net/http"

	"github.com/free-model-gateway/fmg/internal/config"
	"github.com/free-model-gateway/fmg/internal/model"
	"github.com/free-model-gateway/fmg/internal/router"
	"github.com/free-model-gateway/fmg/internal/store"
	"github.com/gin-gonic/gin"
)

type AdminHandler struct {
	pool     *model.Pool
	reloader *config.Reloader
	r        *router.Router
	store    *store.Store
}

func NewAdmin(pool *model.Pool, reloader *config.Reloader, r *router.Router, st *store.Store) *AdminHandler {
	return &AdminHandler{pool: pool, reloader: reloader, r: r, store: st}
}

func (a *AdminHandler) Recover(c *gin.Context) {
	var body struct {
		ProviderID string `json:"provider_id"`
		ModelID    string `json:"model_id"`
	}
	_ = c.ShouldBindJSON(&body)

	count := 0
	if body.ProviderID == "" && body.ModelID == "" {
		count = a.pool.RecoverAll(nil)
	} else {
		for _, m := range a.pool.All() {
			if (body.ProviderID == "" || m.ProviderID == body.ProviderID) &&
				(body.ModelID == "" || m.ModelID == body.ModelID) {
				m.Recover()
				count++
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"recovered": count})
}

func (a *AdminHandler) Reload(c *gin.Context) {
	if a.store == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "store not available"})
		return
	}
	cfg, err := config.LoadConfigFromDB(c.Request.Context(), a.store, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	a.reloader.Reload(cfg)
	c.JSON(http.StatusOK, gin.H{
		"reloaded":  true,
		"providers": len(cfg.Providers),
		"port":      cfg.Gateway.Port,
		"mode":      cfg.Strategy.Mode,
	})
}

func (a *AdminHandler) SwitchStrategy(c *gin.Context) {
	var body struct {
		Mode string `json:"mode"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Mode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "mode is required"})
		return
	}
	a.r.SetStrategy(body.Mode)
	a.r.ClearForcedModel()

	if a.store != nil {
		_ = a.store.SaveRouteConfig(c.Request.Context(), &store.RouteConfig{
			Mode:     "auto",
			Strategy: body.Mode,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"switched": true,
		"mode":     a.r.StrategyName(),
	})
}

func (a *AdminHandler) Providers(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"providers": a.pool.ProviderSummary(),
	})
}

type switchRequest struct {
	ProviderID string `json:"provider_id"`
	ModelID    string `json:"model_id"`
	Auto       bool   `json:"auto"`
}

func (a *AdminHandler) SwitchModel(c *gin.Context) {
	var body switchRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON: " + err.Error()})
		return
	}

	if body.Auto {
		a.r.ClearForcedModel()
		if a.store != nil {
			_ = a.store.SaveRouteConfig(c.Request.Context(), &store.RouteConfig{
				Mode:     "auto",
				Strategy: a.r.StrategyName(),
			})
		}
		c.JSON(http.StatusOK, gin.H{"mode": "auto", "forced_model": nil})
		return
	}

	if body.ProviderID == "" || body.ModelID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "must specify provider_id and model_id, or auto=true"})
		return
	}

	m, err := a.pool.Get(body.ProviderID, body.ModelID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "model not found: " + body.ProviderID + "/" + body.ModelID})
		return
	}

	a.r.ForceModel(body.ProviderID, body.ModelID)
	a.r.RecordLastUsed(body.ProviderID, body.ModelID, m.ModelName)

	if a.store != nil {
		_ = a.store.SaveRouteConfig(c.Request.Context(), &store.RouteConfig{
			Mode:             "manual",
			Strategy:         a.r.StrategyName(),
			ForcedProviderID: body.ProviderID,
			ForcedModelID:    body.ModelID,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"mode": "manual",
		"forced_model": gin.H{
			"provider_id":   body.ProviderID,
			"provider_name": m.ProviderName,
			"model_id":      body.ModelID,
			"model_name":    m.ModelName,
		},
	})
}

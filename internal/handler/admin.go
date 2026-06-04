package handler

import (
	"net/http"
	"time"

	"github.com/free-model-gateway/fmg/internal/config"
	"github.com/free-model-gateway/fmg/internal/model"
	"github.com/free-model-gateway/fmg/internal/router"
	"github.com/gin-gonic/gin"
)

type AdminHandler struct {
	pool     *model.Pool
	reloader *config.Reloader
	cfgPath  string
	envPath  string
	r        *router.Router
}

func NewAdmin(pool *model.Pool, reloader *config.Reloader, cfgPath, envPath string, r *router.Router) *AdminHandler {
	return &AdminHandler{pool: pool, reloader: reloader, cfgPath: cfgPath, envPath: envPath, r: r}
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
	// Re-read .env first so newly-added API keys take effect.
	config.LoadEnvFile(a.envPath)

	if err := a.reloader.Reload(a.cfgPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	cfg := a.reloader.Current()
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "mode is required (priority, round-robin, weighted-rr, random)"})
		return
	}
	a.r.SetStrategy(body.Mode)
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
		c.JSON(http.StatusOK, gin.H{"mode": "auto", "forced_model": nil})
		return
	}

	if body.ProviderID == "" || body.ModelID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "must specify provider_id and model_id, or auto=true"})
		return
	}

	if err := a.r.ProbeModel(body.ProviderID, body.ModelID, 5*time.Second); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":    "probe failed: " + err.Error(),
			"mode":     "auto",
			"model_id": body.ModelID,
		})
		return
	}

	a.r.ForceModel(body.ProviderID, body.ModelID)
	c.JSON(http.StatusOK, gin.H{
		"mode": "manual",
		"forced_model": gin.H{
			"provider_id": body.ProviderID,
			"model_id":    body.ModelID,
		},
	})
}

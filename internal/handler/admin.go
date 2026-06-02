package handler

import (
	"net/http"

	"github.com/free-model-gateway/fmg/internal/config"
	"github.com/free-model-gateway/fmg/internal/model"
	"github.com/gin-gonic/gin"
)

type AdminHandler struct {
	pool     *model.Pool
	reloader *config.Reloader
	cfgPath  string
}

func NewAdmin(pool *model.Pool, reloader *config.Reloader, cfgPath string) *AdminHandler {
	return &AdminHandler{pool: pool, reloader: reloader, cfgPath: cfgPath}
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

func (a *AdminHandler) Providers(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"providers": a.pool.ProviderSummary(),
	})
}

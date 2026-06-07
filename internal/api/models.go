package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func (h *Handler) ListModels(c *gin.Context) {
	models := h.pool.All()
	data := make([]map[string]interface{}, 0, len(models))
	for _, m := range models {
		data = append(data, m.ToAPIModel())
	}
	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   data,
	})
}

func (h *Handler) Health(c *gin.Context) {
	all := h.pool.All()
	healthy, cooldown := 0, 0
	for _, m := range all {
		switch m.Status {
		case "healthy":
			healthy++
		case "cooldown":
			cooldown++
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"status":         "ok",
		"uptime_seconds": int64(time.Since(startTime).Seconds()),
		"version":        h.version,
		"models_total":   len(all),
		"healthy":        healthy,
		"cooldown":       cooldown,
	})
}

func (h *Handler) Stats(c *gin.Context) {
	c.JSON(http.StatusOK, h.stats.Snapshot())
}

package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (h *Handler) Stats(c *gin.Context) {
	c.JSON(http.StatusOK, h.stats.Snapshot())
}

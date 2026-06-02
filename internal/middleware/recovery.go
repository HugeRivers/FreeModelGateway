package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func Recovery(log *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				if log != nil {
					log.WithFields(logrus.Fields{
						"panic":  r,
						"stack":  string(debug.Stack()),
						"path":   c.Request.URL.Path,
						"method": c.Request.Method,
					}).Error("panic recovered in handler")
				}
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error": gin.H{
						"message": "internal server error",
						"type":    "internal_error",
						"code":    "panic_recovered",
					},
				})
			}
		}()
		c.Next()
	}
}

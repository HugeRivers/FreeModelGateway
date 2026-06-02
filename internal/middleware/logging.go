package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func Logging(log *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		latency := time.Since(start)

		entry := log.WithFields(logrus.Fields{
			"method":     c.Request.Method,
			"path":       c.Request.URL.Path,
			"status":     c.Writer.Status(),
			"latency_ms": latency.Milliseconds(),
			"client_ip":  c.ClientIP(),
			"bytes_out":  c.Writer.Size(),
		})
		if model := extractModel(c); model != "" {
			entry = entry.WithField("model", model)
		}
		if len(c.Errors) > 0 {
			entry = entry.WithField("errors", c.Errors.String())
			entry.Error("request completed with errors")
			return
		}
		if c.Writer.Status() >= 500 {
			entry.Error("request completed")
		} else {
			entry.Info("request completed")
		}
	}
}

func extractModel(c *gin.Context) string {
	if v, ok := c.Get("fmg_model"); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

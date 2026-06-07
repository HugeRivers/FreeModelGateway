package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/free-model-gateway/fmg/internal/proxy"
	"github.com/free-model-gateway/fmg/internal/router"
	"github.com/gin-gonic/gin"
)

func (h *Handler) ChatCompletions(c *gin.Context) {
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "failed to read body: " + err.Error(), "type": "invalid_request", "code": "bad_body"},
		})
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(rawBody))

	var probe struct {
		Model       string                 `json:"model"`
		Stream      bool                   `json:"stream"`
		Messages    []router.Message       `json:"messages"`
		Temperature float32                `json:"temperature"`
		MaxTokens   int                    `json:"max_tokens"`
		TopP        float32                `json:"top_p"`
		Extra       map[string]interface{} `json:"-"`
	}
	if err := json.Unmarshal(rawBody, &probe); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "invalid JSON: " + err.Error(), "type": "invalid_request", "code": "bad_json"},
		})
		return
	}
	_ = json.Unmarshal(rawBody, &probe.Extra)

	req := &router.Request{
		Messages:    probe.Messages,
		Model:       probe.Model,
		Stream:      probe.Stream,
		Temperature: probe.Temperature,
		MaxTokens:   probe.MaxTokens,
		TopP:        probe.TopP,
		RawBody:     rawBody,
	}

	if probe.Model != "" {
		c.Set("fmg_model", probe.Model)
	}

	if req.Stream {
		h.handleStream(c, req)
		return
	}

	result, err := h.router.Route(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": err.Error(), "type": "gateway_error", "code": "internal"},
		})
		return
	}
	if result == nil || !result.Success {
		status := http.StatusServiceUnavailable
		if result != nil && result.ErrorStatus > 0 {
			status = result.ErrorStatus
		}
		healthy, cooldown := 0, 0
		for _, m := range h.pool.All() {
			switch m.Status {
			case "cooldown":
				cooldown++
			case "healthy":
				healthy++
			}
		}
		errMsg := "all backends failed"
		if result != nil && result.Error != nil {
			errMsg = result.Error.Error()
		}
		c.JSON(status, gin.H{
			"error": gin.H{
				"message": errMsg,
				"type":    "gateway_error",
				"code":    "no_available_backend",
				"param":   nil,
			},
			"metadata": gin.H{
				"gateway":                "FreeModelGateway",
				"version":                h.version,
				"tried_models":           chainForMeta(result),
				"retries":                retriesForMeta(result),
				"available_models_count": h.pool.Count(),
				"healthy_models_count":   healthy,
				"in_cooldown_count":      cooldown,
			},
		})
		return
	}

	meta := proxy.Metadata{
		Gateway:         "FreeModelGateway",
		Version:         h.version,
		ActualProvider:  result.Model.ProviderName,
		ActualModelID:   result.Model.ModelID,
		ActualModelName: result.Model.ModelName,
		FallbackCount:   result.Retries,
		FallbackChain:   result.FallbackChain,
		RouteStrategy:   h.router.StrategyName(),
		LatencyMs:       result.Latency.Milliseconds(),
	}
	out, err := proxy.InjectMetadata(result.Response, meta)
	if err != nil {
		c.Data(http.StatusOK, "application/json", result.Response)
		return
	}
	c.Data(http.StatusOK, "application/json", out)
}

func (h *Handler) handleStream(c *gin.Context, req *router.Request) {
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": "streaming unsupported by this transport", "type": "transport_error"},
		})
		return
	}

	result, err := h.router.RouteStream(c.Request.Context(), req, c.Writer, flusher)
	if err != nil {
		if errors.Is(err, router.ErrNoCandidate) {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": gin.H{"message": "no available backend", "type": "gateway_error", "code": "no_available_backend"},
			})
			return
		}
		if c.Writer.Written() {
			_, _ = c.Writer.Write([]byte("\ndata: [DONE]\n\n"))
			flusher.Flush()
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{
			"error": gin.H{"message": err.Error(), "type": "gateway_error", "code": "stream_failed"},
		})
		return
	}
	if result != nil && !result.Success {
		if c.Writer.Written() {
			_, _ = c.Writer.Write([]byte("\ndata: [DONE]\n\n"))
			flusher.Flush()
			return
		}
		errMsg := "stream failed"
		if result.Error != nil {
			errMsg = result.Error.Error()
		}
		c.JSON(http.StatusBadGateway, gin.H{
			"error": gin.H{"message": errMsg, "type": "gateway_error", "code": "stream_failed"},
		})
		return
	}
	if result != nil {
		h.log.WithFields(map[string]interface{}{
			"provider":   result.Model.ProviderName,
			"model":      result.Model.ModelID,
			"retries":    result.Retries,
			"latency_ms": result.Latency.Milliseconds(),
		}).Info("stream completed")
	}
}

func chainForMeta(r *router.Result) []string {
	if r == nil {
		return []string{}
	}
	return r.FallbackChain
}

func retriesForMeta(r *router.Result) int {
	if r == nil {
		return 0
	}
	return r.Retries
}

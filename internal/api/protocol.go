package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/free-model-gateway/fmg/internal/model"
	"github.com/free-model-gateway/fmg/internal/protocol"
	"github.com/free-model-gateway/fmg/internal/router"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

type ProtocolHandler struct {
	router  *router.Router
	log     *logrus.Logger
	version string
	client  *http.Client
}

func NewProtocolHandler(r *router.Router, log *logrus.Logger, version string, client *http.Client) *ProtocolHandler {
	return &ProtocolHandler{
		router:  r,
		log:     log,
		version: version,
		client:  client,
	}
}

func (h *ProtocolHandler) OpenAIResponses(c *gin.Context) {
	rawBody, _ := io.ReadAll(c.Request.Body)
	openAIBody, err := protocol.OpenAIResponsesRequestToOpenAI(rawBody)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}
	req := h.buildInternalRequest(openAIBody, false)
	h.handleProtocol(c, req, protocol.OpenAIResponseToOpenAIResponses, nil)
}

func (h *ProtocolHandler) AnthropicMessages(c *gin.Context) {
	rawBody, _ := io.ReadAll(c.Request.Body)
	anthroReq, err := protocol.ParseAnthropicRequest(rawBody)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}
	openAIBody, err := protocol.AnthropicRequestToOpenAI(anthroReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "translation error", "type": "api_error"}})
		return
	}
	req := h.buildInternalRequest(openAIBody, anthroReq.Stream)
	h.handleProtocol(c, req, protocol.OpenAIResponseToAnthropic, protocol.OpenAIStreamChunkToAnthropicSSE)
}

func (h *ProtocolHandler) GeminiGenerate(c *gin.Context) {
	rawBody, _ := io.ReadAll(c.Request.Body)
	openAIBody, err := protocol.GeminiRequestToOpenAI(rawBody)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}
	req := h.buildInternalRequest(openAIBody, false)
	if model := c.Param("model"); model != "" {
		req.Model = model
	}
	h.handleProtocol(c, req, protocol.OpenAIResponseToGemini, nil)
}

func (h *ProtocolHandler) GeminiStream(c *gin.Context) {
	rawBody, _ := io.ReadAll(c.Request.Body)
	openAIBody, err := protocol.GeminiRequestToOpenAI(rawBody)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}
	req := h.buildInternalRequest(openAIBody, true)
	if model := c.Param("model"); model != "" {
		req.Model = model
	}
	h.handleProtocol(c, req, protocol.OpenAIResponseToGemini, protocol.OpenAIStreamChunkToGeminiSSE)
}

func (h *ProtocolHandler) BedrockConverse(c *gin.Context) {
	rawBody, _ := io.ReadAll(c.Request.Body)
	openAIBody, err := protocol.BedrockRequestToOpenAI(rawBody)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}
	req := h.buildInternalRequest(openAIBody, false)
	if model := c.Param("modelId"); model != "" {
		req.Model = model
	}
	h.handleProtocol(c, req, protocol.OpenAIResponseToBedrock, nil)
}

func (h *ProtocolHandler) buildInternalRequest(openAIBody []byte, stream bool) *router.Request {
	var probe struct {
		Model    string           `json:"model"`
		Messages []router.Message `json:"messages"`
		Stream   bool             `json:"stream"`
	}
	_ = json.Unmarshal(openAIBody, &probe)
	return &router.Request{
		Messages: probe.Messages,
		Model:    probe.Model,
		Stream:   stream || probe.Stream,
		RawBody:  openAIBody,
	}
}

type responseTranslator func([]byte) ([]byte, error)
type streamTranslator func([]byte) []string

func (h *ProtocolHandler) handleProtocol(c *gin.Context, req *router.Request, respFn responseTranslator, streamFn streamTranslator) {
	if req.Stream && streamFn != nil {
		h.handleStream(c, req, streamFn)
		return
	}

	result, err := h.router.Route(c.Request.Context(), req)
	if err != nil {
		h.writeError(c, err, result)
		return
	}
	if result == nil || !result.Success {
		errMsg := "all backends failed"
		if result != nil && result.Error != nil {
			errMsg = result.Error.Error()
		}
		h.writeError(c, errors.New(errMsg), result)
		return
	}

	translated, err := respFn(result.Response)
	if err != nil {
		h.log.WithError(err).Error("protocol response translation failed")
		c.Data(http.StatusOK, "application/json", result.Response)
		return
	}
	c.Data(http.StatusOK, "application/json", translated)
}

func (h *ProtocolHandler) handleStream(c *gin.Context, req *router.Request, streamFn streamTranslator) {
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "streaming unsupported", "type": "api_error"}})
		return
	}

	maxRetries := 3
	tried := make([]*model.BackendModel, 0, maxRetries+1)

	for attempt := 0; attempt <= maxRetries; attempt++ {
		selected, err := h.router.SelectBackend(c.Request.Context(), req, tried)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": err.Error(), "type": "api_error"}})
			return
		}

		tried = append(tried, selected)

		rawResp, err := h.makeStreamRequest(c.Request.Context(), selected, req)
		if err != nil {
			h.log.WithError(err).WithField("provider", selected.ProviderID).Warn("stream request failed")
			selected.MarkFailure(err.Error())
			continue
		}

		if rawResp.StatusCode < 200 || rawResp.StatusCode >= 300 {
			body, _ := io.ReadAll(rawResp.Body)
			rawResp.Body.Close()
			h.log.WithFields(logrus.Fields{"provider": selected.ProviderID, "status": rawResp.StatusCode}).Warn("stream upstream error")

			if rawResp.StatusCode >= 400 && rawResp.StatusCode < 500 && rawResp.StatusCode != http.StatusTooManyRequests {
				c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": fmt.Sprintf("upstream error %d: %s", rawResp.StatusCode, string(body)), "type": "api_error"}})
				return
			}

			selected.MarkFailure(fmt.Sprintf("upstream_%d", rawResp.StatusCode))
			continue
		}

		h.writeStream(c, rawResp, streamFn, flusher)
		return
	}

	c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "all backends failed for stream", "type": "api_error"}})
}

func (h *ProtocolHandler) makeStreamRequest(ctx context.Context, selected *model.BackendModel, req *router.Request) (*http.Response, error) {
	rewritten, err := proxyRewriteBody(req.RawBody, selected.ModelID)
	if err != nil {
		return nil, fmt.Errorf("rewrite body: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, selected.BaseURL, bytes.NewReader(rewritten))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+selected.APIKey)
	httpReq.Header.Set("User-Agent", "fmg/"+h.version)
	httpReq.Header.Set("Accept", "text/event-stream")
	for k, v := range selected.ExtraHeaders {
		httpReq.Header.Set(k, v)
	}

	return h.client.Do(httpReq)
}

func (h *ProtocolHandler) writeStream(c *gin.Context, rawResp *http.Response, streamFn streamTranslator, flusher http.Flusher) {
	defer rawResp.Body.Close()

	w := c.Writer
	hdr := w.Header()
	hdr.Set("Content-Type", "text/event-stream")
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	scanner := bufio.NewScanner(rawResp.Body)
	scanner.Buffer(make([]byte, 0, 65536), 65536)

	var buf bytes.Buffer
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		events := streamFn([]byte(data))
		for _, evt := range events {
			buf.WriteString(evt)
			buf.WriteString("\n\n")
		}
		if buf.Len() > 0 {
			w.Write(buf.Bytes())
			flusher.Flush()
			buf.Reset()
		}
	}
}

func proxyRewriteBody(body []byte, targetModel string) ([]byte, error) {
	if len(body) == 0 {
		return body, nil
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	raw["model"] = targetModel
	return json.Marshal(raw)
}

func (h *ProtocolHandler) writeError(c *gin.Context, err error, result *router.Result) {
	status := http.StatusServiceUnavailable
	msg := "all backends failed"
	if err != nil {
		msg = err.Error()
	}
	if result != nil && result.ErrorStatus > 0 {
		status = result.ErrorStatus
	}
	c.JSON(status, gin.H{"error": gin.H{"type": "api_error", "message": msg}})
}

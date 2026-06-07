package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/free-model-gateway/fmg/internal/store"
	"github.com/gin-gonic/gin"
)

func (d *DashboardHandler) ListProvidersAPI(c *gin.Context) {
	ctx := c.Request.Context()
	instances, err := d.store.GetProviderInstances(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	templates, err := d.store.GetProviderTemplates(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	tmplMap := make(map[string]store.ProviderTemplate)
	for _, t := range templates {
		tmplMap[t.ID] = t
	}

	allModels, err := d.store.GetAllModels(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	modelsByProvider := make(map[int64][]gin.H)
	for _, m := range allModels {
		modelsByProvider[m.ProviderInstanceID] = append(modelsByProvider[m.ProviderInstanceID], gin.H{
			"id":         m.ID,
			"model_id":   m.ModelID,
			"name":       m.Name,
			"is_enabled": m.IsEnabled,
		})
	}

	var result []gin.H
	for _, inst := range instances {
		tmpl := tmplMap[inst.TemplateID]
		baseURL := tmpl.BaseURL
		apiFormat := tmpl.APIFormat
		if inst.TemplateID == "custom" {
			var headers map[string]string
			json.Unmarshal([]byte(inst.CustomHeaders), &headers)
			baseURL = headers["__base_url"]
			apiFormat = headers["__api_format"]
		}
		result = append(result, gin.H{
			"id":          inst.ID,
			"provider_id": fmt.Sprintf("%s-%d", inst.TemplateID, inst.ID),
			"template_id": inst.TemplateID,
			"name":        inst.Name,
			"api_key_set": inst.APIKey != "",
			"base_url":    baseURL,
			"api_format":  apiFormat,
			"models":      modelsByProvider[inst.ID],
		})
	}

	c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
	c.JSON(http.StatusOK, gin.H{"providers": result})
}

func (d *DashboardHandler) CreateProviderAPI(c *gin.Context) {
	var body struct {
		TemplateID string `json:"template_id"`
		ProviderID string `json:"provider_id"`
		Name       string `json:"name"`
		APIKey     string `json:"api_key"`
		APIFormat  string `json:"api_format"`
		BaseURL    string `json:"base_url"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	templateID := body.TemplateID
	var customHeaders string
	if templateID == "custom" {
		templateID = "custom"
		headers := map[string]string{
			"__base_url":   body.BaseURL,
			"__api_format": body.APIFormat,
		}
		b, _ := json.Marshal(headers)
		customHeaders = string(b)
	}

	inst := &store.ProviderInstance{
		TemplateID:    templateID,
		Name:          body.Name,
		APIKey:        body.APIKey,
		CustomHeaders: customHeaders,
		IsEnabled:     true,
	}
	id, err := d.store.CreateProviderInstance(c.Request.Context(), inst)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "ok": true})
}

func (d *DashboardHandler) UpdateProviderAPI(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var body struct {
		Name      string `json:"name"`
		APIKey    string `json:"api_key"`
		APIFormat string `json:"api_format"`
		BaseURL   string `json:"base_url"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	instances, err := d.store.GetProviderInstances(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	for _, inst := range instances {
		if inst.ID != id {
			continue
		}
		inst.Name = body.Name
		if body.APIKey != "" {
			inst.APIKey = body.APIKey
		}
		if inst.TemplateID == "custom" {
			headers := map[string]string{
				"__base_url":   body.BaseURL,
				"__api_format": body.APIFormat,
			}
			b, _ := json.Marshal(headers)
			inst.CustomHeaders = string(b)
		}
		if err := d.store.UpdateProviderInstance(c.Request.Context(), &inst); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
}

func (d *DashboardHandler) DeleteProviderAPI(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)

	modelCount, err := d.store.CountModelsByProvider(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if modelCount > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("该 Provider 下还有 %d 个模型，请先删除所有模型后再删除 Provider", modelCount)})
		return
	}

	if err := d.store.DeleteProviderInstance(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (d *DashboardHandler) RevealProviderKey(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	instances, _ := d.store.GetProviderInstances(c.Request.Context())
	for _, inst := range instances {
		if inst.ID == id {
			c.JSON(http.StatusOK, gin.H{"api_key": inst.APIKey})
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
}

func (d *DashboardHandler) GetModelAPI(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	m, err := d.store.GetModel(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if m == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "model not found"})
		return
	}
	c.JSON(http.StatusOK, m)
}

func (d *DashboardHandler) CreateModelAPI(c *gin.Context) {
	var body store.Model
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	body.IsEnabled = true
	id, err := d.store.CreateModel(c.Request.Context(), &body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "ok": true})
}

func (d *DashboardHandler) UpdateModelAPI(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var body struct {
		ModelID     string `json:"model_id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		IsEnabled   bool   `json:"is_enabled"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	m, err := d.store.GetModel(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if m == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "model not found"})
		return
	}

	if body.ModelID != "" {
		m.ModelID = body.ModelID
	}
	if body.Name != "" {
		m.Name = body.Name
	}
	m.Description = body.Description
	m.IsEnabled = body.IsEnabled

	if err := d.store.UpdateModel(c.Request.Context(), m); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (d *DashboardHandler) DeleteModelAPI(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := d.store.DeleteModel(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

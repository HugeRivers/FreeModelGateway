package handler

import (
	"embed"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/free-model-gateway/fmg/internal/config"
	"github.com/free-model-gateway/fmg/internal/model"
	"github.com/free-model-gateway/fmg/internal/router"
	"github.com/free-model-gateway/fmg/internal/stats"
	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
)

//go:embed dashboard.html
var dashboardFS embed.FS

const dashboardFile = "dashboard.html"

type DashboardHandler struct {
	router   *router.Router
	pool     *model.Pool
	stats    *stats.Collector
	reloader *config.Reloader
	version  string
	cfgPath  string
	envPath  string
}

func NewDashboard(r *router.Router, p *model.Pool, s *stats.Collector, reloader *config.Reloader, version, cfgPath, envPath string) *DashboardHandler {
	return &DashboardHandler{
		router:   r,
		pool:     p,
		stats:    s,
		reloader: reloader,
		version:  version,
		cfgPath:  cfgPath,
		envPath:  envPath,
	}
}

// DashboardData aggregates all info needed by the dashboard frontend.
type DashboardData struct {
	Health      map[string]interface{} `json:"health"`
	Stats       stats.Stats            `json:"stats"`
	Strategy    string                 `json:"strategy"`
	Version     string                 `json:"version"`
	ForcedModel *ForcedModelInfo       `json:"forced_model"`
	LastUsed    *LastUsedInfo          `json:"last_used"`
	MissingKeys []string               `json:"missing_keys"`
}

type ForcedModelInfo struct {
	ProviderID   string `json:"provider_id"`
	ProviderName string `json:"provider_name"`
	ModelID      string `json:"model_id"`
	ModelName    string `json:"model_name"`
}

type LastUsedInfo struct {
	ProviderID   string `json:"provider_id"`
	ProviderName string `json:"provider_name"`
	ModelID      string `json:"model_id"`
	ModelName    string `json:"model_name"`
}

// Index serves the dashboard HTML page.
func (d *DashboardHandler) Index(c *gin.Context) {
	f, err := dashboardFS.Open(dashboardFile)
	if err != nil {
		c.String(http.StatusInternalServerError, "dashboard not found")
		return
	}
	defer f.Close()
	html, err := io.ReadAll(f)
	if err != nil {
		c.String(http.StatusInternalServerError, "error reading dashboard")
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", html)
}

// DashboardAPI returns aggregated JSON for the dashboard.
func (d *DashboardHandler) DashboardAPI(c *gin.Context) {
	all := d.pool.All()
	healthy, cooldown := 0, 0
	for _, m := range all {
		switch m.Status {
		case model.StatusHealthy:
			healthy++
		case model.StatusCooldown:
			cooldown++
		}
	}

	health := map[string]interface{}{
		"status":         "ok",
		"version":        d.version,
		"models_total":   len(all),
		"healthy":        healthy,
		"cooldown":       cooldown,
		"strategy":       d.router.StrategyName(),
		"providers":      len(d.pool.ProviderSummary()),
		"uptime_seconds": int64(time.Since(startTime).Seconds()),
	}

	var forcedModel *ForcedModelInfo
	provID, modelID := d.router.ForcedModelIDs()
	if provID != "" && modelID != "" {
		if m, err := d.pool.Get(provID, modelID); err == nil {
			forcedModel = &ForcedModelInfo{
				ProviderID:   provID,
				ProviderName: m.ProviderName,
				ModelID:      modelID,
				ModelName:    m.ModelName,
			}
		}
	}

	var lastUsed *LastUsedInfo
	lProv, lModel, lName := d.router.LastUsedModel()
	if lProv != "" && lModel != "" {
		lastUsed = &LastUsedInfo{
			ProviderID:   lProv,
			ProviderName: "",
			ModelID:      lModel,
			ModelName:    lName,
		}
		if m, err := d.pool.Get(lProv, lModel); err == nil {
			lastUsed.ProviderName = m.ProviderName
		} else {
			lastUsed.ProviderName = lProv
		}
	}

	cfg := d.reloader.Current()
	var missingKeys []string
	for _, p := range cfg.Providers {
		if p.APIKey == "" {
			missingKeys = append(missingKeys, p.ID)
		}
	}

	c.JSON(http.StatusOK, DashboardData{
		Health:      health,
		Stats:       d.stats.Snapshot(),
		Strategy:    d.router.StrategyName(),
		Version:     d.version,
		ForcedModel: forcedModel,
		LastUsed:    lastUsed,
		MissingKeys: missingKeys,
	})
}

// SettingsConfig returns the raw config.yaml contents for the Settings page.
func (d *DashboardHandler) SettingsConfig(c *gin.Context) {
	c.JSON(http.StatusOK, d.readTextFile(d.cfgPath))
}

// SettingsEnv returns the raw .env contents for the Settings page.
func (d *DashboardHandler) SettingsEnv(c *gin.Context) {
	c.JSON(http.StatusOK, d.readTextFile(d.envPath))
}

// UpdateConfig validates and persists config.yaml.
func (d *DashboardHandler) UpdateConfig(c *gin.Context) {
	var body struct {
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON: " + err.Error()})
		return
	}
	if _, err := yamlUnmarshal(body.Content); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "YAML parse error: " + err.Error(),
		})
		return
	}
	if err := d.writeTextFile(d.cfgPath, body.Content); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":               true,
		"path":             d.cfgPath,
		"requires_restart": true,
		"note":             "YAML saved. Provider / port / strategy changes take effect after restart.",
	})
}

// UpdateEnv persists the .env file. No content-level validation beyond
// skipping blanks/comments is performed (matches the parser used at startup).
func (d *DashboardHandler) UpdateEnv(c *gin.Context) {
	var body struct {
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON: " + err.Error()})
		return
	}
	if err := d.writeTextFile(d.envPath, body.Content); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":               true,
		"path":             d.envPath,
		"requires_restart": true,
		"note":             ".env saved. API key changes take effect after restart.",
	})
}

func (d *DashboardHandler) readTextFile(path string) gin.H {
	info, statErr := os.Stat(path)
	exists := statErr == nil
	out := gin.H{
		"path":    path,
		"exists":  exists,
		"content": "",
	}
	if !exists {
		return out
	}
	data, err := os.ReadFile(path)
	if err != nil {
		out["error"] = err.Error()
		return out
	}
	out["content"] = string(data)
	if info != nil {
		out["mtime"] = info.ModTime().UTC().Format("2006-01-02T15:04:05Z")
		out["size"] = info.Size()
	}
	return out
}

func (d *DashboardHandler) writeTextFile(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("ensure dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".fmg-settings-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.Write([]byte(content)); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("rename temp -> %s: %w", path, err)
	}
	return nil
}

func yamlUnmarshal(content string) (interface{}, error) {
	var out interface{}
	if err := yaml.Unmarshal([]byte(content), &out); err != nil {
		return nil, err
	}
	return out, nil
}

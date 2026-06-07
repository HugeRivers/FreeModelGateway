package config

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/free-model-gateway/fmg/internal/store"
)

// BuiltinProviderTemplates defines all built-in provider templates.
// Users can select these from the UI without entering base_url or api_format.
var BuiltinProviderTemplates = []store.ProviderTemplate{
	{
		ID:        "opencode-zen",
		Name:      "OpenCode Zen",
		BaseURL:   "https://opencode.ai/zen/v1/chat/completions",
		APIFormat: APIFormatOpenAICompatible,
		APIKeyEnv: "OPENCODE_API_KEY",
	},
	{
		ID:        "openrouter",
		Name:      "OpenRouter",
		BaseURL:   "https://openrouter.ai/api/v1/chat/completions",
		APIFormat: APIFormatOpenAICompatible,
		APIKeyEnv: "OPENROUTER_API_KEY",
		DefaultHeaders: mustJSON(map[string]string{
			"HTTP-Referer": "http://localhost:10086",
			"X-Title":      "FreeModelGateway",
		}),
	},
	{
		ID:        "aihubmix",
		Name:      "AIHubMix",
		BaseURL:   "https://aihubmix.com/v1/chat/completions",
		APIFormat: APIFormatOpenAICompatible,
		APIKeyEnv: "AIHUBMIX_API_KEY",
	},
	{
		ID:        "zenmux",
		Name:      "ZenMux",
		BaseURL:   "https://zenmux.ai/api/v1/chat/completions",
		APIFormat: APIFormatOpenAICompatible,
		APIKeyEnv: "ZENMUX_API_KEY",
	},
}

// BuiltinModels defines default models for each built-in provider.
// These are inserted into DB on first startup if the table is empty.
var BuiltinModels = map[string][]store.Model{
	"opencode-zen": {
		{ModelID: "deepseek-v4-flash-free", Name: "DeepSeek V4 Flash Free"},
		{ModelID: "minimax-m3-free", Name: "MiniMax M3 Free"},
		{ModelID: "mimo-v2.5-free", Name: "MiMo V2.5 Free"},
		{ModelID: "nemotron-3-super-free", Name: "Nemotron 3 Super Free"},
	},
	"openrouter": {
		{ModelID: "openrouter/free", Name: "Free Models Router (Auto)"},
		{ModelID: "poolside/laguna-m.1:free", Name: "Laguna M.1 Free"},
		{ModelID: "poolside/laguna-xs.2:free", Name: "Laguna XS.2 Free"},
		{ModelID: "liquid/lfm-2.5-1.2b-instruct:free", Name: "LFM2.5 1.2B Instruct Free"},
		{ModelID: "meta-llama/llama-3.2-3b-instruct:free", Name: "Llama 3.2 3B Free"},
		{ModelID: "nvidia/nemotron-nano-12b-v2-vl:free", Name: "Nemotron Nano 12B V2 VL Free"},
		{ModelID: "nvidia/nemotron-nano-9b-v2:free", Name: "Nemotron Nano 9B V2 Free"},
		{ModelID: "cognitivecomputations/dolphin-mistral-24b-venice-edition:free", Name: "Dolphin Mistral 24B Free"},
	},
	"aihubmix": {
		{ModelID: "coding-glm-5.1-free", Name: "Coding GLM 5.1 Free"},
		{ModelID: "coding-minimax-m2.7-free", Name: "Coding MiniMax M2.7 Free"},
		{ModelID: "xiaomi-mimo-v2.5-free", Name: "Xiaomi MiMo V2.5 Free"},
		{ModelID: "xiaomi-mimo-v2.5-pro-free", Name: "Xiaomi MiMo V2.5 Pro Free"},
	},
	"zenmux": {},
}

func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// InitBuiltinData seeds the database with built-in provider templates and default models.
// Templates are always upserted so schema changes propagate. Models and instances
// are only inserted on first startup (when the templates table is empty).
func InitBuiltinData(ctx context.Context, st *store.Store) error {
	// Check if templates already exist
	templates, err := st.GetProviderTemplates(ctx)
	if err != nil {
		return fmt.Errorf("check templates: %w", err)
	}
	isFirstStartup := len(templates) == 0

	for _, t := range BuiltinProviderTemplates {
		if err := st.UpsertProviderTemplate(ctx, &t); err != nil {
			return fmt.Errorf("upsert template %s: %w", t.ID, err)
		}
	}

	if !isFirstStartup {
		return nil
	}

	tmplNameMap := make(map[string]string)
	for _, t := range BuiltinProviderTemplates {
		tmplNameMap[t.ID] = t.Name
	}

	// Insert one default instance per built-in provider with empty API key
	for tmplID, models := range BuiltinModels {
		inst := &store.ProviderInstance{
			TemplateID: tmplID,
			Name:       tmplNameMap[tmplID],
			APIKey:     "",
			IsEnabled:  true,
		}
		instID, err := st.CreateProviderInstance(ctx, inst)
		if err != nil {
			return fmt.Errorf("insert instance %s: %w", tmplID, err)
		}

		for _, m := range models {
			m.ProviderInstanceID = instID
			m.IsEnabled = true
			if _, err := st.CreateModel(ctx, &m); err != nil {
				return fmt.Errorf("insert model %s/%s: %w", tmplID, m.ModelID, err)
			}
		}
	}
	return nil
}

// LoadConfigFromDB reads provider and model configuration from the database
// and converts it into the Config struct used by the rest of the application.
func LoadConfigFromDB(ctx context.Context, st *store.Store, baseCfg *Config) (*Config, error) {
	cfg := DefaultConfig()
	if baseCfg != nil {
		cfg.Gateway = baseCfg.Gateway
		cfg.Strategy = baseCfg.Strategy
		cfg.Fallback = baseCfg.Fallback
		cfg.Log = baseCfg.Log
	}

	templates, err := st.GetProviderTemplates(ctx)
	if err != nil {
		return nil, fmt.Errorf("load templates: %w", err)
	}
	tmplMap := make(map[string]store.ProviderTemplate, len(templates))
	for _, t := range templates {
		tmplMap[t.ID] = t
	}

	instances, err := st.GetProviderInstances(ctx)
	if err != nil {
		return nil, fmt.Errorf("load instances: %w", err)
	}

	for _, inst := range instances {
		if !inst.IsEnabled {
			continue
		}
		tmpl, ok := tmplMap[inst.TemplateID]
		if !ok && inst.TemplateID != "custom" {
			continue
		}

		var baseURL, apiFormat string
		var headers map[string]string
		if inst.TemplateID == "custom" {
			// For custom providers, decode from instance data (stored in custom_headers JSON)
			json.Unmarshal([]byte(inst.CustomHeaders), &headers)
			baseURL = headers["__base_url"]
			apiFormat = headers["__api_format"]
			delete(headers, "__base_url")
			delete(headers, "__api_format")
		} else {
			baseURL = tmpl.BaseURL
			apiFormat = tmpl.APIFormat
			json.Unmarshal([]byte(tmpl.DefaultHeaders), &headers)
		}

		apiKey := inst.APIKey

		pc := ProviderConfig{
			ID:        fmt.Sprintf("%s-%d", inst.TemplateID, inst.ID),
			Name:      inst.Name,
			BaseURL:   baseURL,
			APIKey:    apiKey,
			APIFormat: apiFormat,
			Headers:   headers,
		}

		models, err := st.GetModelsByProvider(ctx, inst.ID)
		if err != nil {
			return nil, fmt.Errorf("load models for %s: %w", inst.Name, err)
		}
		for _, m := range models {
			if !m.IsEnabled {
				continue
			}
			pc.Models = append(pc.Models, ModelConfig{
				ID:   m.ModelID,
				Name: m.Name,
			})
		}

		if len(pc.Models) > 0 {
			cfg.Providers = append(cfg.Providers, pc)
		}
	}

	return cfg, nil
}

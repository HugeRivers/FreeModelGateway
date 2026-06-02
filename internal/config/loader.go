package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Load reads a YAML config file, applies defaults, resolves API key env vars,
// and validates the result. It returns an error if the file is missing or
// invalid.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	// Apply defaults for fields that were left at zero in YAML.
	if cfg.Gateway.Port == 0 {
		cfg.Gateway.Port = 10086
	}
	if cfg.Gateway.MaxRetries == 0 {
		cfg.Gateway.MaxRetries = 3
	}
	if cfg.Gateway.RetryDelayMs == 0 {
		cfg.Gateway.RetryDelayMs = 500
	}
	if cfg.Gateway.ConnectTimeoutS == 0 {
		cfg.Gateway.ConnectTimeoutS = 60
	}
	if cfg.Gateway.ReadTimeoutS == 0 {
		cfg.Gateway.ReadTimeoutS = 120
	}
	if cfg.Strategy.CooldownSeconds == 0 {
		cfg.Strategy.CooldownSeconds = 300
	}
	if cfg.Strategy.MaxCooldownS == 0 {
		cfg.Strategy.MaxCooldownS = 3600
	}
	if cfg.Strategy.Mode == "" {
		cfg.Strategy.Mode = "priority"
	}
	if cfg.Gateway.Host == "" {
		cfg.Gateway.Host = "0.0.0.0"
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
	if cfg.Log.Format == "" {
		cfg.Log.Format = "json"
	}

	// Resolve API keys from env vars.
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if p.APIKeyEnv != "" {
			p.APIKey = os.Getenv(p.APIKeyEnv)
		}
		if p.Weight == 0 {
			p.Weight = 1
		}
	}

	if err := Validate(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate checks structural correctness of the config.
func Validate(c *Config) error {
	if c == nil {
		return fmt.Errorf("config is nil")
	}
	if c.Gateway.Port <= 0 || c.Gateway.Port > 65535 {
		return fmt.Errorf("invalid gateway.port: %d", c.Gateway.Port)
	}
	if c.Gateway.MaxRetries < 0 {
		return fmt.Errorf("max_retries must be >= 0")
	}
	if len(c.Providers) == 0 {
		return fmt.Errorf("at least one provider is required")
	}

	mode := strings.ToLower(c.Strategy.Mode)
	switch mode {
	case "priority", "round-robin", "weighted-rr", "random":
		c.Strategy.Mode = mode
	default:
		return fmt.Errorf("invalid strategy.mode: %q (want priority|round-robin|weighted-rr|random)", c.Strategy.Mode)
	}

	seenProviders := make(map[string]struct{}, len(c.Providers))
	for i, p := range c.Providers {
		if p.ID == "" {
			return fmt.Errorf("providers[%d]: id is required", i)
		}
		if _, dup := seenProviders[p.ID]; dup {
			return fmt.Errorf("providers[%d]: duplicate id %q", i, p.ID)
		}
		seenProviders[p.ID] = struct{}{}

		if p.BaseURL == "" {
			return fmt.Errorf("providers[%d] (%s): base_url is required", i, p.ID)
		}
		if len(p.Models) == 0 {
			return fmt.Errorf("providers[%d] (%s): must have at least one model", i, p.ID)
		}
		seenModels := make(map[string]struct{}, len(p.Models))
		for j, m := range p.Models {
			if m.ID == "" {
				return fmt.Errorf("providers[%d].models[%d]: id is required", i, j)
			}
			if _, dup := seenModels[m.ID]; dup {
				return fmt.Errorf("providers[%d] (%s).models[%d]: duplicate model id %q", i, p.ID, j, m.ID)
			}
			seenModels[m.ID] = struct{}{}
		}
	}
	return nil
}

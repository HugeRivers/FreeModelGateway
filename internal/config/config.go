// Package config provides configuration loading and validation for FMG.
package config

// Config is the top-level gateway configuration.
type Config struct {
	Gateway   GatewayConfig    `yaml:"gateway"`
	Strategy  StrategyConfig   `yaml:"strategy"`
	Log       LogConfig        `yaml:"log"`
	Providers []ProviderConfig `yaml:"providers"`
}

// GatewayConfig holds network and retry parameters.
type GatewayConfig struct {
	Host            string `yaml:"host"`
	Port            int    `yaml:"port"`
	MaxRetries      int    `yaml:"max_retries"`
	RetryDelayMs    int    `yaml:"retry_delay_ms"`
	ConnectTimeoutS int    `yaml:"connect_timeout_s"`
	ReadTimeoutS    int    `yaml:"read_timeout_s"`
}

// StrategyConfig controls the routing strategy and cooldown policy.
type StrategyConfig struct {
	Mode            string `yaml:"mode"`
	CooldownSeconds int    `yaml:"cooldown_seconds"`
	MaxCooldownS    int    `yaml:"max_cooldown_s"`
}

// LogConfig controls logging output.
type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// ProviderConfig describes one upstream provider.
type ProviderConfig struct {
	ID        string            `yaml:"id"`
	Name      string            `yaml:"name"`
	BaseURL   string            `yaml:"base_url"`
	APIKeyEnv string            `yaml:"api_key_env"`
	APIKey    string            `yaml:"-"` // resolved from env at load time
	Priority  int               `yaml:"priority"`
	Weight    int               `yaml:"weight"`
	Headers   map[string]string `yaml:"headers"`
	Models    []ModelConfig     `yaml:"models"`
}

// ModelConfig describes one model under a provider.
type ModelConfig struct {
	ID            string `yaml:"id"`
	Name          string `yaml:"name"`
	ContextWindow int    `yaml:"context_window"`
	OutputLimit   int    `yaml:"output_limit"`
}

// DefaultConfig returns sensible defaults; port is 10086 per project spec.
func DefaultConfig() *Config {
	return &Config{
		Gateway: GatewayConfig{
			Host:            "0.0.0.0",
			Port:            10086,
			MaxRetries:      3,
			RetryDelayMs:    500,
			ConnectTimeoutS: 60,
			ReadTimeoutS:    120,
		},
		Strategy: StrategyConfig{
			Mode:            "priority",
			CooldownSeconds: 300,
			MaxCooldownS:    3600,
		},
		Log: LogConfig{
			Level:  "info",
			Format: "json",
		},
	}
}

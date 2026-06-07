// Package config provides configuration loading and validation for FMG.
package config

// FallbackStrategy 控制 fallback 行为
type FallbackStrategy string

const (
	// FallbackConservative 保守策略：先换 key → 再换 model → 再换 provider
	FallbackConservative FallbackStrategy = "conservative"
	// FallbackAggressive 激进策略：出现 429 直接跳过整个 provider
	FallbackAggressive FallbackStrategy = "aggressive"
	// FallbackAdaptive 混合策略：连续 2 次同 provider 429 后跳过整个 provider（默认）
	FallbackAdaptive FallbackStrategy = "adaptive"
)

// Config is the top-level gateway configuration.
type Config struct {
	Gateway   GatewayConfig    `yaml:"gateway"`
	Strategy  StrategyConfig   `yaml:"strategy"`
	Fallback  FallbackConfig   `yaml:"fallback"`
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

// FallbackConfig controls provider-level fallback behavior.
type FallbackConfig struct {
	Strategy          string `yaml:"strategy"`
	Consecutive429s   int    `yaml:"consecutive_429s"`
	ProviderCooldownS int    `yaml:"provider_cooldown_s"`
}

// LogConfig controls logging output.
type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// APIFormat constants for upstream protocol.
const (
	APIFormatOpenAICompatible = "openai-compatible"
	APIFormatOpenAIResponses  = "openai-responses"
	APIFormatAnthropic        = "anthropic"
	APIFormatGemini           = "gemini"
	APIFormatBedrock          = "bedrock"
)

// ProviderConfig describes one upstream provider.
type ProviderConfig struct {
	ID        string            `yaml:"id"`
	Name      string            `yaml:"name"`
	BaseURL   string            `yaml:"base_url"`
	APIKeyEnv string            `yaml:"api_key_env"`
	APIKey    string            `yaml:"-"` // resolved from env at load time
	APIFormat string            `yaml:"api_format"`
	Headers   map[string]string `yaml:"headers"`
	Models    []ModelConfig     `yaml:"models"`
}

// ModelConfig describes one model under a provider.
type ModelConfig struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
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
			Mode:            "balanced",
			CooldownSeconds: 300,
			MaxCooldownS:    3600,
		},
		Fallback: FallbackConfig{
			Strategy:          "adaptive",
			Consecutive429s:   2,
			ProviderCooldownS: 300,
		},
		Log: LogConfig{
			Level:  "info",
			Format: "json",
		},
	}
}

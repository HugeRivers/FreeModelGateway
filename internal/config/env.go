package config

import (
	"os"
	"strings"
)

// LoadEnvFile reads a KEY=value file and sets each entry as an OS environment
// variable. Existing environment variables are NOT overwritten.
//
// This matches the .env parser used at startup and is shared so that the
// admin Reload handler can refresh environment variables after the user
// edits .env through the Settings page.
func LoadEnvFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		val = strings.Trim(val, `"'`)
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
}

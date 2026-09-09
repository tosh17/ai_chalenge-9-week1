package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Port           string
	DeepSeekAPIKey string
	DeepSeekModel  string
	DeepSeekURL    string

	LocalEnabled bool
	LocalAPIURL  string
	LocalAPIKey  string
	LocalModel   string
	LocalTitle   string
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:          getEnv("PORT", "8080"),
		DeepSeekModel: getEnv("DEEPSEEK_MODEL", "deepseek-v4-flash"),
		DeepSeekURL:   getEnv("DEEPSEEK_API_URL", "https://api.deepseek.com/chat/completions"),
		LocalAPIURL:   getEnv("LOCAL_API_URL", ""),
		LocalAPIKey:   getEnv("LOCAL_API_KEY", ""),
		LocalModel:    getEnv("LOCAL_MODEL", "qwen-local"),
		LocalTitle:    getEnv("LOCAL_TITLE", "Local-Qwen"),
	}

	cfg.DeepSeekAPIKey = os.Getenv("DEEPSEEK_API_KEY")
	if cfg.DeepSeekAPIKey == "" {
		return nil, fmt.Errorf("DEEPSEEK_API_KEY is required")
	}

	enabled := strings.ToLower(getEnv("LOCAL_ENABLED", ""))
	cfg.LocalEnabled = cfg.LocalAPIURL != "" && (enabled == "" || enabled == "1" || enabled == "true" || enabled == "yes")

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// NormalizeChatURL принимает .../v1 или .../v1/chat/completions.
func NormalizeChatURL(base string) string {
	if base == "" {
		return ""
	}
	for len(base) > 0 && base[len(base)-1] == '/' {
		base = base[:len(base)-1]
	}
	const suffix = "/chat/completions"
	if len(base) >= len(suffix) && base[len(base)-len(suffix):] == suffix {
		return base
	}
	return base + suffix
}

package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port           string
	DeepSeekAPIKey string
	DeepSeekModel  string
	DeepSeekURL    string
	MaxTokens      int
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:          getEnv("PORT", "8080"),
		DeepSeekModel: getEnv("DEEPSEEK_MODEL", "deepseek-v4-flash"),
		DeepSeekURL:   getEnv("DEEPSEEK_API_URL", "https://api.deepseek.com/chat/completions"),
		MaxTokens:     getEnvInt("DEEPSEEK_MAX_TOKENS", 300),
	}

	cfg.DeepSeekAPIKey = os.Getenv("DEEPSEEK_API_KEY")
	if cfg.DeepSeekAPIKey == "" {
		return nil, fmt.Errorf("DEEPSEEK_API_KEY is required")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n <= 0 {
		return fallback
	}
	return n
}

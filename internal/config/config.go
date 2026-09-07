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

	LocalAPIURL   string
	LocalAPIKey   string
	LocalModel    string
	LocalTitle    string
	LocalEnabled  bool
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:          getEnv("PORT", "8080"),
		DeepSeekModel: getEnv("DEEPSEEK_MODEL", "deepseek-v4-flash"),
		DeepSeekURL:   getEnv("DEEPSEEK_API_URL", "https://api.deepseek.com/chat/completions"),
		LocalAPIURL:   getEnv("LOCAL_API_URL", ""),
		LocalAPIKey:   getEnv("LOCAL_API_KEY", ""),
		LocalModel:    getEnv("LOCAL_MODEL", "qwen-local"),
		LocalTitle:    getEnv("LOCAL_TITLE", "Локальная · Qwen"),
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

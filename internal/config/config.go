package config

import (
	"fmt"
	"os"
	"strconv"
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

	ChatHistoryPath   string
	ContextTokenLimit int // DeepSeek default context
	LocalContextLimit int // Qwen / local context

	CompressEnabled  bool
	CompressKeepLast int
	CompressEvery    int
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:              getEnv("PORT", "8080"),
		DeepSeekModel:     getEnv("DEEPSEEK_MODEL", "deepseek-v4-flash"),
		DeepSeekURL:       getEnv("DEEPSEEK_API_URL", "https://api.deepseek.com/chat/completions"),
		LocalAPIURL:       getEnv("LOCAL_API_URL", ""),
		LocalAPIKey:       getEnv("LOCAL_API_KEY", ""),
		LocalModel:        getEnv("LOCAL_MODEL", "qwen-local"),
		LocalTitle:        getEnv("LOCAL_TITLE", "Local-Qwen"),
		ChatHistoryPath:   getEnv("CHAT_HISTORY_PATH", "data/chat-history.json"),
		ContextTokenLimit: getEnvInt("CONTEXT_TOKEN_LIMIT", 1_000_000),
		LocalContextLimit: getEnvInt("LOCAL_CONTEXT_LIMIT", 256_000),
		CompressEnabled:   getEnvBool("COMPRESS_ENABLED", true),
		CompressKeepLast:  getEnvInt("COMPRESS_KEEP_LAST", 6),
		CompressEvery:     getEnvInt("COMPRESS_EVERY", 10),
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

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func getEnvBool(key string, fallback bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if v == "" {
		return fallback
	}
	return v == "1" || v == "true" || v == "yes" || v == "on"
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

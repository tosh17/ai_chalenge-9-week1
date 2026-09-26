package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
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

	StrategyKind   string
	SlidingWindowN int
	FactsWindowN   int
	BranchWindowN  int

	MemoryDir        string
	STMWindowN       int
	InjectSTM        bool
	InjectWM         bool
	InjectLTM        bool
	InjectProfile    bool
	InjectInvariants bool

	MCPEnabled bool
	MCPJar     string
	MCPJava    string

	ObserveEnabled bool
	ObserveCity    string
	ObserveEvery   time.Duration
	ObserveDir     string
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
		StrategyKind:      getEnv("CONTEXT_STRATEGY", "sliding"),
		SlidingWindowN:    getEnvInt("SLIDING_WINDOW_N", 8),
		FactsWindowN:      getEnvInt("FACTS_WINDOW_N", 6),
		BranchWindowN:     getEnvInt("BRANCH_WINDOW_N", 0),
		MemoryDir:         getEnv("MEMORY_DIR", "data/memory"),
		STMWindowN:        getEnvInt("STM_WINDOW_N", 8),
		InjectSTM:         getEnvBool("MEMORY_INJECT_STM", false),
		InjectWM:          getEnvBool("MEMORY_INJECT_WM", false),
		InjectLTM:         getEnvBool("MEMORY_INJECT_LTM", false),
		InjectProfile:     getEnvBool("MEMORY_INJECT_PROFILE", true),
		InjectInvariants:  getEnvBool("MEMORY_INJECT_INVARIANTS", false),
		MCPEnabled:        getEnvBool("MCP_ENABLED", true),
		MCPJar:            getEnv("MCP_JAR", ""),
		MCPJava:           getEnv("MCP_JAVA", ""),
		ObserveEnabled:    getEnvBool("OBSERVE_ENABLED", true),
		ObserveCity:       getEnv("OBSERVE_CITY", "Волгоград"),
		ObserveEvery:      getEnvDuration("OBSERVE_EVERY", 15*time.Minute),
		ObserveDir:        getEnv("OBSERVE_DIR", "data/observations"),
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

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
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

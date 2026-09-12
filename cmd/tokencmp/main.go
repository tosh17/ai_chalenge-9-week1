// Command tokencmp — CLI-обёртка над internal/tokendemo.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tosh17/deepseek-service/internal/agent"
	"github.com/tosh17/deepseek-service/internal/config"
	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/tokendemo"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	limit := cfg.ContextTokenLimit
	if v := os.Getenv("TOKENCMP_LIMIT"); v != "" {
		fmt.Sscanf(v, "%d", &limit)
	}
	if limit <= 0 || limit > 50_000 {
		limit = 4096
	}

	client := deepseek.NewClient(cfg.DeepSeekAPIKey, cfg.DeepSeekModel, cfg.DeepSeekURL)
	base := agent.New("day8-tokencmp").
		WithBackendLimit(agent.ProviderDeepSeek, "DeepSeek", cfg.DeepSeekModel, client, cfg.ContextTokenLimit)

	if cfg.LocalEnabled {
		localURL := config.NormalizeChatURL(cfg.LocalAPIURL)
		localClient := deepseek.NewClient(cfg.LocalAPIKey, cfg.LocalModel, localURL)
		base.WithBackendLimit(agent.ProviderLocal, cfg.LocalTitle, cfg.LocalModel, localClient, cfg.LocalContextLimit)
	}

	provider := os.Getenv("TOKENCMP_PROVIDER")
	if provider == "" {
		provider = agent.ProviderDeepSeek
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	rep, err := tokendemo.Run(ctx, base, tokendemo.Options{
		Provider: provider,
		Limit:    limit,
		Dir:      filepath.Join("data", "tokencmp"),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "tokendemo: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(rep.Table)

	outPath := filepath.Join("data", "tokencmp", "report.json")
	raw, _ := json.MarshalIndent(rep, "", "  ")
	_ = os.WriteFile(outPath, append(raw, '\n'), 0o644)
	fmt.Printf("\nreport: %s\n", outPath)
}

package main

import (
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/tosh17/deepseek-service/internal/agent"
	"github.com/tosh17/deepseek-service/internal/config"
	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/handler"
	"github.com/tosh17/deepseek-service/internal/memory"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	compressPath := dualHistoryPath(cfg.ChatHistoryPath, "compress")
	fullPath := dualHistoryPath(cfg.ChatHistoryPath, "full")

	compressStore, err := memory.Open(compressPath)
	if err != nil {
		log.Fatalf("memory compress: %v", err)
	}
	fullStore, err := memory.Open(fullPath)
	if err != nil {
		log.Fatalf("memory full: %v", err)
	}
	log.Printf("chat memory compress: %s (%d messages)", compressStore.Path(), compressStore.Len())
	log.Printf("chat memory full: %s (%d messages)", fullStore.Path(), fullStore.Len())

	deepseekClient := deepseek.NewClient(cfg.DeepSeekAPIKey, cfg.DeepSeekModel, cfg.DeepSeekURL)
	base := agent.New("day9-chat-agent").
		WithContextLimit(cfg.ContextTokenLimit).
		WithBackendLimit(agent.ProviderDeepSeek, "DeepSeek", cfg.DeepSeekModel, deepseekClient, cfg.ContextTokenLimit)

	if cfg.LocalEnabled {
		localURL := config.NormalizeChatURL(cfg.LocalAPIURL)
		localClient := deepseek.NewClient(cfg.LocalAPIKey, cfg.LocalModel, localURL)
		base.
			WithBackendLimit(agent.ProviderLocal, cfg.LocalTitle, cfg.LocalModel, localClient, cfg.LocalContextLimit).
			WithDefaultProvider(agent.ProviderLocal)
		log.Printf("local provider enabled (default): %s @ %s (context=%d)", cfg.LocalModel, localURL, cfg.LocalContextLimit)
	}

	compressAgent := base.CloneWithMemory("day9-compress", compressStore).
		WithCompression(agent.CompressionConfig{
			Enabled:        true,
			KeepLastN:      cfg.CompressKeepLast,
			SummarizeEvery: cfg.CompressEvery,
		})
	fullAgent := base.CloneWithMemory("day9-full", fullStore).
		WithCompression(agent.CompressionConfig{
			Enabled:        false,
			KeepLastN:      cfg.CompressKeepLast,
			SummarizeEvery: cfg.CompressEvery,
		})

	h := handler.NewDual(compressAgent, fullAgent, cfg.DeepSeekModel)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	addr := ":" + cfg.Port
	cc := compressAgent.Compression()
	log.Printf(
		"server listening on %s (dual: compress=%s full=%s, default: %s, compress keep=%d every=%d)",
		addr, compressAgent.Name(), fullAgent.Name(), compressAgent.DefaultProvider(),
		cc.KeepLastN, cc.SummarizeEvery,
	)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func dualHistoryPath(base, side string) string {
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if ext == "" {
		ext = ".json"
	}
	return stem + "-" + side + ext
}

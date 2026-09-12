package main

import (
	"log"
	"net/http"

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

	store, err := memory.Open(cfg.ChatHistoryPath)
	if err != nil {
		log.Fatalf("memory: %v", err)
	}
	log.Printf("chat memory: %s (%d messages)", store.Path(), store.Len())

	deepseekClient := deepseek.NewClient(cfg.DeepSeekAPIKey, cfg.DeepSeekModel, cfg.DeepSeekURL)
	chatAgent := agent.New("day10-chat-agent").
		WithMemory(store).
		WithContextLimit(cfg.ContextTokenLimit).
		WithStrategy(agent.ContextStrategy{
			Kind:           cfg.StrategyKind,
			SlidingWindowN: cfg.SlidingWindowN,
			FactsWindowN:   cfg.FactsWindowN,
			BranchWindowN:  cfg.BranchWindowN,
		}).
		WithCompression(agent.CompressionConfig{Enabled: false}).
		WithBackendLimit(agent.ProviderDeepSeek, "DeepSeek", cfg.DeepSeekModel, deepseekClient, cfg.ContextTokenLimit)

	if cfg.LocalEnabled {
		localURL := config.NormalizeChatURL(cfg.LocalAPIURL)
		localClient := deepseek.NewClient(cfg.LocalAPIKey, cfg.LocalModel, localURL)
		chatAgent.
			WithBackendLimit(agent.ProviderLocal, cfg.LocalTitle, cfg.LocalModel, localClient, cfg.LocalContextLimit).
			WithDefaultProvider(agent.ProviderLocal)
		log.Printf("local provider enabled (default): %s @ %s (context=%d)", cfg.LocalModel, localURL, cfg.LocalContextLimit)
	}

	h := handler.New(chatAgent, cfg.DeepSeekModel)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	addr := ":" + cfg.Port
	st := chatAgent.Strategy()
	log.Printf(
		"server listening on %s (agent: %s, default: %s, strategy: %s sliding=%d facts=%d branch=%d)",
		addr, chatAgent.Name(), chatAgent.DefaultProvider(),
		st.Kind, st.SlidingWindowN, st.FactsWindowN, st.BranchWindowN,
	)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}

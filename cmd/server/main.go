//go:debug x509usefallbackroots=1

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

	layers, err := memory.OpenLayers(cfg.MemoryDir)
	if err != nil {
		log.Fatalf("memory layers: %v", err)
	}
	log.Printf("memory layers: %s (stm=%d wm=%s ltm=%q)", layers.Dir(), layers.ShortTermLen(), layers.Working().Status, layers.LongTerm().Profile.Name)

	deepseekClient := deepseek.NewClient(cfg.DeepSeekAPIKey, cfg.DeepSeekModel, cfg.DeepSeekURL)
	chatAgent := agent.New("day11-memory-agent").
		WithLayers(layers).
		WithMemoryPolicy(agent.MemoryPolicy{
			STMWindowN: cfg.STMWindowN,
			InjectSTM:  cfg.InjectSTM,
			InjectWM:   cfg.InjectWM,
			InjectLTM:  cfg.InjectLTM,
		}).
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
	mp := chatAgent.MemoryPolicy()
	log.Printf(
		"server listening on %s (agent: %s, default: %s, layers stm_window=%d inject stm=%v wm=%v ltm=%v, strategy: %s)",
		addr, chatAgent.Name(), chatAgent.DefaultProvider(),
		mp.STMWindowN, mp.InjectSTM, mp.InjectWM, mp.InjectLTM, st.Kind,
	)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}

package main

import (
	"log"
	"net/http"

	"github.com/tosh17/deepseek-service/internal/agent"
	"github.com/tosh17/deepseek-service/internal/config"
	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/handler"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	deepseekClient := deepseek.NewClient(cfg.DeepSeekAPIKey, cfg.DeepSeekModel, cfg.DeepSeekURL)
	chatAgent := agent.New("day6-chat-agent").
		WithBackend(agent.ProviderDeepSeek, "DeepSeek", cfg.DeepSeekModel, deepseekClient)

	if cfg.LocalEnabled {
		localURL := config.NormalizeChatURL(cfg.LocalAPIURL)
		localClient := deepseek.NewClient(cfg.LocalAPIKey, cfg.LocalModel, localURL)
		chatAgent.WithBackend(agent.ProviderLocal, cfg.LocalTitle, cfg.LocalModel, localClient)
		log.Printf("local provider enabled: %s @ %s", cfg.LocalModel, localURL)
	}

	crew := agent.NewDesignCrew(chatAgent)
	h := handler.New(chatAgent, crew, cfg.DeepSeekModel)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	addr := ":" + cfg.Port
	log.Printf("server listening on %s (agent: %s, providers: %d, design-crew: on)", addr, chatAgent.Name(), len(chatAgent.Providers()))
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}

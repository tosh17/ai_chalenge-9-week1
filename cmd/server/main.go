package main

import (
	"log"
	"net/http"

	"github.com/tosh17/deepseek-service/internal/config"
	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/handler"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	client := deepseek.NewClient(cfg.DeepSeekAPIKey, cfg.DeepSeekModel, cfg.DeepSeekURL, deepseek.LocalModelConfig{
		Enabled: cfg.LocalEnabled,
		BaseURL: cfg.LocalAPIURL,
		APIKey:  cfg.LocalAPIKey,
		Model:   cfg.LocalModel,
		Title:   cfg.LocalTitle,
	})
	h := handler.New(client, cfg.DeepSeekModel)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	addr := ":" + cfg.Port
	if cfg.LocalEnabled {
		log.Printf("server listening on %s (deepseek: %s, local: %s @ %s)", addr, cfg.DeepSeekModel, cfg.LocalModel, cfg.LocalAPIURL)
	} else {
		log.Printf("server listening on %s (model: %s)", addr, cfg.DeepSeekModel)
	}
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}

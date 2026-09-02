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

	client := deepseek.NewClient(cfg.DeepSeekAPIKey, cfg.DeepSeekModel, cfg.DeepSeekURL, cfg.MaxTokens)
	h := handler.New(client, cfg.DeepSeekModel)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	addr := ":" + cfg.Port
	log.Printf("server listening on %s (model: %s, max_tokens: %d)", addr, cfg.DeepSeekModel, cfg.MaxTokens)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}

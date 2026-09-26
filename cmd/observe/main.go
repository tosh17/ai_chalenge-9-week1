//go:debug x509usefallbackroots=1

package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/tosh17/deepseek-service/internal/agent"
	"github.com/tosh17/deepseek-service/internal/config"
	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/mcp"
	"github.com/tosh17/deepseek-service/internal/observe"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if !cfg.ObserveEnabled {
		log.Fatal("observe: OBSERVE_ENABLED is false")
	}

	client, err := startMCP(cfg)
	if err != nil {
		log.Fatalf("observe mcp: %v", err)
	}
	defer client.Close()

	store := observe.Open(cfg.ObserveDir)
	tools := agent.ObservationTools{Store: store, MCP: client, Record: true}
	collector := agent.New("observe").WithTools(tools)

	deepseekClient := deepseek.NewClient(cfg.DeepSeekAPIKey, cfg.DeepSeekModel, cfg.DeepSeekURL)
	collector.WithBackendLimit(agent.ProviderDeepSeek, "DeepSeek", cfg.DeepSeekModel, deepseekClient, cfg.ContextTokenLimit)
	if cfg.LocalEnabled {
		localURL := config.NormalizeChatURL(cfg.LocalAPIURL)
		localClient := deepseek.NewClient(cfg.LocalAPIKey, cfg.LocalModel, localURL)
		collector.
			WithBackendLimit(agent.ProviderLocal, cfg.LocalTitle, cfg.LocalModel, localClient, cfg.LocalContextLimit).
			WithDefaultProvider(agent.ProviderLocal)
	}
	log.Printf("observe process: every %s city=%s dir=%s provider=%s", cfg.ObserveEvery, cfg.ObserveCity, cfg.ObserveDir, collector.DefaultProvider())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	run(ctx, collector, tools, cfg)
}

func startMCP(cfg *config.Config) (*mcp.Client, error) {
	jar := cfg.MCPJar
	if jar == "" {
		jar = filepath.Join("mcp", "open_meteo", "build", "libs", "open-meteo-0.1.0-all.jar")
	}
	javaBin, err := mcp.FindJava(cfg.MCPJava)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return mcp.Start(ctx, javaBin, "-Dkotlin-logging.logStartupMessage=false", "-jar", jar)
}

func run(ctx context.Context, collector *agent.Agent, tools agent.ObservationTools, cfg *config.Config) {
	collect := func() {
		cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		msg := "Плановый сбор. Вызови record_observation для города " + cfg.ObserveCity + "."
		res, err := collector.RunQuiet(cctx, collector.DefaultProvider(), msg)
		for _, ev := range res.ToolEvents {
			if ev.Name == "record_observation" && !ev.IsError {
				log.Printf("observe via model: %s", oneLine(ev.Result))
				return
			}
		}
		log.Printf("observe: model did not record (%v), direct write", err)
		sample, fresh, recErr := tools.RecordCity(cctx, cfg.ObserveCity)
		if recErr != nil {
			log.Printf("observe: direct write failed: %v", recErr)
			return
		}
		log.Printf("observe: direct %s %.1f°C fresh=%v", sample.City, sample.TemperatureC, fresh)
	}
	collect()
	ticker := time.NewTicker(cfg.ObserveEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			collect()
		}
	}
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 180 {
		return s[:180]
	}
	return s
}

//go:debug x509usefallbackroots=1

package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/tosh17/deepseek-service/internal/agent"
	"github.com/tosh17/deepseek-service/internal/config"
	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/handler"
	"github.com/tosh17/deepseek-service/internal/mcp"
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
	profiles, err := memory.OpenProfileBook(cfg.MemoryDir)
	if err != nil {
		log.Fatalf("profiles: %v", err)
	}
	invariants, err := memory.OpenInvariantBook(cfg.MemoryDir)
	if err != nil {
		log.Fatalf("invariants: %v", err)
	}
	log.Printf("memory layers: %s (stm=%d wm=%s/%s ltm=%q profile=%s/%s invariants=%d)",
		layers.Dir(), layers.ShortTermLen(), layers.Working().Status, layers.Working().Task.Stage, layers.LongTerm().Profile.Name,
		profiles.ActiveID(), profiles.Active().Title, len(invariants.Enabled()))

	deepseekClient := deepseek.NewClient(cfg.DeepSeekAPIKey, cfg.DeepSeekModel, cfg.DeepSeekURL)
	chatAgent := agent.New("day16-mcp-agent").
		WithLayers(layers).
		WithProfiles(profiles).
		WithInvariants(invariants).
		WithMemoryPolicy(agent.MemoryPolicy{
			STMWindowN:       cfg.STMWindowN,
			InjectSTM:        cfg.InjectSTM,
			InjectWM:         cfg.InjectWM,
			InjectLTM:        cfg.InjectLTM,
			InjectProfile:    cfg.InjectProfile,
			InjectInvariants: cfg.InjectInvariants,
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

	var mcpClient *mcp.Client
	if cfg.MCPEnabled {
		client, err := attachMCP(chatAgent, cfg)
		if err != nil {
			log.Printf("mcp off: %v", err)
		} else {
			mcpClient = client
		}
	}

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
		"server listening on %s (agent: %s, default: %s, layers stm_window=%d inject stm=%v wm=%v ltm=%v profile=%v inv=%v, strategy: %s)",
		addr, chatAgent.Name(), chatAgent.DefaultProvider(),
		mp.STMWindowN, mp.InjectSTM, mp.InjectWM, mp.InjectLTM, mp.InjectProfile, mp.InjectInvariants, st.Kind,
	)
	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			if mcpClient != nil {
				_ = mcpClient.Close()
			}
			log.Fatalf("server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutCtx)
	if mcpClient != nil {
		_ = mcpClient.Close()
	}
}

func attachMCP(chatAgent *agent.Agent, cfg *config.Config) (*mcp.Client, error) {
	jar := cfg.MCPJar
	if jar == "" {
		jar = filepath.Join("..", "mcp", "open_meteo", "build", "libs", "open-meteo-0.1.0-all.jar")
	}
	if _, err := os.Stat(jar); err != nil {
		return nil, err
	}
	javaBin, err := mcp.FindJava(cfg.MCPJava)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, err := mcp.Start(ctx, javaBin, "-Dkotlin-logging.logStartupMessage=false", "-jar", jar)
	if err != nil {
		return nil, err
	}
	chatAgent.WithTools(agent.MCPTools{Client: client})
	names := make([]string, 0)
	for _, tool := range client.Tools() {
		names = append(names, tool.Name)
	}
	log.Printf("mcp connected: %s tools=%v", jar, names)
	return client, nil
}

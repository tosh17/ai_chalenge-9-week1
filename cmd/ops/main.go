package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/mcp"
	"github.com/tosh17/deepseek-service/internal/rag"
	"github.com/tosh17/deepseek-service/internal/s1"
	"github.com/tosh17/deepseek-service/internal/support"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "support" {
		runSupport(os.Args[2:])
		return
	}
	runServer()
}

func runSupport(args []string) {
	fs := flag.NewFlagSet("support", flag.ExitOnError)
	q := fs.String("q", "", "вопрос о сервисе на s1")
	_ = fs.Parse(args)
	if *q == "" && fs.NArg() > 0 {
		*q = fs.Arg(0)
	}
	if *q == "" {
		log.Fatal("нужен вопрос: ops support -q \"Почему Ollama пишет, что контекст слишком большой?\"")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	ans, err := newDesk(ctx).Reply(ctx, *q)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(ans.Reply)
	names := make([]string, 0, len(ans.Calls))
	for _, call := range ans.Calls {
		names = append(names, call.Name)
	}
	log.Printf("support: tools=%v sources=%v", names, ans.Sources)
}

func runServer() {
	desk := newDesk(context.Background())
	addr := env("OPS_ADDR", ":"+env("OPS_PORT", "8095"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join("web", "index.html"))
	})
	mux.HandleFunc("POST /api/support", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Question string `json:"question"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer cancel()
		ans, err := desk.Reply(ctx, body.Question)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, ans)
	})
	mux.HandleFunc("GET /api/services", func(w http.ResponseWriter, r *http.Request) {
		text, err := (s1.Exec{Host: os.Getenv("S1_HOST")}).Run(r.Context(), s1.ListServices())
		if err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, map[string]string{"services": text})
	})
	log.Printf("ops listening on %s host=%s", addr, os.Getenv("S1_HOST"))
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func newDesk(ctx context.Context) *support.Desk {
	host := os.Getenv("S1_HOST")
	var tools support.Tools
	if os.Getenv("OPS_INPROCESS") == "1" {
		tools = support.Local{Tools: s1.Tools(s1.Exec{Host: host})}
	} else {
		client, err := mcp.Start(ctx, "go", "run", "./cmd/mcp", "s1")
		if err != nil {
			log.Printf("mcp off, in-process: %v", err)
			tools = support.Local{Tools: s1.Tools(s1.Exec{Host: host})}
		} else {
			tools = mcpTools{client: client}
		}
	}
	root, _ := os.Getwd()
	ollama := env("RAG_OLLAMA_URL", "http://10.10.1.101:11434")
	model := env("RAG_EMBED_MODEL", "nomic-embed-text")
	index := rag.New(rag.Options{
		Root:      root,
		CachePath: "data/rag/ops-index.json",
		Model:     model,
		OllamaURL: ollama,
		Embedder:  rag.NewOllama(ollama, model),
		TopK:      3,
	})
	return &support.Desk{Index: index, Chat: completer(), Tools: tools}
}

type mcpTools struct{ client *mcp.Client }

func (m mcpTools) Definitions() []deepseek.Tool {
	src := m.client.Tools()
	out := make([]deepseek.Tool, 0, len(src))
	for _, tool := range src {
		params := tool.InputSchema
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, deepseek.Tool{Name: tool.Name, Description: tool.Description, Parameters: params})
	}
	return out
}

func (m mcpTools) Call(ctx context.Context, name, arguments string) (string, error) {
	if arguments == "" {
		arguments = "{}"
	}
	res, err := m.client.Call(ctx, name, json.RawMessage(arguments))
	if err != nil {
		return res.Text, err
	}
	if res.IsError {
		return res.Text, fmt.Errorf("%s", res.Text)
	}
	return res.Text, nil
}

type chatComplete struct {
	client *deepseek.Client
}

func (c chatComplete) ChatTools(ctx context.Context, messages []deepseek.Message, tools []deepseek.Tool) (deepseek.ChatResult, error) {
	return c.client.ChatTools(ctx, messages, tools)
}

func completer() support.Chatter {
	key := os.Getenv("DEEPSEEK_API_KEY")
	url := os.Getenv("DEEPSEEK_API_URL")
	model := os.Getenv("DEEPSEEK_MODEL")
	if key == "" || url == "" {
		return nil
	}
	if model == "" {
		model = "deepseek-v4-flash"
	}
	return chatComplete{client: deepseek.NewClient(key, model, chatURL(url))}
}

func chatURL(raw string) string {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if strings.HasSuffix(raw, "/chat/completions") {
		return raw
	}
	return raw + "/chat/completions"
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

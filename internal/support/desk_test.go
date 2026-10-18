package support

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/rag"
	"github.com/tosh17/deepseek-service/internal/s1"
)

type scripted struct {
	steps []deepseek.ChatResult
	n     int
}

func (s *scripted) ChatTools(context.Context, []deepseek.Message, []deepseek.Tool) (deepseek.ChatResult, error) {
	if s.n >= len(s.steps) {
		return deepseek.ChatResult{Reply: "## Причина\nконтекст 8192 больше окна модели\n\n## Рекомендация\nуменьшить num_ctx"}, nil
	}
	step := s.steps[s.n]
	s.n++
	return step, nil
}

func TestReplyReadsLiveLogs(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	faq := "# Ollama\n\nКонтейнер ollama. Модель nomic-embed-text обучена на контексте 2048.\n"
	if err := os.WriteFile(filepath.Join(root, "docs", "faq.md"), []byte(faq), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# s1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := &s1.BufferRunner{Out: map[string]string{
		"ps":             "ollama|Up 5 days|10.10.1.101:11434->11434/tcp",
		"inspect:ollama": "running|none",
		"logs:ollama":    `level=WARN msg="requested context size too large for model" num_ctx=8192 n_ctx_train=2048 model=nomic-embed-text`,
	}}
	idx := rag.New(rag.Options{Root: root, Model: "test", Embedder: faqEmbed{}})
	chat := &scripted{steps: []deepseek.ChatResult{
		{ToolCalls: []deepseek.ToolCall{{ID: "1", Name: "list_services", Arguments: `{}`}}},
		{ToolCalls: []deepseek.ToolCall{{ID: "2", Name: "service_logs", Arguments: `{"name":"ollama","tail":80}`}}},
		{Reply: "## Причина\nВ логе ollama запрошен контекст 8192 при окне 2048.\n\n## Рекомендация\nНе увеличивать num_ctx для nomic-embed-text."},
	}}
	ans, err := (&Desk{Index: idx, Chat: chat, Tools: Local{Tools: s1.Tools(run)}}).Reply(context.Background(), "Почему Ollama пишет, что контекст слишком большой?")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ans.Reply, "8192") || !strings.Contains(ans.Reply, "## Рекомендация") {
		t.Fatalf("%s", ans.Reply)
	}
	if saw(ans.Calls, "get_ticket") || !saw(ans.Calls, "service_logs") {
		t.Fatalf("calls %#v", ans.Calls)
	}
}

func TestFallbackWithoutModel(t *testing.T) {
	run := &s1.BufferRunner{Out: map[string]string{
		"ps":             "ollama|Up|10.10.1.101:11434->11434/tcp",
		"inspect:ollama": "running|none",
		"logs:ollama":    "model nomic-embed-text num_ctx=8192 n_ctx_train=2048",
	}}
	ans, err := (&Desk{Tools: Local{Tools: s1.Tools(run)}}).Reply(context.Background(), "Почему Ollama пишет, что контекст слишком большой?")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ans.Reply, "nomic-embed-text") || !strings.Contains(ans.Reply, "## Рекомендация") {
		t.Fatalf("%s", ans.Reply)
	}
}

type faqEmbed struct{}

func (faqEmbed) Embed(context.Context, []string) ([][]float32, error) {
	return nil, nil
}

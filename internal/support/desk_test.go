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
		return deepseek.ChatResult{Reply: "## Причина\nконтейнер gitlab healthy\n\n## Рекомендация\nоткрыть :8929"}, nil
	}
	step := s.steps[s.n]
	s.n++
	return step, nil
}

func TestReplyUsesTicketAndLogs(t *testing.T) {
	root := t.TempDir()
	writeCRM(t, root)
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	faq := "# GitLab\n\nКонтейнер gitlab публикует веб на порту 8929. Если статус healthy, страница открывается по http://10.10.1.101:8929.\n"
	if err := os.WriteFile(filepath.Join(root, "docs", "faq.md"), []byte(faq), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# s1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := &s1.BufferRunner{Out: map[string]string{
		"ps":             "gitlab\tUp 5 days (healthy)\t10.10.1.101:8929->8929/tcp",
		"inspect:gitlab": "running health=healthy",
		"logs:gitlab":    "listening on 8929",
	}}
	tools := Local{Tools: s1.Tools(run, filepath.Join(root, "support", "crm"))}
	idx := rag.New(rag.Options{Root: root, Model: "test", Embedder: faqEmbed{}})
	chat := &scripted{steps: []deepseek.ChatResult{
		{ToolCalls: []deepseek.ToolCall{{ID: "1", Name: "get_ticket", Arguments: `{"id":"TCK-210"}`}}},
		{ToolCalls: []deepseek.ToolCall{{ID: "2", Name: "service_logs", Arguments: `{"name":"gitlab","tail":40}`}}},
		{Reply: "## Причина\nКонтейнер gitlab healthy, порт 8929 слушается.\n\n## Рекомендация\nОткрыть http://10.10.1.101:8929 и войти как root."},
	}}
	ans, err := (&Desk{Index: idx, Chat: chat, Tools: tools}).Reply(context.Background(), "TCK-210 Почему не открывается GitLab?")
	if err != nil {
		t.Fatal(err)
	}
	if ans.Ticket != "TCK-210" || !strings.Contains(ans.Reply, "8929") {
		t.Fatalf("%#v", ans)
	}
	if !saw(ans.Calls, "get_ticket") || !saw(ans.Calls, "service_logs") {
		t.Fatalf("calls %#v", ans.Calls)
	}
}

func TestFallbackWithoutModel(t *testing.T) {
	root := t.TempDir()
	writeCRM(t, root)
	run := &s1.BufferRunner{Out: map[string]string{
		"ps":             "ollama\tUp\t10.10.1.101:11434->11434/tcp",
		"inspect:ollama": "running health=none",
		"logs:ollama":    "model nomic-embed-text loaded",
	}}
	ans, err := (&Desk{Tools: Local{Tools: s1.Tools(run, filepath.Join(root, "support", "crm"))}}).Reply(context.Background(), "TCK-211 эмбеддинги зависают")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ans.Reply, "nomic-embed-text") || !strings.Contains(ans.Reply, "## Рекомендация") {
		t.Fatalf("%s", ans.Reply)
	}
}

func writeCRM(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, "support", "crm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	users := `{"users":[{"id":"u-anna","name":"Анна","role":"admin"}]}`
	tickets := `{"tickets":[
	  {"id":"TCK-210","user_id":"u-anna","service":"gitlab","title":"Не открывается GitLab","body":"страница :8929"},
	  {"id":"TCK-211","user_id":"u-anna","service":"ollama","title":"Эмбеддинги","body":"nomic не отвечает"}
	]}`
	if err := os.WriteFile(filepath.Join(dir, "users.json"), []byte(users), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tickets.json"), []byte(tickets), 0o644); err != nil {
		t.Fatal(err)
	}
}

type faqEmbed struct{}

func (faqEmbed) Embed(context.Context, []string) ([][]float32, error) {
	return nil, nil
}

package help

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tosh17/deepseek-service/internal/agent"
	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/rag"
)

type fakeTools struct {
	calls []string
}

func (f *fakeTools) Names() []string {
	return []string{"git_branch", "list_files", "git_diff"}
}

func (f *fakeTools) Call(_ context.Context, name string, arguments json.RawMessage) (string, error) {
	f.calls = append(f.calls, name+":"+string(arguments))
	switch name {
	case "git_branch":
		return `{"branch":"day31","head":"abc1234","dirty":2}`, nil
	case "list_files":
		return `{"tree":"cmd/\ndocs/\nREADME.md"}`, nil
	case "git_diff":
		if strings.Contains(string(arguments), `"stat_only":true`) || strings.Contains(string(arguments), `"stat_only": true`) {
			return `{"status":"## day31","stat":" README.md | 3 ++","patch":""}`, nil
		}
		return `{"status":"## day31","stat":" README.md | 3 ++","patch":"+строка diff"}`, nil
	default:
		return "", os.ErrNotExist
	}
}

type fakeLLM struct {
	system string
	user   string
}

func (f *fakeLLM) Complete(_ context.Context, providerID, system string, history []deepseek.Message) (agent.Result, error) {
	f.system = system
	if len(history) > 0 {
		f.user = history[len(history)-1].Content
	}
	return agent.Result{Reply: "Проект — Go-чат. Команда /help.", Provider: providerID, Model: "fake"}, nil
}

func TestParseHelp(t *testing.T) {
	if _, ok := Parse("привет"); ok {
		t.Fatal("plain message is not /help")
	}
	q, ok := Parse("/help")
	if !ok || q != "" {
		t.Fatalf("bare: %q %v", q, ok)
	}
	q, ok = Parse("/HELP  какая ветка")
	if !ok || q != "какая ветка" {
		t.Fatalf("question: %q", q)
	}
}

func TestAnswerUsesDocsAndMCP(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "# API\n\nPOST /api/chat принимает message. Команда /help отвечает на вопросы о проекте.\n"
	if err := os.WriteFile(filepath.Join(root, "docs", "api.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Проект\n\nЧат дня 31.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := rag.New(rag.Options{Root: root, Model: "test", Embedder: bagEmbedder{}, TopK: 2})
	tools := &fakeTools{}
	llm := &fakeLLM{}
	guide := New(idx, tools, llm)

	ans, err := guide.Answer(context.Background(), Query{Text: "какие есть API", Provider: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if ans.Reply != "Проект — Go-чат. Команда /help." {
		t.Fatalf("reply: %s", ans.Reply)
	}
	if ans.Meta.Branch != "day31" || ans.Meta.Head != "abc1234" {
		t.Fatalf("meta: %#v", ans.Meta)
	}
	if len(ans.Meta.Sources) == 0 || ans.Meta.Sources[0].Path != "docs/api.md" {
		t.Fatalf("sources: %#v", ans.Meta.Sources)
	}
	joined := strings.Join(tools.calls, "\n")
	for _, name := range []string{"git_branch", "list_files", "git_diff"} {
		if !strings.Contains(joined, name) {
			t.Fatalf("mcp %s not called: %s", name, joined)
		}
	}
	if !strings.Contains(llm.user, "POST /api/chat") || !strings.Contains(llm.user, "day31") {
		t.Fatalf("prompt missed context:\n%s", llm.user)
	}
	if strings.Contains(llm.user, "+строка diff") {
		t.Fatal("stat-only question should not include the patch")
	}
}

func TestAnswerIncludesPatchWhenAsked(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Проект\n\nРепозиторий.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := rag.New(rag.Options{Root: root, Model: "test", Embedder: bagEmbedder{}})
	tools := &fakeTools{}
	llm := &fakeLLM{}
	ans, err := New(idx, tools, llm).Answer(context.Background(), Query{Text: "покажи git diff"})
	if err != nil {
		t.Fatal(err)
	}
	if ans.Reply == "" || !strings.Contains(llm.user, "+строка diff") {
		t.Fatalf("patch missing:\n%s", llm.user)
	}
}

type fallbackLLM struct {
	calls []string
}

func (f *fallbackLLM) Complete(_ context.Context, providerID, _ string, _ []deepseek.Message) (agent.Result, error) {
	f.calls = append(f.calls, providerID)
	if providerID == "local" {
		return agent.Result{Provider: providerID}, context.DeadlineExceeded
	}
	return agent.Result{Reply: "ответ второго провайдера", Provider: providerID, Model: "deepseek"}, nil
}

func TestAnswerFallsBackToSecondProvider(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Проект\n\nКоманда /help.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := rag.New(rag.Options{Root: root, Model: "test", Embedder: bagEmbedder{}})
	llm := &fallbackLLM{}
	ans, err := New(idx, &fakeTools{}, llm).Answer(context.Background(), Query{Text: "что за проект", Provider: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if ans.Meta.Degraded || ans.Reply != "ответ второго провайдера" || ans.Provider != "deepseek" {
		t.Fatalf("ans: %#v", ans)
	}
	if strings.Join(llm.calls, ",") != "local,deepseek" {
		t.Fatalf("calls: %v", llm.calls)
	}
}

func TestAnswerWithoutModelStillReturnsContext(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Проект\n\nКоманда /help читает документацию.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := rag.New(rag.Options{Root: root, Model: "test", Embedder: bagEmbedder{}})
	ans, err := New(idx, &fakeTools{}, nil).Answer(context.Background(), Query{})
	if err != nil {
		t.Fatal(err)
	}
	if !ans.Meta.Degraded || !strings.Contains(ans.Reply, "day31") || !strings.Contains(ans.Reply, "/help") {
		t.Fatalf("degraded reply: %#v", ans)
	}
}

type bagEmbedder struct{}

func (bagEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		low := strings.ToLower(text)
		vec := []float32{0, 0}
		if strings.Contains(low, "api") || strings.Contains(low, "/api/") || strings.Contains(low, "help") {
			vec[0] = 1
		}
		if strings.Contains(low, "проект") || strings.Contains(low, "репозитор") {
			vec[1] = 1
		}
		out[i] = vec
	}
	return out, nil
}

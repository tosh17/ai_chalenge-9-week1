package files

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tosh17/deepseek-service/internal/deepseek"
)

func TestFallbackFixesCloneURL(t *testing.T) {
	root := sample(t)
	res, err := (&Assistant{Tools: Local{Tools: Handlers(root)}}).Run(context.Background(), "Приведи clone_url в конфиге раннера к CLONE_URL из services.env и допиши в compose.yml, что адрес сверен")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, "runner.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `clone_url = "http://gitlab:8929"`) || strings.Contains(string(body), "8292") {
		t.Fatalf("runner:\n%s", body)
	}
	comp, err := os.ReadFile(filepath.Join(root, "compose.yml"))
	if err != nil || !strings.Contains(string(comp), "сверен") {
		t.Fatalf("compose %q %v", comp, err)
	}
	if !strings.Contains(res.Diff, "8929") || len(res.Files) < 2 {
		t.Fatalf("files %#v\n%s", res.Files, res.Diff)
	}
}

func TestReportTouchesSearch(t *testing.T) {
	root := sample(t)
	goal := "Найди все места, где указан GITLAB, и запиши отчёт в url-report.md"
	res, err := (&Assistant{Tools: Local{Tools: Handlers(root)}}).Run(context.Background(), goal)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, "url-report.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "services.env") || !strings.Contains(res.Diff, "diff --git a/url-report.md") {
		t.Fatalf("%s\n%s", body, res.Diff)
	}
}

func TestModelWrite(t *testing.T) {
	root := sample(t)
	chat := scripted{steps: []deepseek.ChatResult{
		{ToolCalls: []deepseek.ToolCall{{ID: "1", Name: "read_file", Arguments: `{"path":"services.env"}`}}},
		{ToolCalls: []deepseek.ToolCall{{ID: "2", Name: "write_file", Arguments: `{"path":"note.txt","content":"gitlab:8929\n"}`}}},
		{Reply: "Записал note.txt"},
	}}
	res, err := (&Assistant{Chat: &chat, Tools: Local{Tools: Handlers(root)}}).Run(context.Background(), "Зафиксируй адрес GitLab отдельной заметкой")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 1 || res.Files[0] != "note.txt" {
		t.Fatalf("%#v", res.Files)
	}
}

func TestJail(t *testing.T) {
	root := sample(t)
	if _, err := write(root, "../secret", "x"); err == nil {
		t.Fatal("parent")
	}
	if _, err := write(root, ".env", "X=1"); err == nil {
		t.Fatal(".env")
	}
}

type scripted struct {
	steps []deepseek.ChatResult
	n     int
}

func (s *scripted) ChatTools(context.Context, []deepseek.Message, []deepseek.Tool) (deepseek.ChatResult, error) {
	if s.n >= len(s.steps) {
		return deepseek.ChatResult{Reply: "готово"}, nil
	}
	step := s.steps[s.n]
	s.n++
	return step, nil
}

func sample(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"services.env": "GITLAB_WEB=http://10.10.1.101:8929\nCLONE_URL=http://gitlab:8929\n",
		"runner.toml":  "concurrent = 1\nclone_url = \"http://gitlab:8292\"\n",
		"compose.yml":  "services:\n  gitlab:\n    image: gitlab/gitlab-ce:19.4.1-ce.0\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

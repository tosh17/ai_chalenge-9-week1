package review

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tosh17/deepseek-service/internal/rag"
)

func TestParseDiffFiles(t *testing.T) {
	diff := "" +
		"diff --git a/internal/review/review.go b/internal/review/review.go\n" +
		"index 111..222 100644\n" +
		"--- a/internal/review/review.go\n" +
		"+++ b/internal/review/review.go\n" +
		"@@ -1 +1,2 @@\n" +
		"-old\n" +
		"+panic(\"boom\")\n" +
		"diff --git a/README.md b/README.md\n" +
		"+++ b/README.md\n" +
		"+# day\n"
	changes := ParseDiff(diff)
	if len(changes) != 2 || changes[0].Path != "internal/review/review.go" || changes[1].Path != "README.md" {
		t.Fatalf("%#v", changes)
	}
	lines := AddedLines(changes[0].Patch)
	if len(lines) != 1 || !strings.Contains(lines[0], "panic") {
		t.Fatalf("added: %#v", lines)
	}
}

func TestRunUsesRAGAndModel(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Проект\n\nЧат на Go.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "architecture.md"), []byte("# Архитектура\n\nОдин HTTP-процесс в cmd/server.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal.go"), []byte("package demo\n\nfunc ReviewHook() { panic(\"x\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := rag.New(rag.Options{Root: root, Model: "test", IncludeCode: true, Embedder: wordEmbedder{}})
	var gotUser string
	guide := &Reviewer{
		Index: idx,
		Complete: func(_ context.Context, system, user string) (string, error) {
			if !strings.Contains(system, "## Потенциальные баги") {
				t.Fatalf("system: %s", system)
			}
			gotUser = user
			return "## Потенциальные баги\npanic\n\n## Архитектурные проблемы\nнет\n\n## Рекомендации\nсмотреть docs\n", nil
		},
	}
	diff := "diff --git a/cmd/review/main.go b/cmd/review/main.go\n+++ b/cmd/review/main.go\n+func main() { panic(\"review\") }\n"
	res, err := guide.Run(context.Background(), diff)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Model || !strings.Contains(res.Text, "## Рекомендации") {
		t.Fatalf("%#v", res)
	}
	if !strings.Contains(gotUser, "cmd/review/main.go") {
		t.Fatalf("prompt missed the file:\n%s", gotUser)
	}
	if len(res.Files) != 1 || res.Files[0] != "cmd/review/main.go" {
		t.Fatalf("files: %#v", res.Files)
	}
}

func TestExtractiveWhenModelIsOff(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Проект\n\nСлои памяти в data/memory.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := rag.New(rag.Options{Root: root, Model: "test"})
	diff := "diff --git a/tmp/hack.py b/tmp/hack.py\n+++ b/tmp/hack.py\n+_ = err\n+TODO: later\n"
	res, err := (&Reviewer{Index: idx}).Run(context.Background(), diff)
	if err != nil {
		t.Fatal(err)
	}
	if res.Model {
		t.Fatal("model should be off")
	}
	for _, title := range []string{"## Потенциальные баги", "## Архитектурные проблемы", "## Рекомендации"} {
		if !strings.Contains(res.Text, title) {
			t.Fatalf("missing %s\n%s", title, res.Text)
		}
	}
	if !strings.Contains(res.Text, "ошибка отброшена") || !strings.Contains(res.Text, "tmp/hack.py") {
		t.Fatalf("text:\n%s", res.Text)
	}
}

func TestGitDiffHead(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-b", "day32")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "README.md")
	git("commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diff, err := GitDiff(root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "README.md") || !strings.Contains(diff, "+two") {
		t.Fatalf("diff:\n%s", diff)
	}
}

func TestPostNote(t *testing.T) {
	var gotHeader, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/projects/12/merge_requests/3/notes" {
			t.Errorf("path %s", r.URL.Path)
		}
		gotHeader = r.Header.Get("JOB-TOKEN")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	err := PostNote(context.Background(), srv.URL+"/api/v4", "12", "3", "job-secret", "JOB-TOKEN", "## Потенциальные баги\nнет")
	if err != nil {
		t.Fatal(err)
	}
	if gotHeader != "job-secret" || !strings.Contains(gotBody, "Потенциальные баги") {
		t.Fatalf("header %q body %s", gotHeader, gotBody)
	}
}

type wordEmbedder struct{}

func (wordEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		low := strings.ToLower(text)
		vec := []float32{0, 0}
		if strings.Contains(low, "review") || strings.Contains(low, "cmd/review") {
			vec[0] = 1
		}
		if strings.Contains(low, "архитектур") {
			vec[1] = 1
		}
		out[i] = vec
	}
	return out, nil
}

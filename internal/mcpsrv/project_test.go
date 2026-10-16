package mcpsrv

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectToolsBranchFilesDiff(t *testing.T) {
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
	git("init", "-b", "day31")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# day31\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "api.md"), []byte("# API\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(root, "docs", "extra.md"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tools := map[string]func(json.RawMessage) (string, error){}
	for _, tool := range ProjectTools(root) {
		tools[tool.Name] = tool.Handle
	}

	raw, err := tools["git_branch"](nil)
	if err != nil {
		t.Fatal(err)
	}
	var branch struct {
		Branch string `json:"branch"`
		Head   string `json:"head"`
		Dirty  int    `json:"dirty"`
	}
	if err := json.Unmarshal([]byte(raw), &branch); err != nil {
		t.Fatal(err)
	}
	if branch.Branch != "day31" || branch.Head == "" || branch.Dirty < 1 {
		t.Fatalf("branch: %#v", branch)
	}

	raw, err = tools["list_files"]([]byte(`{"depth":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid([]byte(raw)) || !strings.Contains(raw, "README.md") || !strings.Contains(raw, "docs/") {
		t.Fatalf("tree: %s", raw)
	}

	if _, err := tools["list_files"]([]byte(`{"path":"../secret"}`)); err == nil {
		t.Fatal("path escape must fail")
	}

	raw, err = tools["git_diff"]([]byte(`{"stat_only":true}`))
	if err != nil {
		t.Fatal(err)
	}
	var diff struct {
		Status string `json:"status"`
		Stat   string `json:"stat"`
		Patch  string `json:"patch"`
	}
	if err := json.Unmarshal([]byte(raw), &diff); err != nil {
		t.Fatal(err)
	}
	if diff.Patch != "" {
		t.Fatalf("stat_only returned patch: %s", diff.Patch)
	}
	if !strings.Contains(diff.Status+"\n"+diff.Stat, "extra.md") {
		t.Fatalf("diff: %#v", diff)
	}
}

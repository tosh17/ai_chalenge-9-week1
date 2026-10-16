package mcpsrv

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ProjectTools — живой контекст репозитория: ветка, файлы, diff.
func ProjectTools(root string) []Tool {
	root = strings.TrimSpace(root)
	if root == "" {
		root, _ = os.Getwd()
	}
	return []Tool{
		{
			Name:        "git_branch",
			Description: "Текущая git-ветка, короткий HEAD и число несохранённых путей.",
			Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
			Handle: func(args json.RawMessage) (string, error) {
				return gitBranch(root)
			},
		},
		{
			Name:        "list_files",
			Description: "Дерево файлов проекта. path — относительный каталог, depth — глубина (по умолчанию 2).",
			Schema:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"depth":{"type":"integer"}},"required":[]}`),
			Handle: func(args json.RawMessage) (string, error) {
				var in struct {
					Path  string `json:"path"`
					Depth int    `json:"depth"`
				}
				_ = json.Unmarshal(args, &in)
				return listFiles(root, in.Path, in.Depth)
			},
		},
		{
			Name:        "git_diff",
			Description: "git status и diff рабочей копии. stat_only=true возвращает только сводку. path ограничивает diff одним путём.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"staged":{"type":"boolean"},"stat_only":{"type":"boolean"}},"required":[]}`),
			Handle: func(args json.RawMessage) (string, error) {
				var in struct {
					Path     string `json:"path"`
					Staged   bool   `json:"staged"`
					StatOnly bool   `json:"stat_only"`
				}
				_ = json.Unmarshal(args, &in)
				return gitDiff(root, in.Path, in.Staged, in.StatOnly)
			},
		},
	}
}

func gitBranch(root string) (string, error) {
	branch, err := gitOut(root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git branch: %s", branch)
	}
	head, _ := gitOut(root, "rev-parse", "--short", "HEAD")
	porcelain, _ := gitOut(root, "status", "--porcelain")
	dirty := 0
	if porcelain != "" {
		dirty = len(strings.Split(porcelain, "\n"))
	}
	raw, err := json.Marshal(map[string]any{
		"branch": branch,
		"head":   head,
		"dirty":  dirty,
	})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func listFiles(root, rel string, depth int) (string, error) {
	if depth <= 0 {
		depth = 2
	}
	if depth > 6 {
		depth = 6
	}
	base, err := withinRoot(root, rel)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(base)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory")
	}
	var b strings.Builder
	count := 0
	err = filepath.WalkDir(base, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if path == base {
			return nil
		}
		name := d.Name()
		if d.IsDir() && skipDir(name) {
			return filepath.SkipDir
		}
		relPath, err := filepath.Rel(base, path)
		if err != nil {
			return nil
		}
		level := strings.Count(relPath, string(os.PathSeparator))
		if level >= depth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if count >= 200 {
			return fs.SkipAll
		}
		indent := strings.Repeat("  ", level)
		label := name
		if d.IsDir() {
			label += "/"
		}
		fmt.Fprintf(&b, "%s%s\n", indent, label)
		count++
		return nil
	})
	if err != nil {
		return "", err
	}
	shown := strings.TrimRight(b.String(), "\n")
	raw, err := json.Marshal(map[string]any{
		"path":  displayPath(rel),
		"depth": depth,
		"count": count,
		"tree":  shown,
	})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func gitDiff(root, rel string, staged, statOnly bool) (string, error) {
	if _, err := withinRoot(root, rel); err != nil {
		return "", err
	}
	status, _ := gitOut(root, "status", "-sb")
	args := []string{"diff", "--stat"}
	if staged {
		args = append(args, "--cached")
	}
	if rel != "" && rel != "." {
		args = append(args, "--", filepath.Clean(rel))
	}
	stat, _ := gitOut(root, args...)
	patch := ""
	if !statOnly {
		patchArgs := []string{"diff"}
		if staged {
			patchArgs = append(patchArgs, "--cached")
		}
		if rel != "" && rel != "." {
			patchArgs = append(patchArgs, "--", filepath.Clean(rel))
		}
		full, _ := gitOut(root, patchArgs...)
		patch = clip(full, 4000)
	}
	raw, err := json.Marshal(map[string]any{
		"status": status,
		"stat":   stat,
		"patch":  patch,
	})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func gitOut(root string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	return text, err
}

func withinRoot(root, rel string) (string, error) {
	root = filepath.Clean(root)
	rel = strings.TrimSpace(rel)
	if rel == "" || rel == "." {
		return root, nil
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("path must be relative")
	}
	cleaned := filepath.Clean(rel)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes repository")
	}
	return filepath.Join(root, cleaned), nil
}

func skipDir(name string) bool {
	switch name {
	case ".git", "bin", "data", "build", ".gradle", "node_modules", ".cursor", ".omp":
		return true
	default:
		return false
	}
}

func displayPath(rel string) string {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "."
	}
	return filepath.ToSlash(rel)
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "\n…"
}

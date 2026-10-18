package files

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/tosh17/deepseek-service/internal/mcpsrv"
)

// Tools читает и правит файлы настроек внутри корня. Модель выбирает путь сама.
func Handlers(root string) []mcpsrv.Tool {
	if root == "" {
		root = filepath.Join("ops", "configs")
	}
	return []mcpsrv.Tool{
		{
			Name:        "search_text",
			Description: "Найти строку сразу в нескольких файлах настроек.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
			Handle: func(args json.RawMessage) (string, error) {
				var in struct {
					Query string `json:"query"`
				}
				_ = json.Unmarshal(args, &in)
				return search(root, in.Query)
			},
		},
		{
			Name:        "read_file",
			Description: "Прочитать файл настроек по относительному пути.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
			Handle: func(args json.RawMessage) (string, error) {
				var in struct {
					Path string `json:"path"`
				}
				_ = json.Unmarshal(args, &in)
				return read(root, in.Path)
			},
		},
		{
			Name:        "write_file",
			Description: "Записать файл целиком. В ответе будет diff.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`),
			Handle: func(args json.RawMessage) (string, error) {
				var in struct {
					Path    string `json:"path"`
					Content string `json:"content"`
				}
				_ = json.Unmarshal(args, &in)
				return write(root, in.Path, in.Content)
			},
		},
	}
}

func search(root, query string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("нужен query")
	}
	var hits []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || len(hits) >= 40 {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || strings.Contains(rel, "..") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil || bytes.Contains(body, []byte{0}) {
			return nil
		}
		for i, line := range strings.Split(string(body), "\n") {
			if strings.Contains(line, query) {
				hits = append(hits, fmt.Sprintf("%s:%d: %s", filepath.ToSlash(rel), i+1, clip(strings.TrimSpace(line), 180)))
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(hits) == 0 {
		return "совпадений нет", nil
	}
	return strings.Join(hits, "\n"), nil
}

func read(root, rel string) (string, error) {
	full, rel, err := safe(root, rel)
	if err != nil {
		return "", err
	}
	body, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	return "# " + rel + "\n" + string(body), nil
}

func write(root, rel, content string) (string, error) {
	full, rel, err := safe(root, rel)
	if err != nil {
		return "", err
	}
	if len(content) > 200*1024 {
		return "", fmt.Errorf("файл слишком большой")
	}
	old, _ := os.ReadFile(full)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return "", err
	}
	return unified(rel, string(old), content), nil
}

func safe(root, rel string) (string, string, error) {
	rel = strings.TrimSpace(filepath.ToSlash(rel))
	if rel == "" || filepath.IsAbs(rel) {
		return "", "", fmt.Errorf("нужен относительный путь")
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", "", fmt.Errorf("путь выходит из настроек")
	}
	base := strings.ToLower(filepath.Base(clean))
	if base == ".env" || strings.HasPrefix(base, ".env.") || strings.Contains(base, "id_rsa") {
		return "", "", fmt.Errorf("этот путь закрыт")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", "", err
	}
	full := filepath.Join(rootAbs, clean)
	fullAbs, err := filepath.Abs(full)
	if err != nil {
		return "", "", err
	}
	if fullAbs != rootAbs && !strings.HasPrefix(fullAbs, rootAbs+string(os.PathSeparator)) {
		return "", "", fmt.Errorf("путь выходит из настроек")
	}
	return fullAbs, filepath.ToSlash(clean), nil
}

func unified(path, old, next string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "diff --git a/%s b/%s\n--- a/%s\n+++ b/%s\n", path, path, path, path)
	if old == next {
		b.WriteString("без изменений\n")
		return b.String()
	}
	if old != "" {
		for _, line := range strings.Split(old, "\n") {
			fmt.Fprintf(&b, "-%s\n", line)
		}
	}
	for _, line := range strings.Split(next, "\n") {
		fmt.Fprintf(&b, "+%s\n", line)
	}
	return b.String()
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

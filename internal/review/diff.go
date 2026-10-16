package review

import (
	"fmt"
	"os/exec"
	"strings"
)

// FileChange — один файл из unified diff.
type FileChange struct {
	Path  string
	Patch string
}

// ParseDiff режет `git diff` на файлы. Путь берётся из `b/`.
func ParseDiff(diff string) []FileChange {
	diff = strings.ReplaceAll(diff, "\r\n", "\n")
	if strings.TrimSpace(diff) == "" {
		return nil
	}
	var out []FileChange
	var cur *FileChange
	flush := func() {
		if cur == nil {
			return
		}
		cur.Patch = strings.TrimRight(cur.Patch, "\n")
		out = append(out, *cur)
		cur = nil
	}
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			flush()
			cur = &FileChange{Path: pathFromGit(line)}
		}
		if cur == nil {
			continue
		}
		cur.Patch += line + "\n"
	}
	flush()
	return out
}

func pathFromGit(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return ""
	}
	path := fields[len(fields)-1]
	path = strings.TrimPrefix(path, "b/")
	path = strings.Trim(path, "\"")
	return path
}

// GitDiff возвращает unified diff.
// head пустой или HEAD — рабочее дерево против base (или против HEAD, если base тоже пустой).
// Явный SHA, как CI_COMMIT_SHA, сравнивается через base...head.
func GitDiff(root, base, head string) (string, error) {
	args := []string{"diff", "--no-ext-diff", "--unified=3"}
	switch {
	case base != "" && head != "" && head != "HEAD":
		args = append(args, base+"..."+head)
	case base != "":
		args = append(args, base)
	default:
		args = append(args, "HEAD")
	}
	text, err := gitCombined(root, args...)
	if err != nil {
		return text, fmt.Errorf("git diff: %s", text)
	}
	if head == "" || head == "HEAD" {
		extra, err := untrackedDiff(root)
		if err != nil {
			return text, err
		}
		if extra != "" {
			if text != "" {
				text += "\n"
			}
			text += extra
		}
	}
	return text, nil
}

func untrackedDiff(root string) (string, error) {
	list, err := gitCombined(root, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return "", fmt.Errorf("git ls-files: %s", list)
	}
	var b strings.Builder
	for _, rel := range strings.Split(list, "\n") {
		rel = strings.TrimSpace(rel)
		if rel == "" || strings.HasPrefix(rel, ".cursor/") || strings.HasPrefix(rel, ".omp/") {
			continue
		}
		piece, err := gitCombined(root, "diff", "--no-index", "--no-ext-diff", "--unified=3", "--", "/dev/null", rel)
		if err != nil && piece == "" {
			return "", fmt.Errorf("git diff --no-index %s: %s", rel, piece)
		}
		piece = strings.TrimSpace(piece)
		if piece == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(piece)
	}
	return b.String(), nil
}

func gitCombined(root string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// AddedLines — строки, которые diff добавляет, без заголовков `+++`.
func AddedLines(patch string) []string {
	var lines []string
	for _, line := range strings.Split(patch, "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			lines = append(lines, strings.TrimPrefix(line, "+"))
		}
	}
	return lines
}

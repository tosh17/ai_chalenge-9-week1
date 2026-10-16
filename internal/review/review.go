package review

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/tosh17/deepseek-service/internal/rag"
)

const systemPrompt = `Ты ревьюер этого репозитория. Пиши по-русски, по делу.
Опирайся на diff и на фрагменты документации и кода. Не выдумывай файлы и функции, которых там нет.
Ответ строго из трёх разделов:
## Потенциальные баги
## Архитектурные проблемы
## Рекомендации
Если в разделе нечего сказать, напиши «нет».`

// Completer — один ответ модели без памяти чата.
type Completer func(ctx context.Context, system, user string) (string, error)

// Result — текст ревью и откуда он собран.
type Result struct {
	Text       string
	Files      []string
	Sources    []rag.Hit
	Mode       string
	Model      bool
	ModelError string
}

// Reviewer собирает diff, RAG и текст ревью.
type Reviewer struct {
	Index    *rag.Index
	Complete Completer
	TopK     int
}

// Run читает diff и возвращает ревью.
func (r *Reviewer) Run(ctx context.Context, diff string) (Result, error) {
	if r == nil || r.Index == nil {
		return Result{}, fmt.Errorf("review: index is off")
	}
	changes := ParseDiff(diff)
	files := make([]string, 0, len(changes))
	for _, ch := range changes {
		if ch.Path != "" {
			files = append(files, ch.Path)
		}
	}
	query := strings.Join(files, " ")
	if query == "" {
		query = clip(diff, 400)
	}
	k := r.TopK
	if k <= 0 {
		k = 6
	}
	hits, err := r.Index.Search(ctx, query+" баги архитектура", k)
	if err != nil {
		return Result{}, err
	}
	st := r.Index.Status()
	prompt := buildPrompt(diff, changes, hits)
	out := Result{Files: files, Sources: hits, Mode: st.Mode}
	if r.Complete != nil {
		text, err := r.Complete(ctx, systemPrompt, prompt)
		if err == nil && strings.TrimSpace(text) != "" {
			out.Text = ensureSections(strings.TrimSpace(text))
			out.Model = true
			return out, nil
		}
		if err != nil {
			out.ModelError = err.Error()
		}
	}
	out.Text = extractive(changes, hits)
	return out, nil
}

func buildPrompt(diff string, changes []FileChange, hits []rag.Hit) string {
	var b strings.Builder
	b.WriteString("## Изменённые файлы\n")
	if len(changes) == 0 {
		b.WriteString("нет\n")
	}
	for _, ch := range changes {
		fmt.Fprintf(&b, "- %s\n", ch.Path)
	}
	b.WriteString("\n## Diff\n")
	b.WriteString(clip(diff, 9000))
	b.WriteString("\n\n## Фрагменты документации и кода\n")
	if len(hits) == 0 {
		b.WriteString("не найдены\n")
	}
	for i, hit := range hits {
		fmt.Fprintf(&b, "\n### %d. %s", i+1, hit.Path)
		if hit.Heading != "" {
			fmt.Fprintf(&b, " — %s", hit.Heading)
		}
		fmt.Fprintf(&b, "\n%s\n", clip(hit.Text, 900))
	}
	return b.String()
}

func ensureSections(text string) string {
	for _, title := range []string{"## Потенциальные баги", "## Архитектурные проблемы", "## Рекомендации"} {
		if !strings.Contains(text, title) {
			text += "\n\n" + title + "\nнет"
		}
	}
	return strings.TrimSpace(text)
}

func extractive(changes []FileChange, hits []rag.Hit) string {
	var b strings.Builder
	b.WriteString("## Потенциальные баги\n")
	bugs := bugLines(changes)
	if len(bugs) == 0 {
		b.WriteString("Явных маркеров в добавленных строках нет.\n")
	} else {
		for _, line := range bugs {
			fmt.Fprintf(&b, "- %s\n", line)
		}
	}
	b.WriteString("\n## Архитектурные проблемы\n")
	notes := archNotes(changes, hits)
	if len(notes) == 0 {
		b.WriteString("нет\n")
	} else {
		for _, note := range notes {
			fmt.Fprintf(&b, "- %s\n", note)
		}
	}
	b.WriteString("\n## Рекомендации\n")
	if len(hits) == 0 {
		b.WriteString("Сверь изменение с README и docs/, индекс ничего не вернул.\n")
	} else {
		b.WriteString("Сверь diff с ближайшими фрагментами:\n")
		for _, hit := range hits {
			fmt.Fprintf(&b, "- `%s`", hit.Path)
			if hit.Heading != "" {
				fmt.Fprintf(&b, " — %s", hit.Heading)
			}
			b.WriteByte('\n')
		}
	}
	return strings.TrimSpace(b.String())
}

func bugLines(changes []FileChange) []string {
	var out []string
	for _, ch := range changes {
		for _, line := range AddedLines(ch.Patch) {
			trim := strings.TrimSpace(line)
			why := ""
			switch {
			case strings.Contains(trim, "panic("):
				why = "panic в новом коде"
			case strings.Contains(trim, "FIXME") || strings.Contains(trim, "TODO") || strings.Contains(trim, "XXX"):
				why = "незакрытая пометка"
			case strings.HasPrefix(trim, "_ =") || strings.Contains(trim, "_ = err"):
				why = "ошибка отброшена"
			}
			if why == "" {
				continue
			}
			out = append(out, fmt.Sprintf("%s: %s — %s", ch.Path, why, clip(trim, 160)))
			if len(out) >= 8 {
				return out
			}
		}
	}
	return out
}

func archNotes(changes []FileChange, hits []rag.Hit) []string {
	var notes []string
	for _, ch := range changes {
		if ch.Path == "" {
			continue
		}
		if !strings.HasPrefix(ch.Path, "cmd/") && !strings.HasPrefix(ch.Path, "internal/") && !strings.HasPrefix(ch.Path, "docs/") && !strings.HasPrefix(ch.Path, "mcp/") && ch.Path != "README.md" && !strings.HasPrefix(ch.Path, ".gitlab-ci") {
			notes = append(notes, ch.Path+" лежит вне cmd, internal, docs и mcp")
		}
	}
	for _, hit := range hits {
		if strings.Contains(hit.Path, "architecture") || strings.Contains(hit.Path, "data-schemas") {
			notes = append(notes, "рядом в индексе "+hit.Path+", проверь, что diff ему не противоречит")
			break
		}
	}
	if len(notes) > 6 {
		notes = notes[:6]
	}
	return notes
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

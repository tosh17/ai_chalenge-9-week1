package help

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tosh17/deepseek-service/internal/agent"
	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/mcp"
	"github.com/tosh17/deepseek-service/internal/rag"
	"github.com/tosh17/deepseek-service/internal/tokens"
)

const defaultQuestion = "Как устроен этот проект: каталоги, API и команда /help?"

// Tools — узкий вызов MCP-инструментов проекта.
type Tools interface {
	Call(ctx context.Context, name string, arguments json.RawMessage) (string, error)
	Names() []string
}

// Completer — разовый ответ модели без памяти кулинарного чата.
type Completer interface {
	Complete(ctx context.Context, providerID, system string, history []deepseek.Message) (agent.Result, error)
}

// Query — вопрос после /help и выбранный провайдер чата.
type Query struct {
	Text     string
	Provider string
}

// Source — фрагмент, который попал в ответ.
type Source struct {
	Path    string  `json:"path"`
	Heading string  `json:"heading,omitempty"`
	Score   float64 `json:"score"`
}

// Meta — откуда собран ответ. Уходит в UI под пузырём.
type Meta struct {
	Question string   `json:"question"`
	Mode     string   `json:"mode"`
	Branch   string   `json:"branch,omitempty"`
	Head     string   `json:"head,omitempty"`
	Dirty    int      `json:"dirty,omitempty"`
	Sources  []Source `json:"sources,omitempty"`
	Tools    []string `json:"tools,omitempty"`
	Degraded bool     `json:"degraded,omitempty"`
}

// Answer — текст для чата и карточка источников.
type Answer struct {
	Reply      string
	Provider   string
	Model      string
	DurationMs int64
	Tokens     *tokens.Usage
	Debug      *deepseek.DebugInfo
	Meta       Meta
}

// Status — индекс RAG и подключённые инструменты.
type Status struct {
	rag.Status
	MCP []string `json:"mcp,omitempty"`
}

// Assistant отвечает на /help по документации и живому git-контексту.
type Assistant struct {
	index     *rag.Index
	tools     Tools
	completer Completer
}

func New(index *rag.Index, tools Tools, completer Completer) *Assistant {
	return &Assistant{index: index, tools: tools, completer: completer}
}

// Warm прогревает индекс до первого вопроса.
func (a *Assistant) Warm(ctx context.Context) error {
	if a == nil || a.index == nil {
		return fmt.Errorf("help: index is off")
	}
	return a.index.Ensure(ctx)
}

func (a *Assistant) Status() Status {
	var st Status
	if a != nil && a.index != nil {
		st.Status = a.index.Status()
	}
	if a != nil && a.tools != nil {
		st.MCP = a.tools.Names()
	}
	return st
}

// Parse отличает /help от обычного сообщения.
func Parse(text string) (string, bool) {
	t := strings.TrimSpace(text)
	if strings.EqualFold(t, "/help") {
		return "", true
	}
	lower := strings.ToLower(t)
	if strings.HasPrefix(lower, "/help ") || strings.HasPrefix(lower, "/help\n") {
		return strings.TrimSpace(t[len("/help"):]), true
	}
	return "", false
}

// Answer собирает RAG, MCP и ответ модели.
func (a *Assistant) Answer(ctx context.Context, q Query) (Answer, error) {
	if a == nil || a.index == nil {
		return Answer{}, fmt.Errorf("ассистент документации выключен")
	}
	question := strings.TrimSpace(q.Text)
	if question == "" {
		question = defaultQuestion
	}
	hits, err := a.index.Search(ctx, question, 0)
	if err != nil {
		return Answer{}, err
	}
	snap, used := a.project(ctx, question)
	st := a.index.Status()
	meta := Meta{
		Question: question,
		Mode:     st.Mode,
		Branch:   snap.Branch,
		Head:     snap.Head,
		Dirty:    snap.Dirty,
		Sources:  sourcesOf(hits),
		Tools:    used,
	}
	prompt := buildPrompt(question, hits, snap)
	if a.completer == nil {
		meta.Degraded = true
		return Answer{Reply: extractive(question, hits, snap, "модель не подключена"), Meta: meta}, nil
	}
	start := time.Now()
	result, err := a.complete(ctx, q.Provider, prompt)
	if err != nil || strings.TrimSpace(result.Reply) == "" {
		reason := "пустой ответ модели"
		if err != nil {
			reason = err.Error()
		}
		meta.Degraded = true
		return Answer{
			Reply:      extractive(question, hits, snap, reason),
			Provider:   result.Provider,
			Model:      result.Model,
			DurationMs: time.Since(start).Milliseconds(),
			Tokens:     result.Tokens,
			Debug:      result.Debug,
			Meta:       meta,
		}, nil
	}
	return Answer{
		Reply:      strings.TrimSpace(result.Reply),
		Provider:   result.Provider,
		Model:      result.Model,
		DurationMs: result.DurationMs,
		Tokens:     result.Tokens,
		Debug:      result.Debug,
		Meta:       meta,
	}, nil
}

type snapshot struct {
	Branch string
	Head   string
	Dirty  int
	Files  string
	Diff   string
}

func (a *Assistant) complete(ctx context.Context, provider, prompt string) (agent.Result, error) {
	messages := []deepseek.Message{{Role: "user", Content: prompt}}
	result, err := a.completer.Complete(ctx, provider, systemPrompt, messages)
	if err == nil && strings.TrimSpace(result.Reply) != "" {
		return result, nil
	}
	first := provider
	if first == "" {
		first = result.Provider
	}
	alt := agent.ProviderDeepSeek
	if first == agent.ProviderDeepSeek {
		alt = agent.ProviderLocal
	}
	if alt == first {
		return result, err
	}
	second, err2 := a.completer.Complete(ctx, alt, systemPrompt, messages)
	if err2 == nil && strings.TrimSpace(second.Reply) != "" {
		return second, nil
	}
	if err != nil {
		return result, err
	}
	return result, err2
}

func (a *Assistant) project(ctx context.Context, question string) (snapshot, []string) {
	if a.tools == nil {
		return snapshot{}, nil
	}
	var snap snapshot
	var used []string
	if raw, err := a.tools.Call(ctx, "git_branch", json.RawMessage(`{}`)); err == nil {
		used = append(used, "git_branch")
		var payload struct {
			Branch string `json:"branch"`
			Head   string `json:"head"`
			Dirty  int    `json:"dirty"`
		}
		if json.Unmarshal([]byte(raw), &payload) == nil {
			snap.Branch = payload.Branch
			snap.Head = payload.Head
			snap.Dirty = payload.Dirty
		}
	}
	if raw, err := a.tools.Call(ctx, "list_files", json.RawMessage(`{"depth":2}`)); err == nil {
		used = append(used, "list_files")
		var payload struct {
			Tree string `json:"tree"`
		}
		if json.Unmarshal([]byte(raw), &payload) == nil {
			snap.Files = clip(payload.Tree, 3500)
		}
	}
	diffArgs := json.RawMessage(`{"stat_only":true}`)
	if wantsPatch(question) {
		diffArgs = json.RawMessage(`{}`)
	}
	if raw, err := a.tools.Call(ctx, "git_diff", diffArgs); err == nil {
		used = append(used, "git_diff")
		var payload struct {
			Status string `json:"status"`
			Stat   string `json:"stat"`
			Patch  string `json:"patch"`
		}
		if json.Unmarshal([]byte(raw), &payload) == nil {
			parts := make([]string, 0, 3)
			if payload.Status != "" {
				parts = append(parts, payload.Status)
			}
			if payload.Stat != "" {
				parts = append(parts, payload.Stat)
			}
			if payload.Patch != "" {
				parts = append(parts, clip(payload.Patch, 2500))
			}
			snap.Diff = strings.Join(parts, "\n")
		}
	}
	return snap, used
}

func wantsPatch(question string) bool {
	q := strings.ToLower(question)
	for _, word := range []string{"diff", "дифф", "изменен", "изменил", "поменял", "uncommitted", "незакоммит"} {
		if strings.Contains(q, word) {
			return true
		}
	}
	return false
}

const systemPrompt = `Ты ассистент разработчика этого репозитория. Отвечай по-русски, коротко и по делу.
Опирайся только на фрагменты документации и блок «Репозиторий». Не выдумывай файлы, ручки и поля.
Если в контексте нет ответа, так и скажи. В конце перечисли 1–3 пути файлов, на которые опирался.`

func buildPrompt(question string, hits []rag.Hit, snap snapshot) string {
	var b strings.Builder
	b.WriteString("## Репозиторий\n")
	if snap.Branch != "" {
		fmt.Fprintf(&b, "Ветка: %s", snap.Branch)
		if snap.Head != "" {
			fmt.Fprintf(&b, " (%s)", snap.Head)
		}
		fmt.Fprintf(&b, ", незакоммиченных путей: %d\n", snap.Dirty)
	} else {
		b.WriteString("Git-контекст недоступен.\n")
	}
	if snap.Files != "" {
		b.WriteString("\nФайлы:\n")
		b.WriteString(snap.Files)
		b.WriteString("\n")
	}
	if snap.Diff != "" {
		b.WriteString("\nDiff:\n")
		b.WriteString(snap.Diff)
		b.WriteString("\n")
	}
	b.WriteString("\n## Документация\n")
	if len(hits) == 0 {
		b.WriteString("Фрагменты не найдены.\n")
	}
	for i, hit := range hits {
		fmt.Fprintf(&b, "\n### %d. %s", i+1, hit.Path)
		if hit.Heading != "" {
			fmt.Fprintf(&b, " — %s", hit.Heading)
		}
		fmt.Fprintf(&b, " (%.2f)\n%s\n", hit.Score, clip(hit.Text, 1200))
	}
	fmt.Fprintf(&b, "\n## Вопрос\n%s\n", question)
	return b.String()
}

func extractive(question string, hits []rag.Hit, snap snapshot, reason string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Модель не ответила (%s). Ниже документация и состояние репозитория по вопросу «%s».\n", shortReason(reason), question)
	if snap.Branch != "" {
		fmt.Fprintf(&b, "\nВетка %s", snap.Branch)
		if snap.Head != "" {
			fmt.Fprintf(&b, " (%s)", snap.Head)
		}
		fmt.Fprintf(&b, ", незакоммиченных путей: %d.\n", snap.Dirty)
	}
	if snap.Files != "" {
		b.WriteString("\nДерево:\n")
		b.WriteString(clip(snap.Files, 1500))
		b.WriteString("\n")
	}
	for _, hit := range hits {
		fmt.Fprintf(&b, "\n%s", hit.Path)
		if hit.Heading != "" {
			fmt.Fprintf(&b, " — %s", hit.Heading)
		}
		b.WriteString("\n")
		b.WriteString(clip(hit.Text, 500))
		b.WriteString("\n")
	}
	if len(hits) == 0 && snap.Branch == "" {
		b.WriteString("\nВ индексе и в git ничего не нашлось.\n")
	}
	return strings.TrimSpace(b.String())
}

func sourcesOf(hits []rag.Hit) []Source {
	out := make([]Source, 0, len(hits))
	for _, hit := range hits {
		out = append(out, Source{Path: hit.Path, Heading: hit.Heading, Score: hit.Score})
	}
	return out
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}

func shortReason(s string) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= 180 {
		return s
	}
	return string([]rune(s)[:180]) + "…"
}

// MCPTools адаптирует stdio-клиент к ассистенту.
type MCPTools struct {
	Client *mcp.Client
}

func (m MCPTools) Names() []string {
	if m.Client == nil {
		return nil
	}
	tools := m.Client.Tools()
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

func (m MCPTools) Call(ctx context.Context, name string, arguments json.RawMessage) (string, error) {
	if m.Client == nil {
		return "", fmt.Errorf("mcp project is off")
	}
	res, err := m.Client.Call(ctx, name, arguments)
	if err != nil {
		return "", err
	}
	if res.IsError {
		return "", fmt.Errorf("%s", res.Text)
	}
	return res.Text, nil
}

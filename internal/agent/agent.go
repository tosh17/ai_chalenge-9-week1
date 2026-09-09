// Package agent — простой LLM-агент как отдельная сущность.
// Инкапсулирует подготовку запроса, выбор бэкенда, вызов модели и разбор ответа.
package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/tosh17/deepseek-service/internal/deepseek"
)

const (
	ProviderDeepSeek = "deepseek"
	ProviderLocal    = "local"
)

// LLM — минимальный контракт модели, который использует агент.
type LLM interface {
	Chat(ctx context.Context, messages []deepseek.Message) (deepseek.ChatResult, error)
}

// Provider — доступный бэкенд для UI-переключателя.
type Provider struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Model string `json:"model"`
}

// Request — вход агента от пользователя / UI.
type Request struct {
	Message  string
	History  []deepseek.Message
	Provider string // deepseek | local
}

// Result — выход агента: ответ и служебные метаданные.
type Result struct {
	Reply      string
	Provider   string
	Model      string
	DurationMs int64
	Debug      *deepseek.DebugInfo
}

type backend struct {
	title string
	model string
	llm   LLM
}

// Agent — самостоятельная сущность: принимает запрос, ходит в выбранный LLM, отдаёт результат.
type Agent struct {
	name         string
	systemPrompt string
	defaultID    string
	backends     map[string]backend
	order        []string
}

// New создаёт агента без бэкендов — добавляйте через WithBackend.
func New(name string) *Agent {
	if name == "" {
		name = "chat-agent"
	}
	return &Agent{
		name:         name,
		systemPrompt: "Ты полезный ассистент. Отвечай кратко и по делу на языке пользователя.",
		backends:     make(map[string]backend),
		order:        nil,
		defaultID:    "",
	}
}

// WithBackend регистрирует LLM-бэкенд (deepseek, local, …).
func (a *Agent) WithBackend(id, title, model string, llm LLM) *Agent {
	if id == "" || llm == nil {
		return a
	}
	if _, exists := a.backends[id]; !exists {
		a.order = append(a.order, id)
	}
	a.backends[id] = backend{title: title, model: model, llm: llm}
	if a.defaultID == "" {
		a.defaultID = id
	}
	return a
}

// Name возвращает имя агента.
func (a *Agent) Name() string {
	return a.name
}

// Providers — список бэкендов для UI.
func (a *Agent) Providers() []Provider {
	out := make([]Provider, 0, len(a.order))
	for _, id := range a.order {
		b := a.backends[id]
		out = append(out, Provider{
			ID:    id,
			Title: b.title,
			Model: b.model,
		})
	}
	return out
}

// Handle обрабатывает один ход диалога через выбранный бэкенд.
func (a *Agent) Handle(ctx context.Context, req Request) (Result, error) {
	if req.Message == "" {
		return Result{}, fmt.Errorf("agent %q: message is required", a.name)
	}

	providerID := req.Provider
	if providerID == "" {
		providerID = a.defaultID
	}
	b, ok := a.backends[providerID]
	if !ok {
		return Result{}, fmt.Errorf("agent %q: unknown provider %q", a.name, providerID)
	}

	messages := a.buildMessages(req)
	start := time.Now()

	chat, err := b.llm.Chat(ctx, messages)
	duration := time.Since(start).Milliseconds()
	if err != nil {
		debug := chat.Debug
		return Result{
			Provider:   providerID,
			Model:      b.model,
			DurationMs: duration,
			Debug:      &debug,
		}, fmt.Errorf("agent %q [%s]: %w", a.name, providerID, err)
	}

	debug := chat.Debug
	return Result{
		Reply:      chat.Reply,
		Provider:   providerID,
		Model:      b.model,
		DurationMs: duration,
		Debug:      &debug,
	}, nil
}

func (a *Agent) buildMessages(req Request) []deepseek.Message {
	out := make([]deepseek.Message, 0, len(req.History)+2)
	if a.systemPrompt != "" {
		out = append(out, deepseek.Message{
			Role:    "system",
			Content: a.systemPrompt,
		})
	}
	for _, m := range req.History {
		if m.Role == "system" {
			continue
		}
		out = append(out, m)
	}
	out = append(out, deepseek.Message{
		Role:    "user",
		Content: req.Message,
	})
	return out
}

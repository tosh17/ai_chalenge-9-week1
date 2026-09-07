// Package agent — простой LLM-агент как отдельная сущность.
// Инкапсулирует подготовку запроса, вызов модели и разбор ответа.
package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/tosh17/deepseek-service/internal/deepseek"
)

// LLM — минимальный контракт модели, который использует агент.
type LLM interface {
	Chat(ctx context.Context, messages []deepseek.Message) (deepseek.ChatResult, error)
}

// Request — вход агента от пользователя / UI.
type Request struct {
	Message string
	History []deepseek.Message
}

// Result — выход агента: ответ и служебные метаданные.
type Result struct {
	Reply      string
	DurationMs int64
	Debug      *deepseek.DebugInfo
}

// Agent — самостоятельная сущность: принимает запрос, ходит в LLM, отдаёт результат.
type Agent struct {
	name         string
	systemPrompt string
	llm          LLM
}

// New создаёт агента поверх LLM-клиента.
func New(name string, llm LLM) *Agent {
	if name == "" {
		name = "chat-agent"
	}
	return &Agent{
		name: name,
		systemPrompt: "Ты полезный ассистент. Отвечай кратко и по делу на языке пользователя.",
		llm:  llm,
	}
}

// Name возвращает имя агента.
func (a *Agent) Name() string {
	return a.name
}

// Handle обрабатывает один ход диалога: собирает сообщения, вызывает LLM, возвращает ответ.
func (a *Agent) Handle(ctx context.Context, req Request) (Result, error) {
	if a.llm == nil {
		return Result{}, fmt.Errorf("agent %q: llm is not configured", a.name)
	}
	if req.Message == "" {
		return Result{}, fmt.Errorf("agent %q: message is required", a.name)
	}

	messages := a.buildMessages(req)
	start := time.Now()

	chat, err := a.llm.Chat(ctx, messages)
	duration := time.Since(start).Milliseconds()
	if err != nil {
		debug := chat.Debug
		return Result{
			DurationMs: duration,
			Debug:      &debug,
		}, fmt.Errorf("agent %q: %w", a.name, err)
	}

	debug := chat.Debug
	return Result{
		Reply:      chat.Reply,
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

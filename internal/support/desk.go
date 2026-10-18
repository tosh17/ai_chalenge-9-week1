package support

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/mcpsrv"
	"github.com/tosh17/deepseek-service/internal/rag"
)

const systemPrompt = `Ты поддержка домашнего сервера s1. Пользователь спрашивает о реальном сервисе, номера тикета нет.
Сначала вызови list_services, затем service_status и service_logs контейнера, о котором вопрос.
Не выдумывай строки логов и статус, которых нет в инструментах.
Ответ по-русски, коротко, без дампа логов. Два раздела:
## Причина
## Рекомендация`

const maxRounds = 6

// Chatter — модель, которая может вызвать инструменты.
type Chatter interface {
	ChatTools(ctx context.Context, messages []deepseek.Message, tools []deepseek.Tool) (deepseek.ChatResult, error)
}

// Tools — команды на s1.
type Tools interface {
	Definitions() []deepseek.Tool
	Call(ctx context.Context, name, arguments string) (string, error)
}

// Local вызывает обработчики MCP в процессе.
type Local struct {
	Tools []mcpsrv.Tool
}

func (l Local) Definitions() []deepseek.Tool {
	out := make([]deepseek.Tool, 0, len(l.Tools))
	for _, tool := range l.Tools {
		out = append(out, deepseek.Tool{Name: tool.Name, Description: tool.Description, Parameters: tool.Schema})
	}
	return out
}

func (l Local) Call(_ context.Context, name, arguments string) (string, error) {
	raw := strings.TrimSpace(arguments)
	if raw == "" {
		raw = "{}"
	}
	for _, tool := range l.Tools {
		if tool.Name == name {
			return tool.Handle(json.RawMessage(raw))
		}
	}
	return "", fmt.Errorf("неизвестный инструмент %s", name)
}

// Call — один вызов.
type Call struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Result    string `json:"result"`
	Err       string `json:"err,omitempty"`
}

// Answer — ответ пользователю.
type Answer struct {
	Reply   string   `json:"reply"`
	Calls   []Call   `json:"calls,omitempty"`
	Sources []string `json:"sources,omitempty"`
}

// Desk собирает логи s1 и FAQ.
type Desk struct {
	Index *rag.Index
	Chat  Chatter
	Tools Tools
}

// Reply отвечает на вопрос по живым логам s1.
func (d *Desk) Reply(ctx context.Context, question string) (Answer, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return Answer{}, fmt.Errorf("нужен вопрос")
	}
	if d.Tools == nil {
		return Answer{}, fmt.Errorf("инструменты не подключены")
	}
	var calls []Call
	reply := ""
	messages := []deepseek.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: question},
	}
	if d.Chat != nil {
		for round := 0; round < maxRounds; round++ {
			res, err := d.Chat.ChatTools(ctx, messages, d.Tools.Definitions())
			if err != nil {
				break
			}
			if len(res.ToolCalls) == 0 {
				reply = strings.TrimSpace(res.Reply)
				break
			}
			messages = append(messages, deepseek.Message{Role: "assistant", Content: res.Reply, ToolCalls: res.ToolCalls})
			for _, call := range res.ToolCalls {
				text, callErr := d.Tools.Call(ctx, call.Name, call.Arguments)
				item := Call{Name: call.Name, Arguments: call.Arguments, Result: clip(text, 4000)}
				if callErr != nil {
					item.Err = callErr.Error()
					text = callErr.Error()
				}
				calls = append(calls, item)
				messages = append(messages, deepseek.Message{Role: "tool", Name: call.Name, ToolCallID: call.ID, Content: clip(text, 4000)})
			}
		}
	}
	if !saw(calls, "service_logs") && !saw(calls, "service_status") && !saw(calls, "list_services") {
		extra := d.fallback(ctx, question)
		calls = append(calls, extra...)
		if strings.TrimSpace(reply) == "" {
			reply = extractive(question, calls, nil)
		}
	}
	var sources []string
	if d.Index != nil {
		hits, err := d.Index.Search(ctx, question+" "+blob(calls), 3)
		if err == nil {
			for _, hit := range hits {
				sources = append(sources, hit.Path)
			}
			if strings.TrimSpace(reply) == "" || !strings.Contains(reply, "## Причина") {
				reply = extractive(question, calls, hits)
			}
		}
	}
	if strings.TrimSpace(reply) == "" {
		reply = extractive(question, calls, nil)
	}
	return Answer{Reply: strings.TrimSpace(reply), Calls: calls, Sources: sources}, nil
}

func (d *Desk) fallback(ctx context.Context, question string) []Call {
	var calls []Call
	raw, err := d.Tools.Call(ctx, "list_services", "{}")
	calls = append(calls, done("list_services", "{}", raw, err))
	name := guessService(question)
	if name != "" {
		statusArgs := fmt.Sprintf(`{"name":%q}`, name)
		raw, err = d.Tools.Call(ctx, "service_status", statusArgs)
		calls = append(calls, done("service_status", statusArgs, raw, err))
		logArgs := fmt.Sprintf(`{"name":%q,"tail":80}`, name)
		raw, err = d.Tools.Call(ctx, "service_logs", logArgs)
		calls = append(calls, done("service_logs", logArgs, raw, err))
	}
	return calls
}

func extractive(question string, calls []Call, hits []rag.Hit) string {
	var b strings.Builder
	b.WriteString("## Причина\n")
	fmt.Fprintf(&b, "Вопрос: %s\n", question)
	for _, call := range calls {
		if call.Result == "" || call.Err != "" {
			continue
		}
		fmt.Fprintf(&b, "\n%s:\n%s\n", call.Name, clip(call.Result, 700))
	}
	b.WriteString("\n## Рекомендация\n")
	if len(hits) > 0 {
		b.WriteString(clip(hits[0].Text, 500))
		b.WriteByte('\n')
	} else {
		b.WriteString("Сверь статус контейнера и хвост логов. Если контейнер не healthy, не перезапускай соседние сервисы.\n")
	}
	return b.String()
}

func done(name, args, result string, err error) Call {
	item := Call{Name: name, Arguments: args, Result: clip(result, 4000)}
	if err != nil {
		item.Err = err.Error()
	}
	return item
}

func saw(calls []Call, name string) bool {
	for _, call := range calls {
		if call.Name == name && call.Err == "" {
			return true
		}
	}
	return false
}

func blob(calls []Call) string {
	var b strings.Builder
	for _, call := range calls {
		b.WriteString(call.Result)
		b.WriteByte(' ')
	}
	return b.String()
}

func guessService(question string) string {
	q := strings.ToLower(question)
	switch {
	case strings.Contains(q, "gitlab") || strings.Contains(q, "гит"):
		return "gitlab"
	case strings.Contains(q, "ollama") || strings.Contains(q, "эмбед") || strings.Contains(q, "контекст") || strings.Contains(q, "nomic"):
		return "ollama"
	case strings.Contains(q, "runner") || strings.Contains(q, "раннер") || strings.Contains(q, "джоб"):
		return "gitlab-runner"
	default:
		return ""
	}
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

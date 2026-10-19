package dock

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/files"
	"github.com/tosh17/deepseek-service/internal/mcpsrv"
	"github.com/tosh17/deepseek-service/internal/s1"
)

const systemPrompt = `Ты управляешь Docker на s1. Команду выбираешь сам и вызываешь инструмент MCP.
Смотри на предыдущие реплики: в них уже может быть имя контейнера.
Ресурсы, CPU, память и потребление — service_stats.
Что это за контейнер, образ, статус, порты — service_info.
Какие запущены — list_services.
Новая пустая страница — run_demo: имя ops-*, образ nginx:alpine или alpine:3.20, порт 18080–18090.
Удалять можно только remove_demo и только имя ops-*. GitLab, Ollama, runner и board не останавливай.
Если из диалога неясно, что сделать, задай один короткий вопрос и не вызывай инструмент.
Отвечай по-русски, только на вопрос. Не добавляй рекомендацию и блок «действие».`

const (
	maxRounds   = 4
	historyKeep = 8
	historyMin  = 5
)

// Chatter вызывает инструменты.
type Chatter interface {
	ChatTools(ctx context.Context, messages []deepseek.Message, tools []deepseek.Tool) (deepseek.ChatResult, error)
}

// Tools — вызовы docker и запись compose.
type Tools interface {
	Definitions() []deepseek.Tool
	Call(ctx context.Context, name, arguments string) (string, error)
}

// Local вызывает инструменты в процессе.
type Local struct{ Tools []mcpsrv.Tool }

func (l Local) Definitions() []deepseek.Tool {
	out := make([]deepseek.Tool, 0, len(l.Tools))
	for _, tool := range l.Tools {
		out = append(out, deepseek.Tool{Name: tool.Name, Description: tool.Description, Parameters: tool.Schema})
	}
	return out
}

func (l Local) Call(_ context.Context, name, arguments string) (string, error) {
	if strings.TrimSpace(arguments) == "" {
		arguments = "{}"
	}
	for _, tool := range l.Tools {
		if tool.Name == name {
			return tool.Handle(json.RawMessage(arguments))
		}
	}
	return "", fmt.Errorf("неизвестный инструмент %s", name)
}

// Call — один вызов.
type Call struct {
	Name, Arguments, Result, Err string
}

// Result — ответ и вызовы.
type Result struct {
	Reply string `json:"reply"`
	Calls []Call `json:"calls,omitempty"`
}

// Turn — реплика чата.
type Turn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Assistant отдаёт реплику модели, а выбранную команду — инструментам MCP.
type Assistant struct {
	Chat  Chatter
	Tools Tools
}

// Run выполняет цель.
func (a *Assistant) Run(ctx context.Context, goal string) (Result, error) {
	return a.Reply(ctx, goal, nil)
}

// Reply отвечает на реплику. history — предыдущие реплики, на сервер уходит не меньше пяти, если они есть.
func (a *Assistant) Reply(ctx context.Context, goal string, history []Turn) (Result, error) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return Result{}, fmt.Errorf("нужна цель")
	}
	if a.Tools == nil {
		return Result{}, fmt.Errorf("инструменты не подключены")
	}
	if a.Chat == nil {
		return Result{}, fmt.Errorf("модель не подключена")
	}
	messages := []deepseek.Message{{Role: "system", Content: systemPrompt}}
	for _, turn := range recentHistory(history) {
		messages = append(messages, deepseek.Message{Role: turn.Role, Content: turn.Content})
	}
	messages = append(messages, deepseek.Message{Role: "user", Content: goal})
	var calls []Call
	reply := ""
	for round := 0; round < maxRounds; round++ {
		res, err := a.Chat.ChatTools(ctx, messages, a.Tools.Definitions())
		if err != nil {
			if reply == "" && len(calls) == 0 {
				return Result{}, err
			}
			break
		}
		if len(res.ToolCalls) == 0 {
			reply = strings.TrimSpace(res.Reply)
			break
		}
		messages = append(messages, deepseek.Message{Role: "assistant", Content: res.Reply, ToolCalls: res.ToolCalls})
		for _, call := range res.ToolCalls {
			text, callErr := a.Tools.Call(ctx, call.Name, call.Arguments)
			item := Call{Name: call.Name, Arguments: call.Arguments, Result: clip(text, 4000)}
			if callErr != nil {
				item.Err = callErr.Error()
				text = callErr.Error()
			}
			calls = append(calls, item)
			messages = append(messages, deepseek.Message{Role: "tool", Name: call.Name, ToolCallID: call.ID, Content: clip(text, 4000)})
		}
	}
	if strings.TrimSpace(reply) == "" {
		reply = "Не понял, что сделать. Уточни, пожалуйста."
	}
	return Result{Reply: reply, Calls: calls}, nil
}

func recentHistory(history []Turn) []Turn {
	var out []Turn
	for _, turn := range history {
		role := strings.TrimSpace(turn.Role)
		content := strings.TrimSpace(turn.Content)
		if content == "" || (role != "user" && role != "assistant") {
			continue
		}
		out = append(out, Turn{Role: role, Content: content})
	}
	if len(out) > historyKeep {
		out = out[len(out)-historyKeep:]
	}
	return out
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// Handlers — docker и запись compose внутри каталога стеков.
func Handlers(run s1.Runner, stackRoot string) []mcpsrv.Tool {
	fileTools := files.Handlers(stackRoot)
	out := []mcpsrv.Tool{
		{
			Name:        "list_services",
			Description: "Список контейнеров s1: имя, статус, порты.",
			Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
			Handle: func(json.RawMessage) (string, error) {
				return run.Run(context.Background(), s1.ListServices())
			},
		},
		{
			Name:        "list_images",
			Description: "Локальные образы Docker.",
			Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
			Handle: func(json.RawMessage) (string, error) {
				return run.Run(context.Background(), s1.ListImages())
			},
		},
		{
			Name:        "service_info",
			Description: "Образ, статус и время старта одного контейнера. Не CPU и не память.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`),
			Handle: func(args json.RawMessage) (string, error) {
				var in struct {
					Name string `json:"name"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return "", err
				}
				argv, err := s1.Info(in.Name)
				if err != nil {
					return "", err
				}
				return run.Run(context.Background(), argv)
			},
		},
		{
			Name:        "service_stats",
			Description: "CPU и память. Имя контейнера, если спрашивают про него. Без имени — все контейнеры.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`),
			Handle: func(args json.RawMessage) (string, error) {
				var in struct {
					Name string `json:"name"`
				}
				_ = json.Unmarshal(args, &in)
				argv, err := s1.Stats(in.Name)
				if err != nil {
					return "", err
				}
				return run.Run(context.Background(), argv)
			},
		},
		{
			Name:        "run_demo",
			Description: "Поднять контейнер ops-* из nginx:alpine или alpine:3.20 на порту 18080–18090.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"image":{"type":"string"},"port":{"type":"integer"}},"required":["name","image","port"]}`),
			Handle: func(args json.RawMessage) (string, error) {
				var in struct {
					Name  string `json:"name"`
					Image string `json:"image"`
					Port  int    `json:"port"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return "", err
				}
				argv, err := s1.DemoRun(in.Name, in.Image, in.Port)
				if err != nil {
					return "", err
				}
				return run.Run(context.Background(), argv)
			},
		},
		{
			Name:        "remove_demo",
			Description: "Удалить только контейнер с именем ops-*.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`),
			Handle: func(args json.RawMessage) (string, error) {
				var in struct {
					Name string `json:"name"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return "", err
				}
				argv, err := s1.DemoRemove(in.Name)
				if err != nil {
					return "", err
				}
				return run.Run(context.Background(), argv)
			},
		},
	}
	for _, tool := range fileTools {
		if tool.Name == "write_file" {
			out = append(out, tool)
		}
	}
	return out
}

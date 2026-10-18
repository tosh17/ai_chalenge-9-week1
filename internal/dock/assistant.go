package dock

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/files"
	"github.com/tosh17/deepseek-service/internal/mcpsrv"
	"github.com/tosh17/deepseek-service/internal/s1"
)

const systemPrompt = `Ты управляешь Docker на s1. Пользователь задаёт цель.
Сначала вызови list_services и list_images. Рекомендуй только то, что увидел, либо nginx:alpine / alpine:3.20.
Новый контейнер — только run_demo с именем ops-*. Не останавливай gitlab, ollama и runner.
Удалять можно только remove_demo. В конце по-русски три заголовка: ## Что запущено, ## Рекомендация, ## Действие.`

const maxRounds = 6

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

// Assistant подбирает образ и может поднять контейнер ops-.
type Assistant struct {
	Chat  Chatter
	Tools Tools
}

// Run выполняет цель.
func (a *Assistant) Run(ctx context.Context, goal string) (Result, error) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return Result{}, fmt.Errorf("нужна цель")
	}
	if a.Tools == nil {
		return Result{}, fmt.Errorf("инструменты не подключены")
	}
	var calls []Call
	reply := ""
	messages := []deepseek.Message{{Role: "system", Content: systemPrompt}, {Role: "user", Content: goal}}
	if a.Chat != nil {
		for round := 0; round < maxRounds; round++ {
			res, err := a.Chat.ChatTools(ctx, messages, a.Tools.Definitions())
			if err != nil {
				break
			}
			if len(res.ToolCalls) == 0 {
				reply = strings.TrimSpace(res.Reply)
				break
			}
			messages = append(messages, deepseek.Message{Role: "assistant", Content: res.Reply, ToolCalls: res.ToolCalls})
			for _, call := range res.ToolCalls {
				text, callErr := a.Tools.Call(ctx, call.Name, call.Arguments)
				item := Call{Name: call.Name, Arguments: call.Arguments, Result: text}
				if callErr != nil {
					item.Err = callErr.Error()
					text = callErr.Error()
				}
				calls = append(calls, item)
				messages = append(messages, deepseek.Message{Role: "tool", Name: call.Name, ToolCallID: call.ID, Content: clip(text, 4000)})
			}
		}
	}
	if !saw(calls, "list_services") {
		calls = append(calls, a.fallback(ctx, goal)...)
	}
	if !hasHeadings(reply) {
		reply = explain(goal, calls)
	}
	return Result{Reply: reply, Calls: calls}, nil
}

func (a *Assistant) fallback(ctx context.Context, goal string) []Call {
	var calls []Call
	for _, name := range []string{"list_services", "list_images"} {
		text, err := a.Tools.Call(ctx, name, "{}")
		calls = append(calls, Call{Name: name, Arguments: "{}", Result: text, Err: errString(err)})
	}
	lower := strings.ToLower(goal)
	if wantsRemove(lower) {
		name := demoName(goal)
		raw, _ := json.Marshal(map[string]string{"name": name})
		text, err := a.Tools.Call(ctx, "remove_demo", string(raw))
		calls = append(calls, Call{Name: "remove_demo", Arguments: string(raw), Result: text, Err: errString(err)})
		return calls
	}
	if !wantsStart(lower) {
		return calls
	}
	image := "nginx:alpine"
	if strings.Contains(lower, "alpine") && !strings.Contains(lower, "nginx") {
		image = "alpine:3.20"
	}
	port := 18080
	body := compose(image, port)
	raw, _ := json.Marshal(map[string]string{"path": "ops-nginx.yml", "content": body})
	text, err := a.Tools.Call(ctx, "write_file", string(raw))
	calls = append(calls, Call{Name: "write_file", Arguments: string(raw), Result: text, Err: errString(err)})
	runRaw, _ := json.Marshal(map[string]any{"name": "ops-nginx", "image": image, "port": port})
	text, err = a.Tools.Call(ctx, "run_demo", string(runRaw))
	calls = append(calls, Call{Name: "run_demo", Arguments: string(runRaw), Result: text, Err: errString(err)})
	return calls
}

func wantsStart(goal string) bool {
	if strings.Contains(goal, "не запускай") || strings.Contains(goal, "не поднимай") {
		return false
	}
	return strings.Contains(goal, "подними") || strings.Contains(goal, "запусти") || strings.Contains(goal, "разверни")
}

func wantsRemove(goal string) bool {
	return strings.Contains(goal, "убери") || strings.Contains(goal, "останови") || strings.Contains(goal, "удали")
}

func demoName(goal string) string {
	if name := regexp.MustCompile(`ops-[a-z0-9-]{1,24}`).FindString(goal); name != "" {
		return name
	}
	return "ops-nginx"
}

func compose(image string, port int) string {
	inside := 80
	if image == "alpine:3.20" {
		inside = 8080
	}
	return fmt.Sprintf("services:\n  ops-nginx:\n    image: %s\n    restart: \"no\"\n    ports:\n      - \"10.10.1.101:%d:%d\"\n", image, port, inside)
}

func explain(goal string, calls []Call) string {
	var b strings.Builder
	b.WriteString("## Что запущено\n")
	b.WriteString(textOf(calls, "list_services"))
	b.WriteString("\n\n## Рекомендация\n")
	if strings.Contains(textOf(calls, "list_images"), "nginx:alpine") {
		b.WriteString("Для простой страницы бери уже лежащий nginx:alpine: он маленький и не заменяет GitLab или Ollama.\n")
	} else {
		b.WriteString("Для простой страницы подойдёт nginx:alpine. GitLab, Ollama и runner не трогай. Если образа ещё нет локально, первый запуск его скачает.\n")
	}
	b.WriteString("\n## Действие\n")
	switch {
	case saw(calls, "remove_demo"):
		b.WriteString("Удалён учебный контейнер " + demoName(goal) + ".\n")
	case saw(calls, "run_demo"):
		b.WriteString("Записан ops-nginx.yml и поднят ops-nginx на http://10.10.1.101:18080.\n")
	default:
		b.WriteString("Ничего не запускал: в цели не было просьбы поднять контейнер.\n")
	}
	if errText := firstErr(calls); errText != "" {
		b.WriteString("\nОшибка: " + errText + "\n")
	}
	return b.String()
}

func hasHeadings(reply string) bool {
	return strings.Contains(reply, "## Что запущено") && strings.Contains(reply, "## Рекомендация")
}

func saw(calls []Call, name string) bool {
	for _, call := range calls {
		if call.Name == name && call.Err == "" {
			return true
		}
	}
	return false
}

func textOf(calls []Call, name string) string {
	for _, call := range calls {
		if call.Name == name && call.Err == "" && strings.TrimSpace(call.Result) != "" {
			return call.Result
		}
	}
	return "пусто"
}

func firstErr(calls []Call) string {
	for _, call := range calls {
		if call.Err != "" {
			return call.Name + ": " + call.Err
		}
	}
	return ""
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
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

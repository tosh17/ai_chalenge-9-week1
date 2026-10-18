package files

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/mcpsrv"
)

const systemPrompt = `Ты правишь файлы настроек s1. Пользователь задаёт цель, а не команду открыть файл.
Сам найди нужные файлы через search_text и read_file, затем запиши write_file.
Не выдумывай значения, которых не видел. В конце коротко по-русски перечисли изменённые файлы.`

const maxRounds = 6

// Chatter вызывает инструменты.
type Chatter interface {
	ChatTools(ctx context.Context, messages []deepseek.Message, tools []deepseek.Tool) (deepseek.ChatResult, error)
}

// Tools — поиск, чтение и запись.
type Tools interface {
	Definitions() []deepseek.Tool
	Call(ctx context.Context, name, arguments string) (string, error)
}

// Local вызывает файловые инструменты без отдельного процесса.
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

// Result — текст, вызовы и diff.
type Result struct {
	Reply string   `json:"reply"`
	Calls []Call   `json:"calls,omitempty"`
	Diff  string   `json:"diff,omitempty"`
	Files []string `json:"files,omitempty"`
}

// Assistant выполняет цель над каталогом настроек.
type Assistant struct {
	Chat  Chatter
	Tools Tools
}

// Run сам выбирает файлы.
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
				messages = append(messages, deepseek.Message{Role: "tool", Name: call.Name, ToolCallID: call.ID, Content: clipRunes(text, 4000)})
			}
		}
	}
	if !wrote(calls) {
		extra := a.fallback(ctx, goal)
		calls = append(calls, extra...)
		if reply == "" {
			reply = "Изменены файлы: " + strings.Join(written(calls), ", ")
		}
	}
	return Result{Reply: reply, Calls: calls, Diff: diffs(calls), Files: written(calls)}, nil
}

func (a *Assistant) fallback(ctx context.Context, goal string) []Call {
	var calls []Call
	query := "clone_url"
	if !strings.Contains(strings.ToLower(goal), "clone_url") {
		query = "GITLAB"
	}
	args := fmt.Sprintf(`{"query":%q}`, query)
	text, err := a.Tools.Call(ctx, "search_text", args)
	calls = append(calls, Call{Name: "search_text", Arguments: args, Result: text, Err: errString(err)})

	envArgs := `{"path":"services.env"}`
	envText, envErr := a.Tools.Call(ctx, "read_file", envArgs)
	calls = append(calls, Call{Name: "read_file", Arguments: envArgs, Result: envText, Err: errString(envErr)})
	runnerArgs := `{"path":"runner.toml"}`
	runnerText, runnerErr := a.Tools.Call(ctx, "read_file", runnerArgs)
	calls = append(calls, Call{Name: "read_file", Arguments: runnerArgs, Result: runnerText, Err: errString(runnerErr)})

	lower := strings.ToLower(goal)
	fix := strings.Contains(lower, "приведи") || strings.Contains(lower, "поправь") || strings.Contains(lower, "обнови")
	if fix && envErr == nil && runnerErr == nil {
		url := value(envText, "CLONE_URL")
		if url != "" {
			body := replaceClone(stripHeader(runnerText), url)
			raw, _ := json.Marshal(map[string]string{"path": "runner.toml", "content": body})
			text, err = a.Tools.Call(ctx, "write_file", string(raw))
			calls = append(calls, Call{Name: "write_file", Arguments: string(raw), Result: text, Err: errString(err)})
		}
	}
	if strings.Contains(lower, "compose") {
		compArgs := `{"path":"compose.yml"}`
		comp, compErr := a.Tools.Call(ctx, "read_file", compArgs)
		calls = append(calls, Call{Name: "read_file", Arguments: compArgs, Result: comp, Err: errString(compErr)})
		if compErr == nil {
			body := strings.TrimRight(stripHeader(comp), "\n") + "\n# clone_url сверен с services.env\n"
			raw, _ := json.Marshal(map[string]string{"path": "compose.yml", "content": body})
			text, err = a.Tools.Call(ctx, "write_file", string(raw))
			calls = append(calls, Call{Name: "write_file", Arguments: string(raw), Result: text, Err: errString(err)})
		}
	}
	if path := reportPath(goal); path != "" {
		body := "# Отчёт\n\nЦель: " + goal + "\n\n```\n" + textOf(calls, "search_text") + "\n```\n"
		raw, _ := json.Marshal(map[string]string{"path": path, "content": body})
		text, err = a.Tools.Call(ctx, "write_file", string(raw))
		calls = append(calls, Call{Name: "write_file", Arguments: string(raw), Result: text, Err: errString(err)})
	}
	return calls
}

func wrote(calls []Call) bool {
	for _, call := range calls {
		if call.Name == "write_file" && call.Err == "" {
			return true
		}
	}
	return false
}

func written(calls []Call) []string {
	var out []string
	for _, call := range calls {
		if call.Name != "write_file" || call.Err != "" {
			continue
		}
		var in struct {
			Path string `json:"path"`
		}
		if json.Unmarshal([]byte(call.Arguments), &in) == nil && in.Path != "" {
			out = append(out, in.Path)
		}
	}
	return out
}

func diffs(calls []Call) string {
	var b strings.Builder
	for _, call := range calls {
		if call.Name == "write_file" && strings.Contains(call.Result, "diff --git") {
			b.WriteString(call.Result)
			if !strings.HasSuffix(call.Result, "\n") {
				b.WriteByte('\n')
			}
		}
	}
	return b.String()
}

func value(text, key string) string {
	re := regexp.MustCompile(`(?m)^` + key + `=(.+)$`)
	m := re.FindStringSubmatch(text)
	if len(m) != 2 {
		return ""
	}
	return strings.Trim(strings.TrimSpace(m[1]), `"'`)
}

func replaceClone(body, url string) string {
	re := regexp.MustCompile(`(?m)^clone_url\s*=.*$`)
	line := `clone_url = "` + url + `"`
	if re.MatchString(body) {
		return re.ReplaceAllString(body, line)
	}
	return strings.TrimRight(body, "\n") + "\n" + line + "\n"
}

func stripHeader(text string) string {
	if strings.HasPrefix(text, "# ") {
		if _, rest, ok := strings.Cut(text, "\n"); ok {
			return rest
		}
	}
	return text
}

func reportPath(goal string) string {
	re := regexp.MustCompile(`[\w./-]+\.md`)
	if path := re.FindString(goal); path != "" && (strings.Contains(strings.ToLower(goal), "запиши") || strings.Contains(strings.ToLower(goal), "отчёт") || strings.Contains(strings.ToLower(goal), "отчет")) {
		return path
	}
	return ""
}

func textOf(calls []Call, name string) string {
	for _, call := range calls {
		if call.Name == name {
			return call.Result
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

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

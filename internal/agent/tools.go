package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/mcp"
)

const maxToolRounds = 6

const mcpToolHint = `ИНСТРУМЕНТЫ: текущая погода — get_weather. Ряд замеров сборщика — observation_report: без периода только город, период передавай from и to сам. Картинки и видео — цепочка: 1) search_media 2) summarize_media с inventory_path 3) chart_media только с summary_path. JSON сводки в chart_media не вставляй. Числа не выдумывай. Запись погодных замеров из чата не запускай.`

// ToolEvent — один вызов MCP за ход диалога.
type ToolEvent struct {
	Name      string          `json:"name"`
	Arguments string          `json:"arguments,omitempty"`
	Result    string          `json:"result"`
	IsError   bool            `json:"is_error,omitempty"`
	Request   json.RawMessage `json:"request,omitempty"`
	Response  json.RawMessage `json:"response,omitempty"`
}

// ToolExchange — ответ инструмента и сырой запрос/ответ MCP.
type ToolExchange struct {
	Text     string
	IsError  bool
	Request  json.RawMessage
	Response json.RawMessage
}

// TurnStep — один шаг хода: запрос к модели или вызов MCP.
type TurnStep struct {
	Kind       string          `json:"kind"`
	Title      string          `json:"title"`
	URL        string          `json:"url,omitempty"`
	Status     int             `json:"status,omitempty"`
	DurationMs int64           `json:"duration_ms,omitempty"`
	Request    json.RawMessage `json:"request,omitempty"`
	Response   json.RawMessage `json:"response,omitempty"`
	Error      string          `json:"error,omitempty"`
}

// ToolSource — внешние инструменты, которые модель может вызвать.
type ToolSource interface {
	Tools() []deepseek.Tool
	Call(ctx context.Context, name, arguments string) (ToolExchange, error)
}

// MCPTools адаптирует stdio-клиент MCP к агенту.
type MCPTools struct {
	Client *mcp.Client
}

func (m MCPTools) Tools() []deepseek.Tool {
	if m.Client == nil {
		return nil
	}
	src := m.Client.Tools()
	out := make([]deepseek.Tool, 0, len(src))
	for _, tool := range src {
		params := tool.InputSchema
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, deepseek.Tool{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  params,
		})
	}
	return out
}

func (m MCPTools) Call(ctx context.Context, name, arguments string) (ToolExchange, error) {
	if m.Client == nil {
		return ToolExchange{IsError: true}, fmt.Errorf("mcp client is nil")
	}
	raw := strings.TrimSpace(arguments)
	if raw == "" {
		raw = "{}"
	}
	if !json.Valid([]byte(raw)) {
		return ToolExchange{IsError: true}, fmt.Errorf("arguments are not json")
	}
	res, err := m.Client.Call(ctx, name, json.RawMessage(raw))
	ex := ToolExchange{
		Text:     res.Text,
		IsError:  res.IsError,
		Request:  res.Request,
		Response: res.Response,
	}
	if err != nil {
		return ex, err
	}
	return ex, nil
}

// ToolSet склеивает несколько источников. Call идёт в тот, где есть имя.
type ToolSet []ToolSource

func (s ToolSet) Tools() []deepseek.Tool {
	var out []deepseek.Tool
	for _, src := range s {
		if src == nil {
			continue
		}
		out = append(out, src.Tools()...)
	}
	return out
}

func (s ToolSet) Call(ctx context.Context, name, arguments string) (ToolExchange, error) {
	for _, src := range s {
		if src == nil {
			continue
		}
		for _, tool := range src.Tools() {
			if tool.Name == name {
				return src.Call(ctx, name, arguments)
			}
		}
	}
	return ToolExchange{IsError: true}, fmt.Errorf("unknown tool %q", name)
}

// WithTools подключает MCP. Клоны агента инструменты не наследуют.
func (a *Agent) WithTools(src ToolSource) *Agent {
	a.tools = src
	return a
}

// Tools возвращает подключённый источник (может быть nil).
func (a *Agent) Tools() ToolSource {
	return a.tools
}

type toolLLM interface {
	ChatTools(ctx context.Context, messages []deepseek.Message, tools []deepseek.Tool) (deepseek.ChatResult, error)
}

func (a *Agent) completeWithTools(ctx context.Context, llm LLM, messages []deepseek.Message) (deepseek.ChatResult, []ToolEvent, []TurnStep, error) {
	tools := a.toolDefs()
	caller, ok := llm.(toolLLM)
	if !ok || len(tools) == 0 {
		chat, err := llm.Chat(ctx, messages)
		return chat, nil, []TurnStep{llmStep(1, chat, err)}, err
	}

	prompt := append([]deepseek.Message(nil), messages...)
	prompt = append(prompt, deepseek.Message{Role: "system", Content: mcpToolHint})

	var events []ToolEvent
	var steps []TurnStep
	var last deepseek.ChatResult
	apiN := 0
	for round := 0; round < maxToolRounds; round++ {
		chat, err := caller.ChatTools(ctx, prompt, tools)
		apiN++
		steps = append(steps, llmStep(apiN, chat, err))
		if err != nil && len(events) == 0 && chat.Debug.StatusCode == 400 {
			plain, plainErr := llm.Chat(ctx, messages)
			apiN++
			steps = append(steps, llmStep(apiN, plain, plainErr))
			return plain, nil, steps, plainErr
		}
		if err != nil {
			return chat, events, steps, err
		}
		last = chat
		if len(chat.ToolCalls) == 0 {
			chat.Reply = attachChartURL(chat.Reply, events)
			return chat, events, steps, nil
		}
		prompt = append(prompt, deepseek.Message{
			Role:      "assistant",
			Content:   chat.Reply,
			ToolCalls: chat.ToolCalls,
		})
		for _, call := range chat.ToolCalls {
			ev := a.execTool(ctx, call)
			events = append(events, ev)
			steps = append(steps, mcpStep(ev))
			prompt = append(prompt, deepseek.Message{
				Role:       "tool",
				ToolCallID: call.ID,
				Name:       call.Name,
				Content:    ev.Result,
			})
		}
	}
	if strings.TrimSpace(last.Reply) != "" && len(last.ToolCalls) == 0 {
		return last, events, steps, nil
	}
	final, err := caller.ChatTools(ctx, prompt, nil)
	apiN++
	steps = append(steps, llmStep(apiN, final, err))
	final.Reply = attachChartURL(final.Reply, events)
	return final, events, steps, err
}

func attachChartURL(reply string, events []ToolEvent) string {
	for _, ev := range events {
		var payload struct {
			ImageURL string `json:"image_url"`
		}
		if json.Unmarshal([]byte(ev.Result), &payload) != nil || payload.ImageURL == "" {
			continue
		}
		if strings.Contains(reply, payload.ImageURL) {
			return reply
		}
		if strings.TrimSpace(reply) == "" {
			return payload.ImageURL
		}
		return strings.TrimRight(reply, "\n") + "\n" + payload.ImageURL
	}
	return reply
}

func llmStep(n int, chat deepseek.ChatResult, err error) TurnStep {
	step := TurnStep{
		Kind:       "llm",
		Title:      fmt.Sprintf("API · запрос %d", n),
		URL:        chat.Debug.URL,
		Status:     chat.Debug.StatusCode,
		DurationMs: chat.Debug.DurationMs,
		Request:    chat.Debug.Request,
		Response:   chat.Debug.Response,
	}
	if err != nil {
		step.Error = err.Error()
	}
	return step
}

func mcpStep(ev ToolEvent) TurnStep {
	step := TurnStep{
		Kind:     "mcp",
		Title:    "MCP · " + ev.Name,
		Request:  ev.Request,
		Response: ev.Response,
	}
	if ev.IsError && ev.Result != "" {
		step.Error = ev.Result
	}
	if len(step.Request) == 0 && strings.TrimSpace(ev.Arguments) != "" {
		if json.Valid([]byte(ev.Arguments)) {
			step.Request = json.RawMessage(ev.Arguments)
		}
	}
	if len(step.Response) == 0 {
		raw, err := json.Marshal(map[string]any{"text": ev.Result, "is_error": ev.IsError})
		if err == nil {
			step.Response = raw
		}
	}
	return step
}

func (a *Agent) toolDefs() []deepseek.Tool {
	if a.tools == nil {
		return nil
	}
	return a.tools.Tools()
}

func (a *Agent) execTool(ctx context.Context, call deepseek.ToolCall) ToolEvent {
	ev := ToolEvent{Name: call.Name, Arguments: call.Arguments}
	if a.tools == nil {
		ev.IsError = true
		ev.Result = "Инструменты не подключены."
		return ev
	}
	ex, err := a.tools.Call(ctx, call.Name, call.Arguments)
	ev.Request = ex.Request
	ev.Response = ex.Response
	if err != nil {
		ev.IsError = true
		if ex.Text != "" {
			ev.Result = ex.Text
		} else {
			ev.Result = err.Error()
		}
		return ev
	}
	ev.IsError = ex.IsError
	ev.Result = ex.Text
	return ev
}

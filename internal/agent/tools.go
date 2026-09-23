package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/mcp"
)

const maxToolRounds = 3

const mcpToolHint = `ИНСТРУМЕНТЫ MCP: если пользователь спрашивает текущую погоду или прогноз, вызови get_weather и ответь по его результату. Температуру и осадки не выдумывай. Для остальных вопросов инструмент не вызывай.`

// ToolEvent — один вызов MCP за ход диалога.
type ToolEvent struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"`
	Result    string `json:"result"`
	IsError   bool   `json:"is_error,omitempty"`
}

// ToolSource — внешние инструменты, которые модель может вызвать.
type ToolSource interface {
	Tools() []deepseek.Tool
	Call(ctx context.Context, name, arguments string) (text string, isError bool, err error)
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

func (m MCPTools) Call(ctx context.Context, name, arguments string) (string, bool, error) {
	if m.Client == nil {
		return "", true, fmt.Errorf("mcp client is nil")
	}
	raw := strings.TrimSpace(arguments)
	if raw == "" {
		raw = "{}"
	}
	if !json.Valid([]byte(raw)) {
		return "", true, fmt.Errorf("arguments are not json")
	}
	res, err := m.Client.Call(ctx, name, json.RawMessage(raw))
	if err != nil {
		return "", true, err
	}
	return res.Text, res.IsError, nil
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

func (a *Agent) completeWithTools(ctx context.Context, llm LLM, messages []deepseek.Message) (deepseek.ChatResult, []ToolEvent, error) {
	tools := a.toolDefs()
	caller, ok := llm.(toolLLM)
	if !ok || len(tools) == 0 {
		chat, err := llm.Chat(ctx, messages)
		return chat, nil, err
	}

	prompt := append([]deepseek.Message(nil), messages...)
	prompt = append(prompt, deepseek.Message{Role: "system", Content: mcpToolHint})

	var events []ToolEvent
	var last deepseek.ChatResult
	for round := 0; round < maxToolRounds; round++ {
		chat, err := caller.ChatTools(ctx, prompt, tools)
		if err != nil && len(events) == 0 && chat.Debug.StatusCode == 400 {
			plain, plainErr := llm.Chat(ctx, messages)
			return plain, nil, plainErr
		}
		if err != nil {
			return chat, events, err
		}
		last = chat
		if len(chat.ToolCalls) == 0 {
			return chat, events, nil
		}
		prompt = append(prompt, deepseek.Message{
			Role:      "assistant",
			Content:   chat.Reply,
			ToolCalls: chat.ToolCalls,
		})
		for _, call := range chat.ToolCalls {
			ev := a.execTool(ctx, call)
			events = append(events, ev)
			prompt = append(prompt, deepseek.Message{
				Role:       "tool",
				ToolCallID: call.ID,
				Name:       call.Name,
				Content:    ev.Result,
			})
		}
	}
	if strings.TrimSpace(last.Reply) != "" && len(last.ToolCalls) == 0 {
		return last, events, nil
	}
	final, err := caller.ChatTools(ctx, prompt, nil)
	return final, events, err
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
	text, isError, err := a.tools.Call(ctx, call.Name, call.Arguments)
	if err != nil {
		ev.IsError = true
		ev.Result = err.Error()
		return ev
	}
	ev.IsError = isError
	ev.Result = text
	return ev
}

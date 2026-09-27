package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/tosh17/deepseek-service/internal/deepseek"
)

type scriptLLM struct {
	calls int
}

func (s *scriptLLM) Chat(ctx context.Context, messages []deepseek.Message) (deepseek.ChatResult, error) {
	return s.ChatTools(ctx, messages, nil)
}

func (s *scriptLLM) ChatTools(ctx context.Context, messages []deepseek.Message, tools []deepseek.Tool) (deepseek.ChatResult, error) {
	s.calls++
	if s.calls == 1 {
		if len(tools) != 1 || tools[0].Name != "get_weather" {
			return deepseek.ChatResult{}, errString("tools were not offered")
		}
		return deepseek.ChatResult{ToolCalls: []deepseek.ToolCall{{
			ID:        "c1",
			Name:      "get_weather",
			Arguments: `{"city":"Москва","days":1}`,
		}}}, nil
	}
	for _, m := range messages {
		if m.Role == "tool" && strings.Contains(m.Content, "17") {
			return deepseek.ChatResult{Reply: "В Москве 17°C, пасмурно."}, nil
		}
	}
	return deepseek.ChatResult{}, errString("tool result missing")
}

type errString string

func (e errString) Error() string { return string(e) }

type stubTools struct {
	called string
}

func (s *stubTools) Tools() []deepseek.Tool {
	return []deepseek.Tool{{
		Name:        "get_weather",
		Description: "weather",
		Parameters:  []byte(`{"type":"object"}`),
	}}
}

func (s *stubTools) Call(ctx context.Context, name, arguments string) (ToolExchange, error) {
	s.called += name + " " + arguments + "\n"
	if name == "draw_flow" {
		return ToolExchange{Text: `{"image_url":"/media/flow.png"}`}, nil
	}
	return ToolExchange{
		Text:    "Москва 17°C",
		Request: []byte(`{"jsonrpc":"2.0","method":"tools/call"}`),
	}, nil
}

func TestCompleteWithTools(t *testing.T) {
	stub := &stubTools{}
	llm := &scriptLLM{}
	a := New("day16").WithTools(stub)
	chat, events, steps, err := a.completeWithTools(context.Background(), llm, []deepseek.Message{
		{Role: "user", Content: "какая погода в Москве?"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(chat.Reply, "В Москве 17°C, пасмурно.") || !strings.Contains(chat.Reply, "/media/flow.png") {
		t.Fatalf("reply: %q", chat.Reply)
	}
	if len(events) < 2 || events[0].Name != "get_weather" || events[0].IsError || events[len(events)-1].Name != "draw_flow" {
		t.Fatalf("events: %+v", events)
	}
	if !strings.Contains(stub.called, "Москва") {
		t.Fatalf("call args: %s", stub.called)
	}
	if llm.calls != 2 {
		t.Fatalf("model calls: %d", llm.calls)
	}
	if len(steps) != 4 || steps[0].Kind != "llm" || steps[1].Kind != "mcp" || steps[2].Kind != "llm" || steps[3].Kind != "mcp" {
		t.Fatalf("steps: %+v", steps)
	}
}

type chainLLM struct {
	calls int
}

func (s *chainLLM) Chat(ctx context.Context, messages []deepseek.Message) (deepseek.ChatResult, error) {
	return s.ChatTools(ctx, messages, nil)
}

func (s *chainLLM) ChatTools(ctx context.Context, messages []deepseek.Message, tools []deepseek.Tool) (deepseek.ChatResult, error) {
	s.calls++
	switch s.calls {
	case 1:
		return deepseek.ChatResult{ToolCalls: []deepseek.ToolCall{{ID: "1", Name: "search_media", Arguments: `{}`}}}, nil
	case 2:
		return deepseek.ChatResult{Reply: "сейчас посчитаю"}, nil
	case 3:
		return deepseek.ChatResult{ToolCalls: []deepseek.ToolCall{{ID: "2", Name: "summarize_media", Arguments: `{"inventory_path":"inv.json"}`}}}, nil
	case 4:
		return deepseek.ChatResult{ToolCalls: []deepseek.ToolCall{{ID: "3", Name: "chart_media", Arguments: `{"summary_path":"sum.json"}`}}}, nil
	default:
		return deepseek.ChatResult{Reply: "3 файла, 130 Б"}, nil
	}
}

type chainTools struct{}

func (chainTools) Tools() []deepseek.Tool {
	return []deepseek.Tool{{Name: "search_media"}, {Name: "summarize_media"}, {Name: "chart_media"}}
}

func (chainTools) Call(ctx context.Context, name, arguments string) (ToolExchange, error) {
	switch name {
	case "search_media":
		return ToolExchange{Text: `{"inventory_path":"inv.json","files":3}`}, nil
	case "summarize_media":
		return ToolExchange{Text: `{"summary_path":"sum.json","files":3}`}, nil
	case "chart_media":
		return ToolExchange{Text: `{"image_url":"/media/chart.png"}`}, nil
	case "draw_flow":
		return ToolExchange{Text: `{"image_url":"/media/flow.png"}`}, nil
	default:
		return ToolExchange{Text: "unknown", IsError: true}, nil
	}
}

func TestMediaChainContinuesToChart(t *testing.T) {
	llm := &chainLLM{}
	a := New("day19").WithTools(chainTools{})
	chat, events, _, err := a.completeWithTools(context.Background(), llm, []deepseek.Message{
		{Role: "user", Content: "посчитай файлы и покажи график"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 || events[2].Name != "chart_media" || events[3].Name != "draw_flow" {
		t.Fatalf("events: %+v", events)
	}
	if !strings.Contains(chat.Reply, "/media/chart.png") || !strings.Contains(chat.Reply, "3 файла") {
		t.Fatalf("reply: %q", chat.Reply)
	}
}

func TestAttachChartURL(t *testing.T) {
	got := attachChartURL("готово", []ToolEvent{{Result: `{"image_url":"/media/chart.png"}`}})
	if !strings.Contains(got, "/media/chart.png") {
		t.Fatalf("url missing: %q", got)
	}
	again := attachChartURL(got, []ToolEvent{{Result: `{"image_url":"/media/chart.png"}`}})
	if strings.Count(again, "/media/chart.png") != 1 {
		t.Fatalf("duplicated: %q", again)
	}
}

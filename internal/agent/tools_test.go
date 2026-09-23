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

func (s *stubTools) Call(ctx context.Context, name, arguments string) (string, bool, error) {
	s.called = name + " " + arguments
	return "Москва 17°C", false, nil
}

func TestCompleteWithTools(t *testing.T) {
	stub := &stubTools{}
	llm := &scriptLLM{}
	a := New("day16").WithTools(stub)
	chat, events, err := a.completeWithTools(context.Background(), llm, []deepseek.Message{
		{Role: "user", Content: "какая погода в Москве?"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if chat.Reply != "В Москве 17°C, пасмурно." {
		t.Fatalf("reply: %q", chat.Reply)
	}
	if len(events) != 1 || events[0].Name != "get_weather" || events[0].IsError {
		t.Fatalf("events: %+v", events)
	}
	if !strings.Contains(stub.called, "Москва") {
		t.Fatalf("call args: %s", stub.called)
	}
	if llm.calls != 2 {
		t.Fatalf("model calls: %d", llm.calls)
	}
}

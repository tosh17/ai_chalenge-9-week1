package deepseek

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMessageToolCallRoundtrip(t *testing.T) {
	msg := Message{
		Role: "assistant",
		ToolCalls: []ToolCall{{
			ID:        "c1",
			Name:      "get_weather",
			Arguments: `{"city":"Москва"}`,
		}},
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"arguments":{`) {
		t.Fatalf("arguments encoded as object: %s", raw)
	}
	var back Message
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.ToolCalls) != 1 || back.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("back: %+v", back)
	}
	if back.ToolCalls[0].Arguments != `{"city":"Москва"}` {
		t.Fatalf("args: %s", back.ToolCalls[0].Arguments)
	}
}

func TestPlainMessageKeepsContent(t *testing.T) {
	raw, err := json.Marshal(Message{Role: "user", Content: "привет"})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"role":"user","content":"привет"}` {
		t.Fatalf("plain: %s", raw)
	}
}

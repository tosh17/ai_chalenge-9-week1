package deepseek

import (
	"encoding/json"
	"testing"
)

func TestSamplingStaysOffTheWireUntilSet(t *testing.T) {
	c := NewClient("key", "qwen-local", "http://llm.local/v1/chat/completions")
	raw, err := c.marshalChat([]Message{{Role: "user", Content: "какао"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var plain map[string]any
	if err := json.Unmarshal(raw, &plain); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"temperature", "max_tokens", "reasoning_effort", "options"} {
		if _, ok := plain[key]; ok {
			t.Fatalf("поле %s ушло без настройки: %s", key, raw)
		}
	}

	temp := 0.15
	c.WithSampling(Sampling{Temperature: &temp, MaxTokens: 320, ReasoningEffort: "none", NumCtx: 8192})
	raw, err = c.marshalChat([]Message{{Role: "user", Content: "какао"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var tuned chatRequest
	if err := json.Unmarshal(raw, &tuned); err != nil {
		t.Fatal(err)
	}
	if tuned.Temperature == nil || *tuned.Temperature != 0.15 || tuned.MaxTokens != 320 || tuned.ReasoningEffort != "none" {
		t.Fatalf("%+v", tuned)
	}
	if tuned.Options == nil || tuned.Options.NumCtx != 8192 || tuned.Options.NumPredict != 320 {
		t.Fatalf("options %+v", tuned.Options)
	}
	bare := c.PlainCopy()
	raw, err = bare.marshalChat([]Message{{Role: "user", Content: "какао"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(raw, &plain) != nil {
		t.Fatal("plain copy")
	}
	if _, ok := plain["reasoning_effort"]; ok {
		t.Fatal("копия сохранила reasoning_effort")
	}
}

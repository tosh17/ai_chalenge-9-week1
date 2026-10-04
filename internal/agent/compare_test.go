package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/index"
)

type echoSideLLM struct{}

func (echoSideLLM) Chat(_ context.Context, messages []deepseek.Message) (deepseek.ChatResult, error) {
	var b strings.Builder
	for _, m := range messages {
		if m.Role == "system" {
			b.WriteString(m.Content)
		}
	}
	return deepseek.ChatResult{Reply: b.String()}, nil
}

func TestAnswerBothUsesDifferentChunks(t *testing.T) {
	a := New("cmp").
		WithSystemPrompt("только фрагменты").
		WithFixed(index.NewMemorySearcher([]index.Chunk{
			{ChunkID: "f1", Section: "окно", Text: "ФИКС давление 2.2", Embedding: []float32{1, 0}},
		})).
		WithManual(index.NewMemorySearcher([]index.Chunk{
			{ChunkID: "s1", Section: "глава", Text: "ГЛАВА давление 2.0", Embedding: []float32{1, 0}},
		}))
	cmp := a.answerBoth(context.Background(), echoSideLLM{}, "давление", []float32{1, 0})
	if !strings.Contains(cmp.Fixed.Reply, "ФИКС") || strings.Contains(cmp.Fixed.Reply, "ГЛАВА") {
		t.Fatalf("fixed: %s", cmp.Fixed.Reply)
	}
	if !strings.Contains(cmp.Struct.Reply, "ГЛАВА") || strings.Contains(cmp.Struct.Reply, "ФИКС") {
		t.Fatalf("struct: %s", cmp.Struct.Reply)
	}
	if len(cmp.Fixed.Hits) != 1 || cmp.Fixed.Hits[0].Preview == "" {
		t.Fatalf("%+v", cmp.Fixed.Hits)
	}
}

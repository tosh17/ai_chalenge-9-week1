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

func TestDay23BaselineHasTenRows(t *testing.T) {
	rows := controlRows(day23Baseline())
	if len(rows) != 10 {
		t.Fatalf("контролей %d", len(rows))
	}
	plainNoise, ragNoise := 0, 0
	for _, row := range rows {
		if !row.PlainHit || !row.RagHit || !row.SourceHit || row.Expect == "" || row.Sources == "" {
			t.Fatalf("%+v", row)
		}
		plainNoise += row.PlainNoise
		ragNoise += row.RagNoise
	}
	if plainNoise != 29 || ragNoise != 15 {
		t.Fatalf("лишних разделов %d и %d", plainNoise, ragNoise)
	}
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
	cmp := a.answerFiltered(context.Background(), echoSideLLM{}, "давление", "pressure", []float32{1, 0}, "pressure", []float32{1, 0})
	if !strings.Contains(cmp.Fixed.Reply, "ГЛАВА") || strings.Contains(cmp.Fixed.Reply, "ФИКС") {
		t.Fatalf("без фильтра: %s", cmp.Fixed.Reply)
	}
	if !strings.Contains(cmp.Struct.Reply, "ГЛАВА") || cmp.KBefore != 1 || cmp.KAfter != 1 {
		t.Fatalf("с фильтром: %s before=%d after=%d", cmp.Struct.Reply, cmp.KBefore, cmp.KAfter)
	}
}

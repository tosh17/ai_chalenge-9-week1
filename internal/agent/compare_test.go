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

func TestDay22BaselineScores(t *testing.T) {
	rows := controlRows(day22Baseline())
	if len(rows) != 10 {
		t.Fatalf("контролей %d", len(rows))
	}
	plain := 0
	for _, row := range rows {
		if !row.RagHit || !row.SourceHit || row.Expect == "" || row.Sources == "" {
			t.Fatalf("%+v", row)
		}
		if row.PlainHit {
			plain++
		}
	}
	if plain != 1 {
		t.Fatalf("без RAG зачёт только у голландского соуса, получено %d", plain)
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
	cmp := a.answerBoth(context.Background(), echoSideLLM{}, "давление", []float32{1, 0})
	if strings.Contains(cmp.Fixed.Reply, "ГЛАВА") || len(cmp.Fixed.Hits) != 0 {
		t.Fatalf("без RAG не должен видеть книгу: %s hits=%d", cmp.Fixed.Reply, len(cmp.Fixed.Hits))
	}
	if !strings.Contains(cmp.Struct.Reply, "ГЛАВА") || strings.Contains(cmp.Struct.Reply, "ФИКС") {
		t.Fatalf("rag: %s", cmp.Struct.Reply)
	}
	if len(cmp.Struct.Hits) != 1 || cmp.Struct.Hits[0].Section != "глава" {
		t.Fatalf("%+v", cmp.Struct.Hits)
	}
}

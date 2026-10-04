package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

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

func TestDay24BaselineHasSourcesAndQuotes(t *testing.T) {
	rows := controlRows(day24Baseline())
	if len(rows) != 10 {
		t.Fatalf("контролей %d", len(rows))
	}
	miss := 0
	for _, row := range rows {
		if !row.PlainHit || !row.RagHit || row.Expect == "" || row.Sources == "" {
			t.Fatalf("%+v", row)
		}
		if !row.SourceHit {
			miss++
		}
	}
	if miss != 1 {
		t.Fatalf("смысл не совпал у %d вопросов", miss)
	}
}

type failLLM struct{}

func (failLLM) Chat(context.Context, []deepseek.Message) (deepseek.ChatResult, error) {
	return deepseek.ChatResult{}, fmt.Errorf("модель не должна вызываться")
}

func TestAnswerCitedRefusesWeakContext(t *testing.T) {
	a := New("cite").WithManual(index.NewMemorySearcher([]index.Chunk{
		{ChunkID: "s1", Section: "Boiled Coffee", Text: "1 cup coffee", Embedding: []float32{1, 0}},
	}))
	cmp := a.answerCited(context.Background(), failLLM{}, "масло в двигателе", "oil", "engine oil", []float32{0, 1}, []float32{0, 1})
	if !cmp.Unknown || !strings.HasPrefix(cmp.Fixed.Reply, "Не знаю") || len(cmp.Citations) != 0 {
		t.Fatalf("%+v", cmp)
	}
}

func TestAnswerCitedAttachesSourceAndQuote(t *testing.T) {
	a := New("cite").WithManual(index.NewMemorySearcher([]index.Chunk{
		{ChunkID: "s1", Section: "Boiled Coffee", Text: "Boil 3 minutes.", Embedding: []float32{1, 0}},
	}))
	cmp := a.answerCited(context.Background(), echoSideLLM{}, "кофе", "coffee", "boiled coffee", []float32{1, 0}, []float32{1, 0})
	if cmp.Unknown || len(cmp.Citations) != 1 {
		t.Fatalf("%+v", cmp)
	}
	c := cmp.Citations[0]
	if c.Source != index.BookFile || c.ChunkID != "s1" || !strings.Contains(c.Quote, "3 minutes") {
		t.Fatalf("%+v", c)
	}
	if !strings.Contains(cmp.Struct.Reply, "s1") || !strings.Contains(cmp.Fixed.Reply, "3 minutes") {
		t.Fatalf("reply %q quotes %q", cmp.Fixed.Reply, cmp.Struct.Reply)
	}
}

func TestAnswerCitedFallsBackToSearchPhrase(t *testing.T) {
	a := New("cite").WithManual(index.NewMemorySearcher([]index.Chunk{
		{ChunkID: "tea", Section: "How to Make Tea", Text: "3 teaspoons tea, 2 cups boiling water, five minutes.", Embedding: []float32{1, 0}},
	}))
	cmp := a.answerCited(context.Background(), echoSideLLM{}, "как заварить чай", "how to brew tea", "Brewed Tea", []float32{1, 0}, []float32{0, 1})
	if cmp.Unknown || !cmp.Fallback || cmp.KAfter != 1 || len(cmp.Citations) != 1 || cmp.Citations[0].Section != "How to Make Tea" {
		t.Fatalf("%+v", cmp)
	}
}

func TestBrewedTeaUsesSearchPhrase(t *testing.T) {
	if _, err := os.Stat("../../data/cookbook/struct.json"); err != nil {
		t.Skip("нет индекса")
	}
	manual, err := index.OpenSearcher("../../data/cookbook/struct.json", "http://10.10.1.101:11434", "nomic-embed-text")
	if err != nil {
		t.Fatal(err)
	}
	a := New("cite").WithManual(manual)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	raw, err := manual.QueryVector(ctx, "how to brew tea")
	if err != nil {
		t.Fatal(err)
	}
	re, err := manual.QueryVector(ctx, "Brewed Tea")
	if err != nil {
		t.Fatal(err)
	}
	cmp := a.answerCited(ctx, echoSideLLM{}, "как заварить чай", "how to brew tea", "Brewed Tea", raw, re)
	if cmp.Unknown || !cmp.Fallback || cmp.KAfter != 1 || len(cmp.Citations) != 1 || cmp.Citations[0].Section != "How to Make Tea" {
		t.Fatalf("unknown=%v fallback=%v after=%d cites=%v", cmp.Unknown, cmp.Fallback, cmp.KAfter, cmp.Citations)
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

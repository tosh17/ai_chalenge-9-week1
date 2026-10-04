package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/index"
	"github.com/tosh17/deepseek-service/internal/memory"
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

func TestAnswerCitedUsesOutsideSourceWhenBookMisses(t *testing.T) {
	a := New("cite").WithManual(index.NewMemorySearcher([]index.Chunk{
		{ChunkID: "s1", Section: "Boiled Coffee", Text: "1 cup coffee", Embedding: []float32{1, 0}},
	}))
	cmp := a.answerCited(context.Background(), echoSideLLM{}, "масло в двигателе", "oil", "engine oil", []float32{0, 1}, []float32{0, 1}, TaskView{}, "")
	if cmp.Unknown || len(cmp.Citations) != 1 || cmp.Citations[0].Source != "не из книги" || !strings.Contains(cmp.Fixed.Reply, "не из книги") {
		t.Fatalf("%+v", cmp)
	}
}

func TestAnswerCitedRecallsTaskWithoutBook(t *testing.T) {
	layers, err := memory.OpenLayers(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New("cite").WithLayers(layers).WithManual(index.NewMemorySearcher([]index.Chunk{
		{ChunkID: "s1", Section: "Boiled Coffee", Text: "1 cup coffee", Embedding: []float32{1, 0}},
	}))
	cmp := a.answerCited(context.Background(), echoSideLLM{}, "напомни цель", "goal", "remind the goal", []float32{0, 1}, []float32{0, 1}, TaskView{Goal: "завтрак: какао", Constraints: []string{"без яиц"}}, "")
	if cmp.Unknown || cmp.Citations[0].Source != "не из книги" || !strings.Contains(cmp.Fixed.Reply, "какао") || !strings.Contains(cmp.Fixed.Reply, "без яиц") {
		t.Fatalf("%+v", cmp.Fixed.Reply)
	}
}

func TestAnswerCitedAttachesSourceAndQuote(t *testing.T) {
	a := New("cite").WithManual(index.NewMemorySearcher([]index.Chunk{
		{ChunkID: "s1", Section: "Boiled Coffee", Text: "Boil 3 minutes.", Embedding: []float32{1, 0}},
	}))
	cmp := a.answerCited(context.Background(), echoSideLLM{}, "кофе", "coffee", "boiled coffee", []float32{1, 0}, []float32{1, 0}, TaskView{Goal: "кофе"}, "user: как сварить")
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

func TestFormatCompareReplyIsDialogue(t *testing.T) {
	got := formatCompareReply(ChunkCompare{
		Fixed:     SideAnswer{Reply: "Ответ\nК какао подойдёт кекс."},
		Citations: []index.Citation{{Section: "Cocoanut Cream Candy", ChunkID: "struct:1", Quote: "1½ cups sugar"}},
		Struct:    SideAnswer{Reply: "huge quote"},
	})
	if strings.Contains(got, "1½ cups") || strings.Contains(got, "Источники и цитаты") || !strings.Contains(got, "К какао подойдёт кекс.") || !strings.Contains(got, "Cocoanut Cream Candy · struct:1") {
		t.Fatal(got)
	}
}

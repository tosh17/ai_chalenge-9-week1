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

func TestDay22ControlAnswers(t *testing.T) {
	if os.Getenv("COOKBOOK_LLM") == "" {
		t.Skip("COOKBOOK_LLM=1")
	}
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Fatal("нет DEEPSEEK_API_KEY")
	}
	manual, err := index.OpenSearcher("../../data/cookbook/struct.json", "http://10.10.1.101:11434", "nomic-embed-text")
	if err != nil {
		t.Fatal(err)
	}
	fixed, err := index.OpenSearcher("../../data/cookbook/fixed.json", "http://10.10.1.101:11434", "nomic-embed-text")
	if err != nil {
		t.Fatal(err)
	}
	a := New("day22-eval").
		WithSystemPrompt("Ты кулинарный помощник. Отвечай по фрагментам поваренной книги из системного сообщения. Граммы, время и шаги бери только оттуда. Если нужного рецепта во фрагментах нет, так и скажи. Отвечай по-русски и коротко.").
		WithManual(manual).
		WithFixed(fixed)
	llm := deepseek.NewClient(key, "deepseek-v4-flash", "https://api.deepseek.com/chat/completions")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	for _, c := range index.Controls() {
		vec, err := manual.QueryVector(ctx, c.Search)
		if err != nil {
			t.Fatal(err)
		}
		cmp := a.answerCited(ctx, llm, c.Question, c.Search, c.Search, vec, vec, TaskView{}, "")
		quotes := quoteBodies(cmp.Citations)
		fmt.Printf("Q %s\n  unknown=%v sources=%v quotes=%v grounded=%v facts=%v section=%v cites=%d\n  answer: %s\n",
			c.Question,
			cmp.Unknown,
			len(cmp.Citations) > 0 && cmp.Citations[0].ChunkID != "" && cmp.Citations[0].Source != "",
			strings.TrimSpace(quotes) != "",
			index.Grounded(cmp.Fixed.Reply, quotes),
			index.FactsHit(cmp.Fixed.Reply, c.Facts),
			index.SectionHit(cmp.Fixed.Hits, c.Section),
			len(cmp.Citations),
			oneLine(cmp.Fixed.Reply),
		)
	}
	oil, err := manual.QueryVector(ctx, "how to change engine oil")
	if err != nil {
		t.Fatal(err)
	}
	off := a.answerCited(ctx, llm, "как заменить масло в двигателе", "engine oil", "how to change engine oil", oil, oil, TaskView{}, "")
	fmt.Printf("OFF unknown=%v k_after=%d reply=%s\n", off.Unknown, off.KAfter, oneLine(off.Fixed.Reply))
}

func quoteBodies(cites []index.Citation) string {
	var b strings.Builder
	for _, c := range cites {
		b.WriteString(c.Quote)
		b.WriteString("\n")
	}
	return b.String()
}

func oneLine(s string) string {
	if len(s) > 280 {
		return s[:280]
	}
	return s
}

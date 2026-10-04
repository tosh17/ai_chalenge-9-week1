package agent

import (
	"context"
	"fmt"
	"os"
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
		cmp := a.answerBoth(ctx, llm, c.Question, vec)
		fmt.Printf("Q %s\n  source=%v plain=%v rag=%v\n  plain: %s\n  rag: %s\n",
			c.Question,
			index.SectionHit(cmp.Struct.Hits, c.Section),
			index.FactsHit(cmp.Fixed.Reply, c.Facts),
			index.FactsHit(cmp.Struct.Reply, c.Facts),
			oneLine(cmp.Fixed.Reply),
			oneLine(cmp.Struct.Reply),
		)
	}
}

func oneLine(s string) string {
	if len(s) > 280 {
		return s[:280]
	}
	return s
}

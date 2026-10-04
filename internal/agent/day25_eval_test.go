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
	"github.com/tosh17/deepseek-service/internal/memory"
)

func TestDay25LongScenarios(t *testing.T) {
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
	llm := deepseek.NewClient(key, "deepseek-v4-flash", "https://api.deepseek.com/chat/completions")
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	runScenario(t, ctx, manual, llm, "завтрак", []string{"какао", "яиц"}, []scenarioTurn{
		{text: "Цель диалога: завтрак с какао по книге."},
		{text: "Уточняю: без яиц."},
		{text: "Сколько молока в какао к завтраку?", cite: true},
		{text: "Сколько сахара в этом какао?", cite: true},
		{text: "Сколько ложек какао-порошка?", cite: true},
		{text: "Напомни цель диалога."},
		{text: "Ограничение: порция на двоих, молоко не заменяй водой."},
		{text: "Сколько воды в какао к завтраку?", cite: true},
		{text: "Как заварить чай, если какао не хочется? Цель завтрака не меняй.", cite: true},
		{text: "Снова про какао: сколько молока?", cite: true},
		{text: "Что уже уточнено?"},
		{text: "Повтори цель и ограничения."},
	})
	runScenario(t, ctx, manual, llm, "соус", []string{"соус", "кайен"}, []scenarioTurn{
		{text: "Цель: голландский соус к рыбе."},
		{text: "Ограничение: без кайенского перца. Если он есть в рецепте, предупреди."},
		{text: "Какие продукты в голландском соусе?", cite: true},
		{text: "Сколько масла в голландском соусе?", cite: true},
		{text: "Сколько желтков в голландском соусе?", cite: true},
		{text: "Напомни, к какому блюду соус."},
		{text: "Как испечь хлеб на воде? Цель соуса не меняй.", cite: true},
		{text: "Вернись к соусу: есть ли в нём лимон?", cite: true},
		{text: "Ограничение: уксус не добавляй, только лимон из книги."},
		{text: "Повтори цель и ограничения."},
		{text: "Кратко шаги голландского соуса.", cite: true},
		{text: "Сколько лимонного сока в голландском соусе?", cite: true},
	})
}

type scenarioTurn struct {
	text string
	cite bool
}

func runScenario(t *testing.T, ctx context.Context, manual *index.Searcher, llm LLM, name string, hold []string, turns []scenarioTurn) {
	t.Helper()
	layers, err := memory.OpenLayers(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New("day25-" + name).
		WithSystemPrompt("Ты кулинарный помощник. Отвечай по фрагментам книги.").
		WithLayers(layers).
		WithMemoryPolicy(MemoryPolicy{STMWindowN: 30}).
		WithManual(manual).
		WithFixed(manual)
	cited := 0
	for i, turn := range turns {
		task := a.rememberTask(ctx, llm, turn.text)
		searchQuery := a.searchQuery(ctx, llm, turn.text)
		rewritten := a.rewriteQuery(ctx, llm, turn.text, searchQuery)
		vec, err := manual.QueryVector(ctx, rewritten)
		if err != nil {
			t.Fatal(err)
		}
		cmp := a.answerCited(ctx, llm, turn.text, searchQuery, rewritten, vec, task, a.dialogContext())
		reply := formatCompareReply(cmp)
		if _, err := layers.AppendShortTerm(30,
			deepseek.Message{Role: "user", Content: turn.text},
			deepseek.Message{Role: "assistant", Content: reply},
		); err != nil {
			t.Fatal(err)
		}
		fmt.Printf("%s #%d cites=%d unknown=%v goal=%q\n  %s\n", name, i+1, len(cmp.Citations), cmp.Unknown, task.Goal, oneLine(cmp.Fixed.Reply))
		if turn.cite {
			if cmp.Unknown || len(cmp.Citations) == 0 {
				t.Errorf("%s ход %d без источника: %s", name, i+1, oneLine(cmp.Fixed.Reply))
			} else {
				cited++
			}
		}
	}
	blob := strings.ToLower(a.taskView().Goal + " " + strings.Join(a.taskView().Clarified, " ") + " " + strings.Join(a.taskView().Constraints, " "))
	for _, needle := range hold {
		if !strings.Contains(blob, needle) {
			t.Errorf("%s память потеряла %q: %s", name, needle, blob)
		}
	}
	if cited < 6 {
		t.Errorf("%s ответов с источниками %d", name, cited)
	}
}

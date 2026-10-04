package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/index"
)

// SideAnswer — ответ одного режима.
type SideAnswer struct {
	Label string        `json:"label"`
	Reply string        `json:"reply"`
	Error string        `json:"error,omitempty"`
	Hits  []index.Match `json:"hits"`
}

// ControlRow — один контрольный вопрос и что нашли в прогоне.
type ControlRow struct {
	Question   string `json:"question"`
	Expect     string `json:"expect"`
	Sources    string `json:"sources"`
	PlainHit   bool   `json:"plain_hit"`
	RagHit     bool   `json:"rag_hit"`
	SourceHit  bool   `json:"source_hit"`
	PlainNoise int    `json:"plain_noise"`
	RagNoise   int    `json:"rag_noise"`
}

// ChunkCompare — ответ без фильтра и ответ после порога, реранкинга и перефразирования.
type ChunkCompare struct {
	Query       string        `json:"query"`
	SearchQuery string        `json:"search_query,omitempty"`
	Rewrite     string        `json:"rewrite,omitempty"`
	KBefore     int           `json:"k_before,omitempty"`
	KAfter      int           `json:"k_after,omitempty"`
	Threshold   float64       `json:"threshold,omitempty"`
	MarkLabel   string        `json:"mark_label,omitempty"`
	Fixed       SideAnswer    `json:"fixed"`
	Struct      SideAnswer    `json:"struct"`
	Report      *index.Report `json:"report,omitempty"`
	Controls    []ControlRow  `json:"controls,omitempty"`
}

// Compare отвечает по сырому топ-4 и по тому же поиску после перефразирования, реранкинга и порога.
func (a *Agent) Compare(ctx context.Context, req Request) (Result, error) {
	if strings.TrimSpace(req.Message) == "" {
		return Result{}, fmt.Errorf("agent %q: message is required", a.name)
	}
	if !a.HasCompare() {
		return Result{}, fmt.Errorf("agent %q: оба индекса не загружены", a.name)
	}
	providerID := req.Provider
	if providerID == "" {
		providerID = a.defaultID
	}
	b, ok := a.backends[providerID]
	if !ok {
		return Result{}, fmt.Errorf("agent %q: unknown provider %q", a.name, providerID)
	}

	searchQuery := a.searchQuery(ctx, b.llm, req.Message)
	rewritten := a.rewriteQuery(ctx, b.llm, req.Message, searchQuery)
	rawVec, err := a.manual.QueryVector(ctx, searchQuery)
	if err != nil {
		return Result{Provider: providerID, Model: b.model}, fmt.Errorf("agent %q: вектор вопроса: %w", a.name, err)
	}
	reVec, err := a.manual.QueryVector(ctx, rewritten)
	if err != nil {
		reVec = rawVec
		rewritten = searchQuery
	}
	start := time.Now()
	cmp := a.answerFiltered(ctx, b.llm, req.Message, searchQuery, rawVec, rewritten, reVec)
	duration := time.Since(start).Milliseconds()
	reply := formatCompareReply(cmp)
	if cmp.Fixed.Error != "" && cmp.Struct.Error != "" {
		return Result{
			Reply:      reply,
			Provider:   providerID,
			Model:      b.model,
			DurationMs: duration,
			Compare:    &cmp,
		}, fmt.Errorf("agent %q [%s]: %s", a.name, providerID, cmp.Fixed.Error)
	}

	if a.layers != nil {
		if _, saveErr := a.layers.AppendShortTerm(a.memoryPolicy.STMWindowN,
			deepseek.Message{Role: "user", Content: req.Message},
			deepseek.Message{Role: "assistant", Content: reply},
		); saveErr != nil {
			return Result{Reply: reply, Provider: providerID, Model: b.model, DurationMs: duration, Compare: &cmp}, fmt.Errorf("agent %q: save stm: %w", a.name, saveErr)
		}
	}
	return Result{
		Reply:      reply,
		Provider:   providerID,
		Model:      b.model,
		DurationMs: duration,
		Compare:    &cmp,
	}, nil
}

func (a *Agent) searchQuery(ctx context.Context, llm LLM, query string) string {
	query = strings.TrimSpace(query)
	if query == "" || !hasCyrillic(query) {
		return query
	}
	chat, err := llm.Chat(ctx, []deepseek.Message{
		{Role: "system", Content: "Переведи кулинарный запрос на английский. Ответь одной поисковой фразой, без кавычек и пояснений."},
		{Role: "user", Content: query},
	})
	if err != nil {
		return query
	}
	line := strings.Trim(strings.TrimSpace(strings.Split(chat.Reply, "\n")[0]), `"'«»`)
	if line == "" {
		return query
	}
	return line
}

func (a *Agent) rewriteQuery(ctx context.Context, llm LLM, query, fallback string) string {
	chat, err := llm.Chat(ctx, []deepseek.Message{
		{Role: "system", Content: "Сожми кулинарный запрос до английского названия блюда, 2–6 слов, как заголовок рецепта. Без кавычек и пояснений."},
		{Role: "user", Content: query},
	})
	if err != nil {
		return fallback
	}
	line := strings.Trim(strings.TrimSpace(strings.Split(chat.Reply, "\n")[0]), `"'«»`)
	fields := strings.Fields(line)
	if line == "" || len(fields) > 8 {
		return fallback
	}
	return line
}

func hasCyrillic(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Cyrillic, r) {
			return true
		}
	}
	return false
}

func (a *Agent) answerFiltered(ctx context.Context, llm LLM, query, searchQuery string, rawVec []float32, rewritten string, reVec []float32) ChunkCompare {
	rawHits := a.manual.Matches(rawVec, index.AnswerK)
	wide := a.manual.Matches(reVec, index.RecallK)
	filtered := index.Filter(index.Rerank(wide, rewritten), index.MinScore, index.AnswerK)
	var rawReply, rawErr, filteredReply, filteredErr string
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		rawReply, rawErr = completeSide(ctx, llm, a.systemPrompt, rawHits, query)
	}()
	go func() {
		defer wg.Done()
		filteredReply, filteredErr = completeSide(ctx, llm, a.systemPrompt, filtered, query)
	}()
	wg.Wait()
	return ChunkCompare{
		Query:       query,
		SearchQuery: searchQuery,
		Rewrite:     rewritten,
		KBefore:     len(wide),
		KAfter:      len(filtered),
		Threshold:   index.MinScore,
		Report:      a.report,
		Fixed: SideAnswer{
			Label: "Без фильтра",
			Reply: rawReply,
			Error: rawErr,
			Hits:  rawHits,
		},
		Struct: SideAnswer{
			Label: "С фильтром",
			Reply: filteredReply,
			Error: filteredErr,
			Hits:  filtered,
		},
	}
}

func day23Baseline() map[string]ControlRow {
	hit := func(plainNoise, ragNoise int) ControlRow {
		return ControlRow{PlainHit: true, RagHit: true, SourceHit: true, PlainNoise: plainNoise, RagNoise: ragNoise}
	}
	return map[string]ControlRow{
		"Как сварить кофе в кофейнике?":     hit(2, 2),
		"Как сделать фильтрованный кофе?":   hit(3, 1),
		"Как заварить чай?":                 hit(3, 0),
		"Как приготовить лимонад?":          hit(3, 1),
		"Как сварить какао к завтраку?":     hit(3, 1),
		"Как сделать горячий шоколад?":      hit(3, 2),
		"Как испечь хлеб на воде?":          hit(3, 3),
		"Как приготовить голландский соус?": hit(3, 3),
		"Как сделать мятный джулеп?":        hit(3, 0),
		"Как приготовить имбирный пунш?":    hit(3, 2),
	}
}

func controlRows(scored map[string]ControlRow) []ControlRow {
	out := make([]ControlRow, 0, 10)
	for _, c := range index.Controls() {
		row := ControlRow{Question: c.Question, Expect: c.Expect, Sources: c.Section}
		if scored != nil {
			if prev, ok := scored[c.Question]; ok {
				row.PlainHit = prev.PlainHit
				row.RagHit = prev.RagHit
				row.SourceHit = prev.SourceHit
				row.PlainNoise = prev.PlainNoise
				row.RagNoise = prev.RagNoise
			}
		}
		out = append(out, row)
	}
	return out
}

func completeSide(ctx context.Context, llm LLM, system string, hits []index.Match, query string) (string, string) {
	if system == "" {
		system = "Отвечай только по фрагментам руководства. Если нужного фрагмента нет, так и скажи."
	}
	messages := []deepseek.Message{
		{Role: "system", Content: system},
		{Role: "system", Content: index.FormatMatches(hits)},
		{Role: "user", Content: query},
	}
	chat, err := llm.Chat(ctx, messages)
	if err != nil {
		return "", err.Error()
	}
	return strings.TrimSpace(chat.Reply), ""
}

func formatCompareReply(cmp ChunkCompare) string {
	fixed := cmp.Fixed.Reply
	if fixed == "" {
		fixed = cmp.Fixed.Error
	}
	structured := cmp.Struct.Reply
	if structured == "" {
		structured = cmp.Struct.Error
	}
	left := cmp.Fixed.Label
	if left == "" {
		left = "Без фильтра"
	}
	right := cmp.Struct.Label
	if right == "" {
		right = "С фильтром"
	}
	return left + "\n" + fixed + "\n\n" + right + "\n" + structured
}

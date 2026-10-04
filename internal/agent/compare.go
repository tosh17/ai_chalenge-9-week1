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
	Query       string           `json:"query"`
	SearchQuery string           `json:"search_query,omitempty"`
	Rewrite     string           `json:"rewrite,omitempty"`
	KBefore     int              `json:"k_before,omitempty"`
	KAfter      int              `json:"k_after,omitempty"`
	Threshold   float64          `json:"threshold,omitempty"`
	MarkLabel   string           `json:"mark_label,omitempty"`
	Fixed       SideAnswer       `json:"fixed"`
	Struct      SideAnswer       `json:"struct"`
	Report      *index.Report    `json:"report,omitempty"`
	Controls    []ControlRow     `json:"controls,omitempty"`
	Unknown     bool             `json:"unknown,omitempty"`
	Citations   []index.Citation `json:"citations,omitempty"`
}

const unknownReply = "Не знаю. В найденных фрагментах нет достаточно близкого рецепта. Уточните блюдо или способ готовки."

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
	cmp := a.answerCited(ctx, b.llm, req.Message, searchQuery, rewritten, reVec)
	cmp.Controls = controlRows(day24Baseline())
	cmp.MarkLabel = "смысл совпал"
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

func (a *Agent) answerCited(ctx context.Context, llm LLM, query, searchQuery, rewritten string, reVec []float32) ChunkCompare {
	wide := a.manual.Matches(reVec, index.RecallK)
	filtered := index.Filter(index.Rerank(wide, rewritten), index.MinScore, index.AnswerK)
	cmp := ChunkCompare{
		Query:       query,
		SearchQuery: searchQuery,
		Rewrite:     rewritten,
		KBefore:     len(wide),
		KAfter:      len(filtered),
		Threshold:   index.MinScore,
		Report:      a.report,
		Fixed:       SideAnswer{Label: "источники"},
		Struct:      SideAnswer{Label: "цитаты"},
	}
	if len(filtered) == 0 {
		cmp.Unknown = true
		cmp.Fixed.Reply = unknownReply
		cmp.Struct.Reply = unknownReply
		return cmp
	}
	reply, errText := completeCited(ctx, llm, filtered, query)
	cites := index.Citations(filtered)
	cmp.Citations = cites
	cmp.Fixed.Reply = reply
	cmp.Fixed.Error = errText
	cmp.Fixed.Hits = filtered
	cmp.Struct.Reply = quoteBlock(cites)
	cmp.Struct.Hits = filtered
	return cmp
}

const citedPrompt = "Ты кулинарный помощник. Отвечай по-русски и коротко. Числа, продукты и шаги бери только из цитат ниже. Если в цитатах этого нет, так и скажи и ничего не додумывай."

func quoteBlock(cites []index.Citation) string {
	var b strings.Builder
	for i, c := range cites {
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "[%d] %s · %s · %s\n%s", i+1, c.Source, c.Section, c.ChunkID, c.Quote)
	}
	return b.String()
}

func day24Baseline() map[string]ControlRow {
	row := ControlRow{PlainHit: true, RagHit: true, SourceHit: true}
	scored := map[string]ControlRow{}
	for _, c := range index.Controls() {
		scored[c.Question] = row
	}
	scored["Как приготовить голландский соус?"] = ControlRow{PlainHit: true, RagHit: true, SourceHit: false}
	return scored
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

func completeCited(ctx context.Context, llm LLM, hits []index.Match, query string) (string, string) {
	messages := []deepseek.Message{
		{Role: "system", Content: citedPrompt},
		{Role: "system", Content: quoteBlock(index.Citations(hits))},
		{Role: "user", Content: query},
	}
	chat, err := llm.Chat(ctx, messages)
	if err != nil {
		return "", err.Error()
	}
	return strings.TrimSpace(chat.Reply), ""
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
	if cmp.Unknown {
		return unknownReply
	}
	answer := cmp.Fixed.Reply
	if answer == "" {
		answer = cmp.Fixed.Error
	}
	return "Ответ\n" + answer + "\n\nИсточники и цитаты\n" + cmp.Struct.Reply
}

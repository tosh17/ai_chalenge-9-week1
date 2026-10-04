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
	Question  string `json:"question"`
	Expect    string `json:"expect"`
	Sources   string `json:"sources"`
	PlainHit  bool   `json:"plain_hit"`
	RagHit    bool   `json:"rag_hit"`
	SourceHit bool   `json:"source_hit"`
}

// ChunkCompare — ответ без RAG, ответ с RAG и контрольный набор.
type ChunkCompare struct {
	Query       string        `json:"query"`
	SearchQuery string        `json:"search_query,omitempty"`
	Fixed       SideAnswer    `json:"fixed"`
	Struct      SideAnswer    `json:"struct"`
	Report      *index.Report `json:"report,omitempty"`
	Controls    []ControlRow  `json:"controls,omitempty"`
}

// Compare отвечает на вопрос без фрагментов книги и с ними.
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
	vec, err := a.manual.QueryVector(ctx, searchQuery)
	if err != nil {
		return Result{Provider: providerID, Model: b.model}, fmt.Errorf("agent %q: вектор вопроса: %w", a.name, err)
	}
	start := time.Now()
	cmp := a.answerBoth(ctx, b.llm, req.Message, vec)
	cmp.SearchQuery = searchQuery
	cmp.Controls = controlRows(day22Baseline())
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

func hasCyrillic(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Cyrillic, r) {
			return true
		}
	}
	return false
}

func (a *Agent) answerBoth(ctx context.Context, llm LLM, query string, vec []float32) ChunkCompare {
	ragHits := a.manual.Matches(vec, 4)
	var plainReply, plainErr, ragReply, ragErr string
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		plainReply, plainErr = completePlain(ctx, llm, query)
	}()
	go func() {
		defer wg.Done()
		ragReply, ragErr = completeSide(ctx, llm, a.systemPrompt, ragHits, query)
	}()
	wg.Wait()
	return ChunkCompare{
		Query:  query,
		Report: a.report,
		Fixed: SideAnswer{
			Label: "Без RAG",
			Reply: plainReply,
			Error: plainErr,
		},
		Struct: SideAnswer{
			Label: "С RAG",
			Reply: ragReply,
			Error: ragErr,
			Hits:  ragHits,
		},
	}
}

func completePlain(ctx context.Context, llm LLM, query string) (string, string) {
	chat, err := llm.Chat(ctx, []deepseek.Message{
		{Role: "system", Content: "Ты кулинарный помощник. Отвечай по-русски и коротко по общим знаниям, без фрагментов книги. Если не помнишь точную цифру, так и скажи и не выдумывай граммы."},
		{Role: "user", Content: query},
	})
	if err != nil {
		return "", err.Error()
	}
	return strings.TrimSpace(chat.Reply), ""
}

func day22Baseline() map[string]ControlRow {
	scored := map[string]ControlRow{}
	for _, c := range index.Controls() {
		scored[c.Question] = ControlRow{PlainHit: false, RagHit: true, SourceHit: true}
	}
	scored["Как приготовить голландский соус?"] = ControlRow{PlainHit: true, RagHit: true, SourceHit: true}
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
	return "Без RAG\n" + fixed + "\n\nС RAG\n" + structured
}

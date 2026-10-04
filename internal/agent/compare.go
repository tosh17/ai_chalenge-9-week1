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

// SideAnswer — ответ одной стратегии нарезки.
type SideAnswer struct {
	Label string        `json:"label"`
	Reply string        `json:"reply"`
	Error string        `json:"error,omitempty"`
	Hits  []index.Match `json:"hits"`
}

// ChunkCompare — два ответа, найденные фрагменты и сводка стратегий.
type ChunkCompare struct {
	Query       string        `json:"query"`
	SearchQuery string        `json:"search_query,omitempty"`
	Fixed       SideAnswer    `json:"fixed"`
	Struct      SideAnswer    `json:"struct"`
	Report      *index.Report `json:"report,omitempty"`
}

// Compare отвечает на вопрос дважды: по фиксированному окну и по главам.
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
	fixedHits := a.fixed.Matches(vec, 4)
	structHits := a.manual.Matches(vec, 4)
	var fixedReply, fixedErr, structReply, structErr string
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		fixedReply, fixedErr = completeSide(ctx, llm, a.systemPrompt, fixedHits, query)
	}()
	go func() {
		defer wg.Done()
		structReply, structErr = completeSide(ctx, llm, a.systemPrompt, structHits, query)
	}()
	wg.Wait()
	cmp := ChunkCompare{
		Query:  query,
		Report: a.report,
		Fixed: SideAnswer{
			Label: "Фиксированное окно",
			Reply: fixedReply,
			Error: fixedErr,
			Hits:  fixedHits,
		},
		Struct: SideAnswer{
			Label: "По главам и рецептам",
			Reply: structReply,
			Error: structErr,
			Hits:  structHits,
		},
	}
	return cmp
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
	return "Фиксированное окно\n" + fixed + "\n\nПо главам\n" + structured
}

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
	Fallback    bool             `json:"fallback,omitempty"`
	MarkLabel   string           `json:"mark_label,omitempty"`
	Fixed       SideAnswer       `json:"fixed"`
	Struct      SideAnswer       `json:"struct"`
	Report      *index.Report    `json:"report,omitempty"`
	Controls    []ControlRow     `json:"controls,omitempty"`
	Unknown     bool             `json:"unknown,omitempty"`
	Citations   []index.Citation `json:"citations,omitempty"`
	Task        *TaskView        `json:"task,omitempty"`
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

	task := a.rememberTask(ctx, b.llm, req.Message)
	searchQuery := a.searchQuery(ctx, b.llm, req.Message, task.Goal)
	rewritten := a.rewriteQuery(ctx, b.llm, req.Message, searchQuery, task.Goal)
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
	cmp := a.answerCited(ctx, b.llm, req.Message, searchQuery, rewritten, rawVec, reVec, task, a.dialogContext())
	cmp.Task = &task
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
		if _, saveErr := a.layers.AppendShortTerm(0,
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

func (a *Agent) searchQuery(ctx context.Context, llm LLM, query, goal string) string {
	query = strings.TrimSpace(query)
	if query == "" || !hasCyrillic(query) {
		return query
	}
	chat, err := llm.Chat(ctx, []deepseek.Message{
		{Role: "system", Content: "Переведи текущую реплику на английский одной поисковой фразой из нескольких слов, без кавычек. Не отвечай одним словом. Если это короткое уточнение без нового блюда и цель задана, включи цель. Если пользователь называет другое блюдо или другую тему, переводи только её и не добавляй старую цель."},
		{Role: "user", Content: "Цель: " + emptyDash(goal) + "\nРеплика: " + query},
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

func (a *Agent) rewriteQuery(ctx context.Context, llm LLM, query, fallback, goal string) string {
	chat, err := llm.Chat(ctx, []deepseek.Message{
		{Role: "system", Content: "Сожми текущую реплику до английского названия блюда, 2–6 слов, как заголовок рецепта. Без кавычек. Если пользователь просит другое блюдо, назови его, а не прежнюю цель. Если это уточнение без нового блюда, можно опереться на цель."},
		{Role: "user", Content: "Цель: " + emptyDash(goal) + "\nРеплика: " + query},
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

func (a *Agent) answerCited(ctx context.Context, llm LLM, query, searchQuery, rewritten string, rawVec, reVec []float32, task TaskView, history string) ChunkCompare {
	wide := a.manual.Matches(reVec, index.RecallK)
	filtered := index.Filter(index.Rerank(wide, rewritten), index.MinScore, index.AnswerK)
	fellBack := false
	if len(filtered) == 0 && searchQuery != rewritten && len(rawVec) > 0 {
		rawWide := a.manual.Matches(rawVec, index.RecallK)
		rawKept := index.Filter(index.Rerank(rawWide, searchQuery), index.MinScore, index.AnswerK)
		if len(rawKept) > 0 {
			wide = rawWide
			filtered = rawKept
			fellBack = true
		}
	}
	cmp := ChunkCompare{
		Query:       query,
		SearchQuery: searchQuery,
		Rewrite:     rewritten,
		KBefore:     len(wide),
		KAfter:      len(filtered),
		Threshold:   index.MinScore,
		Fallback:    fellBack,
		Report:      a.report,
		Fixed:       SideAnswer{Label: "источники"},
		Struct:      SideAnswer{Label: "цитаты"},
	}
	if len(filtered) == 0 {
		reply, errText := completeOutside(ctx, llm, query, taskPrompt(task, history))
		cite := outsideCitation()
		if isOffTopicReply(reply) {
			cite = offTopicCitation()
		}
		cmp.Citations = []index.Citation{cite}
		cmp.Fixed.Reply = stripReplyChrome(reply)
		cmp.Fixed.Error = errText
		cmp.Struct.Reply = quoteBlock(cmp.Citations)
		return cmp
	}
	reply, errText := completeCited(ctx, llm, filtered, query, taskPrompt(task, history))
	cites := index.Citations(filtered)
	cmp.Citations = cites
	cmp.Fixed.Reply = stripReplyChrome(reply)
	cmp.Fixed.Error = errText
	cmp.Fixed.Hits = filtered
	cmp.Struct.Reply = quoteBlock(cites)
	cmp.Struct.Hits = filtered
	return cmp
}

const citedPrompt = "Ты кулинарный помощник. Отвечай по-русски, коротко, как следующая реплика диалога. Не пиши заголовки «Ответ» и «Источники». Если пользователь меняет цель, прими новую и не удерживай старую. Числа из книги бери только из цитат ниже. Если цитаты не отвечают на вопрос, но вопрос про еду, ответь из общих знаний и прямо напиши, что это не из книги. Если вопрос не про еду и не меняет цель, скажи, что это не по теме разговора."

const outsidePrompt = "В книге достаточно близкого фрагмента нет. Ответь по-русски, коротко. Если вопрос про еду и продолжает разговор, ответь из общих знаний и прямо напиши, что это не из книги. Если вопрос не про еду и не меняет цель, ответь только: это не по теме разговора. Если пользователь меняет цель, прими новую. Не придумывай цитату из поваренной книги."

func outsideCitation() index.Citation {
	return index.Citation{
		Source:  "не из книги",
		Section: "общий ответ",
		ChunkID: "вне книги",
		Quote:   "В книге фрагмент с оценкой 0.75 и выше не найден. Ответ дан не по поваренной книге.",
	}
}

func isOffTopicReply(reply string) bool {
	reply = strings.ToLower(strings.TrimSpace(reply))
	return strings.HasPrefix(reply, "это не по теме") || strings.HasPrefix(reply, "не по теме")
}

func offTopicCitation() index.Citation {
	return index.Citation{
		Source:  "не по теме",
		Section: "вне диалога",
		ChunkID: "не по теме",
		Quote:   "Вопрос не про еду и не меняет цель разговора.",
	}
}

func completeOutside(ctx context.Context, llm LLM, query, contextNote string) (string, string) {
	messages := []deepseek.Message{{Role: "system", Content: outsidePrompt}}
	if strings.TrimSpace(contextNote) != "" {
		messages = append(messages, deepseek.Message{Role: "system", Content: contextNote})
	}
	messages = append(messages, deepseek.Message{Role: "user", Content: query})
	chat, err := llm.Chat(ctx, messages)
	if err != nil {
		return "", err.Error()
	}
	return strings.TrimSpace(chat.Reply), ""
}

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

func completeCited(ctx context.Context, llm LLM, hits []index.Match, query, contextNote string) (string, string) {
	messages := []deepseek.Message{
		{Role: "system", Content: citedPrompt},
		{Role: "system", Content: quoteBlock(index.Citations(hits))},
	}
	if strings.TrimSpace(contextNote) != "" {
		messages = append(messages, deepseek.Message{Role: "system", Content: contextNote})
	}
	messages = append(messages, deepseek.Message{Role: "user", Content: query})
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
	answer := stripReplyChrome(cmp.Fixed.Reply)
	if answer == "" {
		answer = strings.TrimSpace(cmp.Fixed.Error)
	}
	line := sourceLine(cmp.Citations)
	if line == "" {
		return answer
	}
	return answer + "\n\nИсточники: " + line
}

func stripReplyChrome(s string) string {
	s = strings.TrimSpace(s)
	for strings.HasPrefix(s, "Ответ\n") || strings.HasPrefix(s, "Ответ ") {
		s = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(s, "Ответ\n"), "Ответ "))
	}
	if i := strings.Index(s, "\nИсточники и цитаты"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

func sourceLine(cites []index.Citation) string {
	if len(cites) == 0 {
		return ""
	}
	parts := make([]string, 0, len(cites))
	for _, c := range cites {
		parts = append(parts, c.Section+" · "+c.ChunkID)
	}
	return strings.Join(parts, "; ")
}

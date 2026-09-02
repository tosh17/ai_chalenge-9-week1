package deepseek

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// StrategyID — идентификатор техники промптинга (день 3).
type StrategyID string

const (
	StrategyDirect     StrategyID = "direct"
	StrategyStepByStep StrategyID = "step_by_step"
	StrategyMetaPrompt StrategyID = "meta_prompt"
	StrategyExperts    StrategyID = "experts"
)

// StrategyMeta описывает карточку сравнения в UI.
type StrategyMeta struct {
	ID          StrategyID `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
}

// StrategyResult — ответ одной техники.
type StrategyResult struct {
	ID              StrategyID `json:"id"`
	Title           string     `json:"title"`
	Description     string     `json:"description"`
	Reply           string     `json:"reply,omitempty"`
	GeneratedPrompt string     `json:"generated_prompt,omitempty"` // только meta_prompt
	Error           string     `json:"error,omitempty"`
	DurationMs      int64      `json:"duration_ms"`
}

func Strategies() []StrategyMeta {
	return []StrategyMeta{
		{
			ID:          StrategyDirect,
			Title:       "Прямой ответ",
			Description: "Без дополнительных инструкций — только текст задачи",
		},
		{
			ID:          StrategyStepByStep,
			Title:       "Пошагово",
			Description: "В промпт добавлена инструкция «решай пошагово»",
		},
		{
			ID:          StrategyMetaPrompt,
			Title:       "Мета-промпт",
			Description: "Сначала модель пишет промпт, затем решает задачу по нему",
		},
		{
			ID:          StrategyExperts,
			Title:       "Группа экспертов",
			Description: "Аналитик, инженер и критик — решение от каждого",
		},
	}
}

// CompareOptions — параметры пакетного сравнения стратегий.
type CompareOptions struct {
	MaxTokens int
}

// CompareResult — колбэк по мере готовности каждой стратегии.
type CompareProgress func(result StrategyResult)

// Compare запускает все стратегии. Прямой/пошаговый/эксперты — параллельно;
// мета-промпт — два последовательных запроса внутри себя.
func (c *Client) Compare(ctx context.Context, question string, opts CompareOptions, onDone CompareProgress) []StrategyResult {
	metas := Strategies()
	results := make([]StrategyResult, len(metas))
	var wg sync.WaitGroup

	for i, meta := range metas {
		wg.Add(1)
		go func(i int, meta StrategyMeta) {
			defer wg.Done()
			res := c.runStrategy(ctx, question, meta, opts)
			results[i] = res
			if onDone != nil {
				onDone(res)
			}
		}(i, meta)
	}

	wg.Wait()
	return results
}

func (c *Client) runStrategy(ctx context.Context, question string, meta StrategyMeta, opts CompareOptions) StrategyResult {
	out := StrategyResult{
		ID:          meta.ID,
		Title:       meta.Title,
		Description: meta.Description,
	}

	var (
		reply string
		err   error
		gen   string
		ms    int64
	)

	switch meta.ID {
	case StrategyDirect:
		reply, ms, err = c.askOnce(ctx, question, opts.MaxTokens)
	case StrategyStepByStep:
		reply, ms, err = c.askOnce(ctx, question+"\n\nрешай пошагово", opts.MaxTokens)
	case StrategyMetaPrompt:
		reply, gen, ms, err = c.askMetaPrompt(ctx, question, opts.MaxTokens)
		out.GeneratedPrompt = gen
	case StrategyExperts:
		reply, ms, err = c.askOnce(ctx, expertsPrompt(question), opts.MaxTokens)
	default:
		err = fmt.Errorf("unknown strategy: %s", meta.ID)
	}

	out.DurationMs = ms
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.Reply = reply
	return out
}

func (c *Client) askOnce(ctx context.Context, userContent string, maxTokens int) (string, int64, error) {
	res, err := c.ChatRaw(ctx, []Message{{Role: "user", Content: userContent}}, maxTokens)
	return res.Reply, res.Debug.DurationMs, err
}

func (c *Client) askMetaPrompt(ctx context.Context, question string, maxTokens int) (reply, generated string, ms int64, err error) {
	compose := fmt.Sprintf(
		`Ты — инженер промптов. По задаче ниже составь один готовый промпт для другой ИИ-модели.
Правила:
- верни ТОЛЬКО текст промпта;
- без предисловий вроде «Вот промпт»;
- промпт должен содержать условие задачи и инструкции, как её решить.

Задача:
%s`, question)

	first, err := c.ChatRaw(ctx, []Message{{Role: "user", Content: compose}}, maxTokens)
	ms += first.Debug.DurationMs
	if err != nil {
		return "", "", ms, err
	}
	generated = strings.TrimSpace(first.Reply)
	if generated == "" {
		return "", "", ms, fmt.Errorf("empty generated prompt")
	}

	solve := fmt.Sprintf(
		`Ниже дан промпт. Выполни его и дай решение задачи.

--- ПРОМПТ ---
%s
--- КОНЕЦ ПРОМПТА ---

Реши задачу сейчас.`, generated)

	second, err := c.ChatRaw(ctx, []Message{{Role: "user", Content: solve}}, maxTokens)
	ms += second.Debug.DurationMs
	if err != nil {
		return "", generated, ms, err
	}
	reply = strings.TrimSpace(second.Reply)
	if reply == "" {
		return "", generated, ms, fmt.Errorf("empty solution for generated prompt")
	}
	return reply, generated, ms, nil
}

func expertsPrompt(question string) string {
	return fmt.Sprintf(`Создай группу экспертов и получи решение задачи от каждого:
1) Аналитик — разбирает условие, допущения и ключевые факты.
2) Инженер — предлагает практическое решение и шаги реализации.
3) Критик — ищет слабые места, риски и альтернативы.

Формат ответа строго такой:
### Аналитик
...
### Инженер
...
### Критик
...
### Итоговое решение
Краткий согласованный вывод на основе трёх мнений.

Задача:
%s`, question)
}

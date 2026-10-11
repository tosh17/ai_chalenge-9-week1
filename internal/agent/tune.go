package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/index"
)

const (
	tuneTempBefore = 0.4
	tuneCtxBefore  = 32768
	tuneTempAfter  = 0.15
	tuneMaxAfter   = 320
	tuneCtxAfter   = 8192

	// baselineCookPrompt — шаблон дня 25, до настройки локальной модели.
	baselineCookPrompt = "Ты кулинарный помощник. Отвечай по-русски, коротко, как следующая реплика диалога. Не пиши заголовки «Ответ» и «Источники». Если пользователь меняет цель, прими новую и не удерживай старую. Числа из книги бери только из цитат ниже. Если цитаты не отвечают на вопрос, но вопрос про еду, ответь из общих знаний и прямо напиши, что это не из книги. Если вопрос не про еду и не меняет цель, скажи, что это не по теме разговора."
)

// CookbookSampling — параметры локальной Qwen под ответы по книге.
// Температура ниже, ответ короткий, внутренние рассуждения выключены, окно 8192.
func CookbookSampling() deepseek.Sampling {
	temperature := tuneTempAfter
	return deepseek.Sampling{
		Temperature:     &temperature,
		MaxTokens:       tuneMaxAfter,
		ReasoningEffort: "none",
		NumCtx:          tuneCtxAfter,
	}
}

// TuneSide — один ответ и чем он обошёлся.
type TuneSide struct {
	Reply            string `json:"reply"`
	Quality          int    `json:"quality"`
	FactsHit         int    `json:"facts_hit"`
	FactsTotal       int    `json:"facts_total"`
	Grounded         bool   `json:"grounded"`
	DurationMs       int64  `json:"duration_ms"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	Error            string `json:"error,omitempty"`
}

// TuneRow — один контрольный вопрос до и после настройки.
type TuneRow struct {
	Question string   `json:"question"`
	Expect   string   `json:"expect"`
	Section  string   `json:"section"`
	Before   TuneSide `json:"before"`
	After    TuneSide `json:"after"`
}

// TuneProfile — какие ручки стояли в прогоне.
type TuneProfile struct {
	Label         string  `json:"label"`
	Temperature   float64 `json:"temperature"`
	MaxTokens     int     `json:"max_tokens"`
	ContextTokens int     `json:"context_tokens"`
	Thinking      bool    `json:"thinking"`
	Prompt        string  `json:"prompt"`
}

// TuneStats — средние по четырём вопросам.
type TuneStats struct {
	Quality          int   `json:"quality"`
	DurationMs       int64 `json:"duration_ms"`
	PromptTokens     int   `json:"prompt_tokens"`
	CompletionTokens int   `json:"completion_tokens"`
}

// TuneReport — сравнение локальной модели до и после настройки.
type TuneReport struct {
	Model      []deepseek.ModelFile `json:"models"`
	QuantNote  string               `json:"quant_note"`
	Before     TuneProfile          `json:"before"`
	After      TuneProfile          `json:"after"`
	Rows       []TuneRow            `json:"rows"`
	BeforeAvg  TuneStats            `json:"before_avg"`
	AfterAvg   TuneStats            `json:"after_avg"`
	DurationMs int64                `json:"duration_ms"`
}

// RunLocalTune гоняет четыре рецепта на локальной модели: сначала дефолт хоста, потом настройку дня 29.
func (a *Agent) RunLocalTune(ctx context.Context) (TuneReport, error) {
	if a == nil {
		return TuneReport{}, fmt.Errorf("агент не создан")
	}
	live, plain, err := a.localPair()
	if err != nil {
		return TuneReport{}, err
	}
	started := time.Now()
	models, modelErr := live.ListModels(ctx)
	report := TuneReport{
		Model: models,
		Before: TuneProfile{
			Label:         "До",
			Temperature:   tuneTempBefore,
			MaxTokens:     0,
			ContextTokens: tuneCtxBefore,
			Thinking:      true,
			Prompt:        baselineCookPrompt,
		},
		After: TuneProfile{
			Label:         "После",
			Temperature:   tuneTempAfter,
			MaxTokens:     tuneMaxAfter,
			ContextTokens: tuneCtxAfter,
			Thinking:      false,
			Prompt:        a.answerPrompt(),
		},
		QuantNote: quantNote(models, live.Model(), modelErr),
	}
	for _, control := range tuneControls() {
		row, err := a.tuneOne(ctx, plain, live, control)
		if err != nil {
			return TuneReport{}, err
		}
		report.Rows = append(report.Rows, row)
	}
	report.BeforeAvg = tuneAverage(report.Rows, true)
	report.AfterAvg = tuneAverage(report.Rows, false)
	report.DurationMs = time.Since(started).Milliseconds()
	return report, nil
}

func (a *Agent) localPair() (live, plain *deepseek.Client, err error) {
	b, ok := a.backends[ProviderLocal]
	if !ok {
		return nil, nil, fmt.Errorf("локальная модель не подключена")
	}
	client, ok := b.llm.(*deepseek.Client)
	if !ok {
		return nil, nil, fmt.Errorf("локальная модель не OpenAI-совместимый клиент")
	}
	return client, client.PlainCopy(), nil
}

func tuneControls() []index.Control {
	want := map[string]bool{
		"Boiled Coffee":       true,
		"How to Make Tea":     true,
		"Breakfast Cocoa":     true,
		"Hollandaise Sauce I": true,
	}
	out := make([]index.Control, 0, len(want))
	for _, control := range index.Controls() {
		if want[control.Section] {
			out = append(out, control)
		}
	}
	return out
}

func (a *Agent) tuneOne(ctx context.Context, before, after LLM, control index.Control) (TuneRow, error) {
	row := TuneRow{Question: control.Question, Expect: control.Expect, Section: control.Section}
	row.Before = askTuned(ctx, before, baselineCookPrompt, "", control.Question, control.Facts)
	row.After = askTuned(ctx, after, a.answerPrompt(), "", control.Question, control.Facts)
	return row, nil
}

func askTuned(ctx context.Context, llm LLM, system, quotes, question string, facts [][]string) TuneSide {
	start := time.Now()
	messages := []deepseek.Message{{Role: "system", Content: system}}
	if strings.TrimSpace(quotes) != "" {
		messages = append(messages, deepseek.Message{Role: "system", Content: quotes})
	}
	messages = append(messages, deepseek.Message{Role: "user", Content: question})
	chat, err := llm.Chat(ctx, messages)
	side := TuneSide{DurationMs: time.Since(start).Milliseconds(), FactsTotal: len(facts)}
	if err != nil {
		side.Error = err.Error()
		return side
	}
	side.Reply = strings.TrimSpace(chat.Reply)
	if chat.Usage != nil {
		side.PromptTokens = chat.Usage.PromptTokens
		side.CompletionTokens = chat.Usage.CompletionTokens
	}
	side.Quality, side.FactsHit, side.Grounded = scoreTune(side.Reply, quotes, facts)
	return side
}

func scoreTune(reply, quotes string, facts [][]string) (quality, hit int, grounded bool) {
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return 0, 0, false
	}
	low := strings.ToLower(reply)
	for _, group := range facts {
		for _, alt := range group {
			if alt != "" && strings.Contains(low, strings.ToLower(alt)) {
				hit++
				break
			}
		}
	}
	if strings.TrimSpace(quotes) == "" {
		if n := len(facts); n > 0 {
			return hit * 100 / n, hit, false
		}
		return 0, hit, false
	}
	grounded = index.Grounded(reply, quotes)
	factPart := 0
	if n := len(facts); n > 0 {
		factPart = hit * 70 / n
	}
	groundPart := 0
	if grounded {
		groundPart = 30
	}
	return factPart + groundPart, hit, grounded
}

func tuneAverage(rows []TuneRow, before bool) TuneStats {
	if len(rows) == 0 {
		return TuneStats{}
	}
	var stats TuneStats
	for _, row := range rows {
		side := row.After
		if before {
			side = row.Before
		}
		stats.Quality += side.Quality
		stats.DurationMs += side.DurationMs
		stats.PromptTokens += side.PromptTokens
		stats.CompletionTokens += side.CompletionTokens
	}
	n := len(rows)
	stats.Quality /= n
	stats.DurationMs /= int64(n)
	return stats
}

func quantNote(models []deepseek.ModelFile, active string, err error) string {
	if err != nil {
		return "Список файлов на хосте не прочитался. В чате остаётся уже загруженная модель, без смены квантования."
	}
	if len(models) == 0 {
		return "Хост не отдал ни одного файла модели."
	}
	var b strings.Builder
	quants := map[string]struct{}{}
	for _, m := range models {
		quants[m.Quant] = struct{}{}
		mark := ""
		if modelMatches(active, m.Name) {
			mark = " — эта модель в чате"
		}
		fmt.Fprintf(&b, "%s: %s, %s, %s%s. ", m.Name, emptyQuant(m.Params), emptyQuant(m.Quant), formatGB(m.Bytes), mark)
	}
	if len(quants) == 1 {
		b.WriteString("Q8 и FP16 на хосте нет, новые файлы не скачивал. Для кулинарных ответов оставлен Q4_K_M: он уже в памяти.")
	}
	return strings.TrimSpace(b.String())
}

func modelMatches(id, name string) bool {
	id = strings.TrimSpace(id)
	name = strings.TrimSpace(name)
	return id != "" && (name == id || strings.HasPrefix(name, id+":"))
}

func emptyQuant(s string) string {
	if strings.TrimSpace(s) == "" {
		return "не указано"
	}
	return s
}

// LocalSettings — ручки из задания дня 29.
type LocalSettings struct {
	Temperature   float64  `json:"temperature"`
	MaxTokens     int      `json:"max_tokens"`
	ContextTokens int      `json:"context_tokens"`
	Quant         string   `json:"quant"`
	Quants        []string `json:"quants"`
	Prompt        string   `json:"prompt"`
}

func (a *Agent) savedPrompt() string {
	if a == nil {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.localPrompt
}

func (a *Agent) answerPrompt() string {
	if a == nil {
		return citedPrompt
	}
	a.mu.Lock()
	prompt := a.localPrompt
	a.mu.Unlock()
	if strings.TrimSpace(prompt) == "" {
		return citedPrompt
	}
	return prompt
}

// ReadLocalSettings отдаёт текущие параметры локальной модели.
func (a *Agent) ReadLocalSettings(ctx context.Context) (LocalSettings, error) {
	client, _, err := a.localPair()
	if err != nil {
		return LocalSettings{}, err
	}
	sampling := client.SamplingCopy()
	temp := tuneTempAfter
	if sampling.Temperature != nil {
		temp = *sampling.Temperature
	}
	maxTokens := sampling.MaxTokens
	if maxTokens <= 0 {
		maxTokens = tuneMaxAfter
	}
	ctxTokens := sampling.NumCtx
	if ctxTokens <= 0 {
		ctxTokens = tuneCtxAfter
	}
	models, _ := client.ListModels(ctx)
	quants := uniqueQuants(models)
	if len(quants) == 0 {
		quants = []string{"Q4_K_M"}
	}
	a.mu.Lock()
	quant := a.localQuant
	a.mu.Unlock()
	if quant == "" || !containsString(quants, quant) {
		quant = quants[0]
	}
	return LocalSettings{
		Temperature:   temp,
		MaxTokens:     maxTokens,
		ContextTokens: ctxTokens,
		Quant:         quant,
		Quants:        quants,
		Prompt:        a.answerPrompt(),
	}, nil
}

// ApplyLocalSettings записывает параметры локальной модели.
func (a *Agent) ApplyLocalSettings(in LocalSettings) (LocalSettings, error) {
	client, _, err := a.localPair()
	if err != nil {
		return LocalSettings{}, err
	}
	quants := in.Quants
	if len(quants) == 0 {
		if current, readErr := a.ReadLocalSettings(context.Background()); readErr == nil {
			quants = current.Quants
		}
	}
	if err := validateLocalSettings(in, quants); err != nil {
		return LocalSettings{}, err
	}
	temperature := in.Temperature
	client.WithSampling(deepseek.Sampling{
		Temperature:     &temperature,
		MaxTokens:       in.MaxTokens,
		ReasoningEffort: "none",
		NumCtx:          in.ContextTokens,
	})
	a.mu.Lock()
	a.localPrompt = strings.TrimSpace(in.Prompt)
	a.localQuant = in.Quant
	a.mu.Unlock()
	out := in
	out.Prompt = a.answerPrompt()
	out.Quants = quants
	return out, nil
}

func validateLocalSettings(in LocalSettings, quants []string) error {
	if in.Temperature < 0 || in.Temperature > 2 {
		return fmt.Errorf("температура от 0 до 2")
	}
	if in.MaxTokens < 32 || in.MaxTokens > 4096 {
		return fmt.Errorf("макс. токенов от 32 до 4096")
	}
	if in.ContextTokens < 1024 || in.ContextTokens > 32768 {
		return fmt.Errorf("окно контекста от 1024 до 32768")
	}
	if strings.TrimSpace(in.Prompt) == "" {
		return fmt.Errorf("шаблон промпта пустой")
	}
	if len(in.Prompt) > 4000 {
		return fmt.Errorf("шаблон промпта длиннее 4000 знаков")
	}
	if len(quants) > 0 && !containsString(quants, in.Quant) {
		return fmt.Errorf("квантование %s на хосте нет", in.Quant)
	}
	return nil
}

func uniqueQuants(models []deepseek.ModelFile) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, m := range models {
		q := strings.TrimSpace(m.Quant)
		if q == "" {
			continue
		}
		if _, ok := seen[q]; ok {
			continue
		}
		seen[q] = struct{}{}
		out = append(out, q)
	}
	return out
}

func containsString(list []string, item string) bool {
	for _, v := range list {
		if v == item {
			return true
		}
	}
	return false
}

func formatGB(n int64) string {
	if n <= 0 {
		return "размер не указан"
	}
	return fmt.Sprintf("%.1f ГБ", float64(n)/1e9)
}

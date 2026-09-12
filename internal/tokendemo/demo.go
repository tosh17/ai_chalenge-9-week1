// Package tokendemo — short / long / overflow сравнение токенов и стоимости.
package tokendemo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tosh17/deepseek-service/internal/agent"
	"github.com/tosh17/deepseek-service/internal/memory"
)

// Row — одна строка сравнения.
type Row struct {
	Scenario     string  `json:"scenario"`
	Turn         int     `json:"turn"`
	OK           bool    `json:"ok"`
	Error        string  `json:"error,omitempty"`
	Request      int     `json:"request_tokens"`
	History      int     `json:"history_tokens"`
	Completion   int     `json:"completion_tokens"`
	Prompt       int     `json:"prompt_tokens"`
	Total        int     `json:"total_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	SessionUSD   float64 `json:"session_cost_usd"`
	ContextPct   float64 `json:"context_pct"`
	OverLimit    bool    `json:"over_limit"`
	Source       string  `json:"source,omitempty"`
	ReplyPreview string  `json:"reply_preview,omitempty"`
	Status       string  `json:"status"`
}

// Report — итог демо.
type Report struct {
	Provider    string   `json:"provider"`
	Model       string   `json:"model,omitempty"`
	DemoLimit   int      `json:"demo_limit"`
	GeneratedAt string   `json:"generated_at"`
	Rows        []Row    `json:"rows"`
	Notes       []string `json:"notes"`
	Table       string   `json:"table"`
}

// Options — параметры прогона.
type Options struct {
	Provider string
	Limit    int
	Dir      string
}

// Run прогоняет short / long / overflow на клоне агента (отдельная memory).
func Run(ctx context.Context, base *agent.Agent, opt Options) (Report, error) {
	if base == nil {
		return Report{}, fmt.Errorf("agent is nil")
	}
	limit := opt.Limit
	if limit <= 0 {
		limit = 4096
	}
	dir := opt.Dir
	if dir == "" {
		dir = filepath.Join("data", "tokencmp")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Report{}, err
	}

	provider := opt.Provider
	if provider == "" {
		provider = base.DefaultProvider()
	}

	rep := Report{
		Provider:    provider,
		DemoLimit:   limit,
		GeneratedAt: time.Now().Format(time.RFC3339),
		Notes: []string{
			"request = токены текущего user-сообщения",
			"history = накопленный диалог до текущего запроса",
			"completion = ответ модели",
			"prompt = system+history+request",
			"OVERFLOW: агент блокирует вызов модели, если prompt > demo_limit",
			"стоимость: оценка по тарифам DeepSeek Flash ($0.14 in / $0.28 out за 1M)",
		},
	}

	rows := make([]Row, 0, 16)

	shortRows, model, err := runScenario(ctx, base, provider, filepath.Join(dir, "ui-short.json"), limit, "short", []string{
		"Скажи одним словом: привет",
		"А теперь одним словом: пока",
	})
	rows = append(rows, shortRows...)
	if model != "" {
		rep.Model = model
	}
	if err != nil && !isOverflow(err) {
		// short failure is soft — всё равно покажем long/overflow
	}

	chunk := strings.Repeat("факт о Спрингфилде номер X: голубое небо и жёлтый дом. ", 80)
	longMsgs := make([]string, 0, 6)
	for i := 1; i <= 5; i++ {
		longMsgs = append(longMsgs, fmt.Sprintf(
			"Запомни блок #%d и ответь коротко «ок».\n%s",
			i, strings.ReplaceAll(chunk, "X", fmt.Sprintf("%d", i)),
		))
	}
	longMsgs = append(longMsgs, "Сколько блоков я просил запомнить? Ответь числом.")
	longRows, model, _ := runScenario(ctx, base, provider, filepath.Join(dir, "ui-long.json"), limit, "long", longMsgs)
	rows = append(rows, longRows...)
	if model != "" {
		rep.Model = model
	}

	overflowLimit := limit
	if overflowLimit > 50_000 {
		overflowLimit = 2048
	}
	pad := strings.Repeat("переполнение контекста токен токен токен ", 400)
	overRows, model, _ := runScenario(ctx, base, provider, filepath.Join(dir, "ui-overflow.json"), overflowLimit, "overflow", []string{
		"Короткий старт: запомни слово ALPHA. Ответь: ок",
		pad + "\n\nПовтори слово, которое я просил запомнить.",
	})
	rows = append(rows, overRows...)
	if model != "" {
		rep.Model = model
	}

	rep.Rows = rows
	rep.Table = FormatTable(rows, limit, overflowLimit)
	return rep, nil
}

func isOverflow(err error) bool {
	var o *agent.ErrContextOverflow
	return errors.As(err, &o)
}

func runScenario(
	ctx context.Context,
	base *agent.Agent,
	provider, memPath string,
	limit int,
	name string,
	messages []string,
) ([]Row, string, error) {
	_ = os.Remove(memPath)
	store, err := memory.Open(memPath)
	if err != nil {
		return []Row{{Scenario: name, OK: false, Error: err.Error(), Status: "FAIL"}}, "", err
	}

	demo := base.Clone(fmt.Sprintf("token-demo-%s", name)).
		WithMemory(store).
		WithContextLimit(limit, true).
		WithDefaultProvider(provider)

	out := make([]Row, 0, len(messages))
	model := ""
	var lastErr error

	for i, msg := range messages {
		res, err := demo.Handle(ctx, agent.Request{Message: msg, Provider: provider})
		row := Row{Scenario: name, Turn: i + 1}
		if res.Model != "" {
			model = res.Model
		}
		if res.Tokens != nil {
			row.Request = res.Tokens.Request
			row.History = res.Tokens.History
			row.Completion = res.Tokens.Completion
			row.Prompt = res.Tokens.Prompt
			row.Total = res.Tokens.Total
			row.CostUSD = res.Tokens.CostUSD
			row.ContextPct = res.Tokens.ContextPct
			row.OverLimit = res.Tokens.OverLimit
			row.Source = res.Tokens.Source
		}
		if res.Session != nil {
			row.SessionUSD = res.Session.EstimatedCostUSD
		}
		if err != nil {
			row.OK = false
			row.Error = err.Error()
			row.Status = "FAIL"
			if isOverflow(err) {
				row.Status = "OVERFLOW"
				row.OverLimit = true
			}
			out = append(out, row)
			lastErr = err
			if isOverflow(err) {
				break
			}
			continue
		}
		row.OK = true
		row.Status = "ok"
		row.ReplyPreview = truncate(res.Reply, 60)
		out = append(out, row)
	}
	return out, model, lastErr
}

// FormatTable — понятный отчёт для чата (без жаргона).
func FormatTable(rows []Row, demoLimit, overflowLimit int) string {
	var b strings.Builder
	b.WriteString("Что проверяли\n")
	b.WriteString("─────────────\n")
	b.WriteString("Токен — кусочек текста, которым «считает» модель.\n")
	b.WriteString("Чем длиннее переписка, тем больше токенов уходит в каждый новый запрос.\n")
	b.WriteString(fmt.Sprintf("Для демо специально поставили потолок: %d токенов", demoLimit))
	if overflowLimit > 0 && overflowLimit != demoLimit {
		b.WriteString(fmt.Sprintf(" (для переполнения — %d)", overflowLimit))
	}
	b.WriteString(".\n")
	b.WriteString("Если запрос больше потолка — модель даже не вызываем.\n\n")

	groups := []struct {
		key, title, blurb string
	}{
		{"short", "1) Короткий диалог", "Два коротких сообщения. Мало текста → мало токенов → почти нулевая цена."},
		{"long", "2) Длинный диалог", "Большие куски текста подряд. История растёт → каждый следующий запрос тяжелее и дороже."},
		{"overflow", "3) Переполнение", "Намеренно отправили слишком много. Система должна остановить запрос до модели."},
	}

	byScenario := map[string][]Row{}
	for _, r := range rows {
		byScenario[r.Scenario] = append(byScenario[r.Scenario], r)
	}

	for _, g := range groups {
		list := byScenario[g.key]
		if len(list) == 0 {
			continue
		}
		b.WriteString(g.title + "\n")
		b.WriteString(g.blurb + "\n")
		for _, r := range list {
			status := "ок"
			if r.OverLimit || r.Status == "OVERFLOW" {
				status = "СТОПП — слишком много текста"
			} else if !r.OK {
				status = "ошибка"
			}
			b.WriteString(fmt.Sprintf(
				"  ход %d: твоё сообщение ≈ %d ток. · уже в истории ≈ %d · ответ модели ≈ %d · всего в запросе ≈ %d · цена ≈ $%.6f · %s\n",
				r.Turn, r.Request, r.History, r.Completion, r.Prompt, r.CostUSD, status,
			))
			if !r.OK && r.Error != "" {
				b.WriteString("    почему: " + humanizeError(r.Error) + "\n")
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("Короткий вывод\n")
	b.WriteString("──────────────\n")
	b.WriteString("• Короткий чат — дёшево и легко.\n")
	b.WriteString("• Длинный чат — токены и цена растут с каждым ходом.\n")
	b.WriteString("• Когда лимит превышен — ответ модели не приходит (защита от переполнения).\n")
	return b.String()
}

func humanizeError(err string) string {
	switch {
	case strings.Contains(err, "context overflow"):
		return "запрос больше потолка токенов — модель не вызывали"
	case strings.Contains(err, "timeout") || strings.Contains(err, "deadline"):
		return "модель слишком долго не отвечала"
	default:
		return truncate(err, 120)
	}
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

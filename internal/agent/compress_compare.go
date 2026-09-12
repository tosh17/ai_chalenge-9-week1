package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/memory"
)

// CompressCompareResult — сравнение без сжатия / со сжатием.
type CompressCompareResult struct {
	Question string `json:"question"`
	Summary  string `json:"summary"`

	Without CompressRun `json:"without_compression"`
	With    CompressRun `json:"with_compression"`

	TokenDelta int    `json:"token_delta"` // without.prompt - with.prompt (экономия)
	Report     string `json:"report"`
}

// CompressRun — один прогон контрольного вопроса.
type CompressRun struct {
	Reply              string           `json:"reply"`
	PromptTokens       int              `json:"prompt_tokens"`
	CompletionTokens   int              `json:"completion_tokens"`
	TotalTokens        int              `json:"total_tokens"`
	CostUSD            float64          `json:"cost_usd"`
	DurationMs         int64            `json:"duration_ms"`
	Compression        *CompressionInfo `json:"compression,omitempty"`
	FullHistoryMsgs    int              `json:"full_history_messages"`
}

// RunCompressCompare готовит длинную историю с фактами и спрашивает одно и то же
// без сжатия и со сжатием.
func (a *Agent) RunCompressCompare(ctx context.Context, provider string) (CompressCompareResult, error) {
	if provider == "" {
		provider = a.defaultID
	}

	dir := filepath.Join("data", "compress-demo")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, "history.json")
	_ = os.Remove(path)

	store, err := memory.Open(path)
	if err != nil {
		return CompressCompareResult{}, err
	}

	demo := a.Clone("day9-compress-demo").
		WithMemory(store).
		WithCompression(CompressionConfig{
			Enabled:        true,
			KeepLastN:      4,
			SummarizeEvery: 8,
		}).
		WithDefaultProvider(provider)

	// Факты «в начале» диалога + шум в середине, чтобы без summary их было дорого тащить.
	seeds := []struct{ u, a string }{
		{"Запомни секретный код: ORANGE-42.", "Ок, запомнил код ORANGE-42."},
		{"Меня зовут Кира, я из Казани.", "Приятно познакомиться, Кира из Казани."},
		{"Любимое блюдо — пельмени.", "Понял: любимое блюдо — пельмени."},
		{"Завтра встреча в 11:30.", "Отметил встречу на 11:30."},
		{"Пароль от шкафа: солнце.", "Ок, пароль от шкафа — солнце."},
		{"Не люблю шпинат.", "Хорошо, без шпината."},
		{"У меня кот Барсик.", "Запомнил кота Барсика."},
		{"Рабочий проект называется Нептун.", "Ок, проект Нептун."},
		{"Скажи коротко: погода нормальная?", "Да, нормальная."},
		{"Ещё раз коротко: всё ок?", "Всё ок."},
		{"Просто кивни словом да.", "да"},
		{"И ещё раз да.", "да"},
	}
	for _, s := range seeds {
		if err := store.Append(
			deepseek.Message{Role: "user", Content: s.u},
			deepseek.Message{Role: "assistant", Content: s.a},
		); err != nil {
			return CompressCompareResult{}, err
		}
	}

	// Принудительно сжать старую часть.
	if _, err := demo.maybeCompress(ctx, provider); err != nil {
		return CompressCompareResult{}, fmt.Errorf("compress: %w", err)
	}

	question := "Назови секретный код, моё имя и город, любимое блюдо и время завтрашней встречи. Коротко списком."

	off := false
	on := true

	without, err := demo.Handle(ctx, Request{Message: question, Provider: provider, Compress: &off})
	if err != nil {
		return CompressCompareResult{}, fmt.Errorf("without: %w", err)
	}
	// Откатим последние 2 сообщения (вопрос+ответ without), чтобы with шёл по той же истории.
	// Проще: переоткрыть store без последних 2 — но Append уже записал.
	// Пересоберём файл из seeds + summary.
	summary := store.Summary()
	upTo := store.SummarizedUpTo()
	_ = store.Clear()
	for _, s := range seeds {
		_ = store.Append(
			deepseek.Message{Role: "user", Content: s.u},
			deepseek.Message{Role: "assistant", Content: s.a},
		)
	}
	_ = store.SetSummary(summary, upTo)

	with, err := demo.Handle(ctx, Request{Message: question, Provider: provider, Compress: &on})
	if err != nil {
		return CompressCompareResult{}, fmt.Errorf("with: %w", err)
	}

	out := CompressCompareResult{
		Question: question,
		Summary:  store.Summary(),
		Without:  toCompressRun(without, len(seeds)*2),
		With:     toCompressRun(with, len(seeds)*2),
	}
	if without.Tokens != nil && with.Tokens != nil {
		out.TokenDelta = without.Tokens.Prompt - with.Tokens.Prompt
	}
	out.Report = formatCompressReport(out)
	return out, nil
}

func toCompressRun(r Result, fullMsgs int) CompressRun {
	cr := CompressRun{
		Reply:           r.Reply,
		DurationMs:      r.DurationMs,
		Compression:     r.Compression,
		FullHistoryMsgs: fullMsgs,
	}
	if r.Tokens != nil {
		cr.PromptTokens = r.Tokens.Prompt
		cr.CompletionTokens = r.Tokens.Completion
		cr.TotalTokens = r.Tokens.Total
		cr.CostUSD = r.Tokens.CostUSD
	}
	return cr
}

func formatCompressReport(r CompressCompareResult) string {
	var b strings.Builder
	b.WriteString("Сравнение: полная история vs сжатие\n")
	b.WriteString("══════════════════════════════════\n\n")
	b.WriteString("Вопрос к агенту (нужны факты из начала диалога):\n")
	b.WriteString(r.Question + "\n\n")

	if r.Summary != "" {
		b.WriteString("Что лежит в summary (вместо старых реплик):\n")
		b.WriteString(r.Summary + "\n\n")
	} else {
		b.WriteString("Summary пока пустой — сжатие не накопило пачку.\n\n")
	}

	b.WriteString("1) Без сжатия (вся история как есть)\n")
	b.WriteString(fmt.Sprintf("   токены запроса: %d · ответ: %d · всего: %d · цена ≈ $%.6f\n",
		r.Without.PromptTokens, r.Without.CompletionTokens, r.Without.TotalTokens, r.Without.CostUSD))
	b.WriteString("   ответ модели:\n   " + indentReply(r.Without.Reply) + "\n\n")

	b.WriteString("2) Со сжатием (summary + последние сообщения)\n")
	b.WriteString(fmt.Sprintf("   токены запроса: %d · ответ: %d · всего: %d · цена ≈ $%.6f\n",
		r.With.PromptTokens, r.With.CompletionTokens, r.With.TotalTokens, r.With.CostUSD))
	if r.With.Compression != nil {
		b.WriteString(fmt.Sprintf("   в summary сообщений: %d · сырых в запросе: %d · оценка экономии ≈ %d ток.\n",
			r.With.Compression.SummarizedMessages,
			r.With.Compression.RawMessagesInPrompt,
			r.With.Compression.TokensSavedEstimate,
		))
	}
	b.WriteString("   ответ модели:\n   " + indentReply(r.With.Reply) + "\n\n")

	b.WriteString("Итог\n")
	b.WriteString("────\n")
	if r.TokenDelta > 0 {
		b.WriteString(fmt.Sprintf("Со сжатием запрос короче примерно на %d токенов.\n", r.TokenDelta))
	} else if r.TokenDelta < 0 {
		b.WriteString(fmt.Sprintf("Со сжатием запрос даже длиннее на %d ток. (summary пока не выгоднее).\n", -r.TokenDelta))
	} else {
		b.WriteString("Длина запроса почти одинаковая.\n")
	}
	b.WriteString("Сравни ответы выше: помнит ли агент код/имя/город/блюдо/время в обоих режимах.\n")
	return b.String()
}

func indentReply(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", "\n   ")
	return s
}

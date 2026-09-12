package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/tokens"
)

// CompressionConfig — политика сжатия истории.
type CompressionConfig struct {
	Enabled        bool
	KeepLastN      int
	SummarizeEvery int
}

// CompressionInfo — что ушло в запрос после сжатия.
type CompressionInfo struct {
	Enabled             bool   `json:"enabled"`
	Summary             string `json:"summary,omitempty"`
	SummarizedMessages  int    `json:"summarized_messages"`
	RawMessagesInPrompt int    `json:"raw_messages_in_prompt"`
	FullHistoryMessages int    `json:"full_history_messages"`
	FullHistoryTokens   int    `json:"full_history_tokens"`
	CompressedPromptTok int    `json:"compressed_prompt_tokens"`
	TokensSavedEstimate int    `json:"tokens_saved_estimate"`
}

// SummarizeEvent — факт операции сжатия (для UI и учёта токенов).
type SummarizeEvent struct {
	Kind               string  `json:"kind"` // "summarize"
	MessagesCompressed int     `json:"messages_compressed"`
	PromptTokens       int     `json:"prompt_tokens"`
	CompletionTokens   int     `json:"completion_tokens"`
	TotalTokens        int     `json:"total_tokens"`
	CostUSD            float64 `json:"cost_usd"`
	DurationMs         int64   `json:"duration_ms"`
	SummaryPreview     string  `json:"summary_preview,omitempty"`
}

func defaultCompression() CompressionConfig {
	return CompressionConfig{
		Enabled:        true,
		KeepLastN:      6,
		SummarizeEvery: 10,
	}
}

// WithCompression настраивает сжатие контекста.
func (a *Agent) WithCompression(cfg CompressionConfig) *Agent {
	if cfg.KeepLastN <= 0 {
		cfg.KeepLastN = 6
	}
	if cfg.SummarizeEvery <= 0 {
		cfg.SummarizeEvery = 10
	}
	a.compression = cfg
	return a
}

// Compression возвращает текущие настройки.
func (a *Agent) Compression() CompressionConfig {
	return a.compression
}

// SetCompressionEnabled включает/выключает сжатие на лету.
func (a *Agent) SetCompressionEnabled(on bool) {
	a.compression.Enabled = on
}

// ContextStats — summary + оценка экономии для UI.
func (a *Agent) ContextStats(providerID string, compress bool) CompressionInfo {
	info := CompressionInfo{Enabled: compress}
	if a.memory == nil {
		return info
	}
	full := a.memory.Messages()
	info.FullHistoryMessages = len(full)
	info.SummarizedMessages = a.memory.SummarizedUpTo()
	info.Summary = a.memory.Summary()

	fullPrompt := a.buildMessagesRaw(Request{Message: "…"}, full)
	compPrompt := a.buildMessagesCompressed(Request{Message: "…"}, true)
	info.FullHistoryTokens = estimatePrompt(fullPrompt)
	info.CompressedPromptTok = estimatePrompt(compPrompt)
	if info.Summary != "" {
		info.RawMessagesInPrompt = len(a.memory.RawTail())
	} else {
		info.RawMessagesInPrompt = len(full)
	}
	if info.FullHistoryTokens > info.CompressedPromptTok {
		info.TokensSavedEstimate = info.FullHistoryTokens - info.CompressedPromptTok
	}
	_ = providerID
	return info
}

func estimatePrompt(msgs []deepseek.Message) int {
	n := 0
	for _, m := range msgs {
		n += tokens.EstimateMessage(m.Role, m.Content)
	}
	return n
}

// maybeCompress сжимает все накопившиеся пачки; токены суммаризации добавляет в session.
func (a *Agent) maybeCompress(ctx context.Context, providerID string) ([]SummarizeEvent, error) {
	if !a.compression.Enabled || a.memory == nil {
		return nil, nil
	}
	var events []SummarizeEvent
	for {
		pending := a.memory.PendingForSummary(a.compression.KeepLastN)
		if len(pending) < a.compression.SummarizeEvery {
			break
		}
		chunk := pending[:a.compression.SummarizeEvery]
		prev := a.memory.Summary()
		summary, usage, dur, err := a.summarizeChunk(ctx, providerID, prev, chunk)
		if err != nil {
			return events, err
		}
		upTo := a.memory.SummarizedUpTo() + len(chunk)
		if err := a.memory.SetSummary(summary, upTo); err != nil {
			return events, err
		}
		ev := SummarizeEvent{
			Kind:               "summarize",
			MessagesCompressed: len(chunk),
			DurationMs:         dur,
			SummaryPreview:     truncateRunes(summary, 180),
		}
		if usage != nil {
			ev.PromptTokens = usage.Prompt
			ev.CompletionTokens = usage.Completion
			ev.TotalTokens = usage.Total
			ev.CostUSD = usage.CostUSD
			a.addSession(*usage)
		}
		events = append(events, ev)
	}
	return events, nil
}

func (a *Agent) summarizeChunk(ctx context.Context, providerID, prevSummary string, chunk []deepseek.Message) (string, *tokens.Usage, int64, error) {
	var b strings.Builder
	b.WriteString("Ты сжимаешь историю диалога для экономии токенов.\n")
	b.WriteString("Сохрани факты, имена, договорённости, числа и важные детали.\n")
	b.WriteString("Пиши кратко на русском, связным текстом, без списков ролей.\n")
	if prevSummary != "" {
		b.WriteString("\nУже есть краткое содержание:\n")
		b.WriteString(prevSummary)
		b.WriteString("\n\nДобавь к нему новые реплики ниже, обнови summary целиком.\n")
	} else {
		b.WriteString("\nСоставь краткое содержание следующих реплик:\n")
	}
	for _, m := range chunk {
		b.WriteString(m.Role)
		b.WriteString(": ")
		b.WriteString(m.Content)
		b.WriteString("\n")
	}

	res, err := a.Complete(ctx, providerID, "Ты помощник по суммаризации диалогов. Отвечай только текстом summary.", []deepseek.Message{
		{Role: "user", Content: b.String()},
	})
	if err != nil {
		return "", res.Tokens, res.DurationMs, fmt.Errorf("summarize: %w", err)
	}
	out := strings.TrimSpace(res.Reply)
	if out == "" {
		return "", res.Tokens, res.DurationMs, fmt.Errorf("summarize: empty summary")
	}
	return out, res.Tokens, res.DurationMs, nil
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func (a *Agent) buildMessagesRaw(req Request, history []deepseek.Message) []deepseek.Message {
	out := make([]deepseek.Message, 0, len(history)+2)
	if a.systemPrompt != "" {
		out = append(out, deepseek.Message{Role: "system", Content: a.systemPrompt})
	}
	for _, m := range history {
		if m.Role == "system" {
			continue
		}
		out = append(out, m)
	}
	out = append(out, deepseek.Message{Role: "user", Content: req.Message})
	return out
}

func (a *Agent) buildMessagesCompressed(req Request, compress bool) []deepseek.Message {
	history := req.History
	if a.memory != nil {
		if compress && a.compression.Enabled {
			out := make([]deepseek.Message, 0, a.compression.KeepLastN+3)
			if a.systemPrompt != "" {
				out = append(out, deepseek.Message{Role: "system", Content: a.systemPrompt})
			}
			if sum := a.memory.Summary(); sum != "" {
				out = append(out, deepseek.Message{
					Role:    "system",
					Content: "Краткое содержание более ранней части диалога (вместо полной истории):\n" + sum,
				})
			}
			for _, m := range a.memory.RawTail() {
				if m.Role == "system" {
					continue
				}
				out = append(out, m)
			}
			out = append(out, deepseek.Message{Role: "user", Content: req.Message})
			return out
		}
		history = a.memory.Messages()
	}
	return a.buildMessagesRaw(req, history)
}

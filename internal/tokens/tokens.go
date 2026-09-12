// Package tokens — оценка и учёт токенов/стоимости для агента.
package tokens

import (
	"fmt"
	"math"
	"unicode/utf8"
)

// Usage — разбивка токенов одного хода.
type Usage struct {
	// Request — только текущее сообщение пользователя.
	Request int `json:"request"`
	// History — накопленная история диалога (без текущего запроса и system).
	History int `json:"history"`
	// System — system prompt.
	System int `json:"system"`
	// Prompt — всё, что уходит в модель (system + history + request).
	Prompt int `json:"prompt"`
	// Completion — ответ модели.
	Completion int `json:"completion"`
	// Total — prompt + completion.
	Total int `json:"total"`

	ContextLimit int     `json:"context_limit"`
	ContextPct   float64 `json:"context_pct"`
	OverLimit    bool    `json:"over_limit"`

	// Source: "estimate" до ответа API, "api" если пришёл usage.
	Source string `json:"source"`

	PromptCacheHit  int `json:"prompt_cache_hit,omitempty"`
	PromptCacheMiss int `json:"prompt_cache_miss,omitempty"`

	CostUSD   float64 `json:"cost_usd"`
	CostNote  string  `json:"cost_note,omitempty"`
	Model     string  `json:"model,omitempty"`
}

// SessionTotals — накопление по диалогу между рестартами процесса.
type SessionTotals struct {
	Turns              int     `json:"turns"`
	PromptTokens       int     `json:"prompt_tokens"`
	CompletionTokens   int     `json:"completion_tokens"`
	TotalTokens        int     `json:"total_tokens"`
	EstimatedCostUSD   float64 `json:"estimated_cost_usd"`
}

// Pricing — $/1M tokens (DeepSeek V4 Flash defaults).
type Pricing struct {
	InputPerMillion  float64 // cache miss
	OutputPerMillion float64
	CacheHitPerM     float64
}

// DefaultFlashPricing — ориентир для deepseek-v4-flash.
func DefaultFlashPricing() Pricing {
	return Pricing{
		InputPerMillion:  0.14,
		OutputPerMillion: 0.28,
		CacheHitPerM:     0.0028,
	}
}

// EstimateText — грубая оценка без tokenizer: ~1 токен на 2 руны (RU/EN чат).
func EstimateText(s string) int {
	if s == "" {
		return 0
	}
	n := utf8.RuneCountInString(s)
	return (n + 1) / 2
}

// EstimateMessage учитывает небольшой overhead роли.
func EstimateMessage(role, content string) int {
	return 4 + EstimateText(role) + EstimateText(content)
}

// EstimatePair — оценка одного сообщения.
func EstimatePair(role, content string) int {
	return EstimateMessage(role, content)
}

// CostUSD считает стоимость по usage.
func (p Pricing) CostUSD(prompt, completion, cacheHit, cacheMiss int) float64 {
	if cacheHit+cacheMiss == 0 && prompt > 0 {
		cacheMiss = prompt
	}
	in := (float64(cacheMiss)/1e6)*p.InputPerMillion + (float64(cacheHit)/1e6)*p.CacheHitPerM
	out := (float64(completion) / 1e6) * p.OutputPerMillion
	return round6(in + out)
}

func round6(v float64) float64 {
	return math.Round(v*1e6) / 1e6
}

// FormatUSD коротко для UI.
func FormatUSD(v float64) string {
	if v == 0 {
		return "$0"
	}
	if v < 0.0001 {
		return fmt.Sprintf("$%.6f", v)
	}
	return fmt.Sprintf("$%.4f", v)
}

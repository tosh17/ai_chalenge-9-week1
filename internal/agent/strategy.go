package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/tosh17/deepseek-service/internal/deepseek"
)

const (
	StrategySliding = "sliding"
	StrategyFacts   = "facts"
	StrategyBranch  = "branch"
)

// ContextStrategy — настройки управления контекстом (day10).
type ContextStrategy struct {
	Kind string `json:"kind"` // sliding | facts | branch

	// SlidingWindowN — сколько последних сообщений хранить/отправлять (sliding).
	SlidingWindowN int `json:"sliding_window_n"`

	// FactsWindowN — сколько последних сообщений слать вместе с facts.
	FactsWindowN int `json:"facts_window_n"`

	// BranchWindowN — сколько последних сообщений активной ветки слать в LLM
	// (0 = вся видимая история ветки).
	BranchWindowN int `json:"branch_window_n"`
}

// StrategyInfo — что ушло в запрос и состояние стратегии.
type StrategyInfo struct {
	Kind              string            `json:"kind"`
	WindowN           int               `json:"window_n,omitempty"`
	MessagesInPrompt  int               `json:"messages_in_prompt"`
	DiscardedMessages int               `json:"discarded_messages,omitempty"`
	Facts             map[string]string `json:"facts,omitempty"`
	FactsUpdated      bool              `json:"facts_updated,omitempty"`
	ActiveBranch      string            `json:"active_branch,omitempty"`
	Branches          []string          `json:"branches,omitempty"`
	CheckpointAt      int               `json:"checkpoint_at,omitempty"`
}

// FactUpdateEvent — обновление sticky facts (для UI и учёта токенов).
type FactUpdateEvent struct {
	Kind             string            `json:"kind"` // "facts_update"
	PromptTokens     int               `json:"prompt_tokens"`
	CompletionTokens int               `json:"completion_tokens"`
	TotalTokens      int               `json:"total_tokens"`
	CostUSD          float64           `json:"cost_usd"`
	DurationMs       int64             `json:"duration_ms"`
	Facts            map[string]string `json:"facts,omitempty"`
}

func defaultStrategy() ContextStrategy {
	return ContextStrategy{
		Kind:           StrategySliding,
		SlidingWindowN: 8,
		FactsWindowN:   6,
		BranchWindowN:  0,
	}
}

// WithStrategy задаёт стратегию контекста.
func (a *Agent) WithStrategy(s ContextStrategy) *Agent {
	a.strategy = normalizeStrategy(s)
	return a
}

// Strategy возвращает текущие настройки.
func (a *Agent) Strategy() ContextStrategy {
	return a.strategy
}

// SetStrategy обновляет стратегию на лету.
func (a *Agent) SetStrategy(s ContextStrategy) {
	a.strategy = normalizeStrategy(s)
}

func normalizeStrategy(s ContextStrategy) ContextStrategy {
	switch s.Kind {
	case StrategySliding, StrategyFacts, StrategyBranch:
	default:
		s.Kind = StrategySliding
	}
	if s.SlidingWindowN <= 0 {
		s.SlidingWindowN = 8
	}
	if s.FactsWindowN <= 0 {
		s.FactsWindowN = 6
	}
	if s.BranchWindowN < 0 {
		s.BranchWindowN = 0
	}
	return s
}

func (a *Agent) strategySnapshot() StrategyInfo {
	info := StrategyInfo{Kind: a.strategy.Kind}
	switch a.strategy.Kind {
	case StrategySliding:
		info.WindowN = a.strategy.SlidingWindowN
	case StrategyFacts:
		info.WindowN = a.strategy.FactsWindowN
	case StrategyBranch:
		info.WindowN = a.strategy.BranchWindowN
	}
	if a.memory == nil {
		return info
	}
	info.Facts = a.memory.Facts()
	info.ActiveBranch = a.memory.ActiveBranchID()
	info.CheckpointAt = a.memory.CheckpointAt()
	for _, b := range a.memory.BranchesSnapshot() {
		info.Branches = append(info.Branches, b.ID+":"+b.Title)
	}
	return info
}

func lastN(msgs []deepseek.Message, n int) []deepseek.Message {
	if n <= 0 || len(msgs) <= n {
		out := make([]deepseek.Message, len(msgs))
		copy(out, msgs)
		return out
	}
	out := make([]deepseek.Message, n)
	copy(out, msgs[len(msgs)-n:])
	return out
}

func formatFactsBlock(facts map[string]string) string {
	if len(facts) == 0 {
		return "Пока нет сохранённых фактов."
	}
	keys := make([]string, 0, len(facts))
	for k := range facts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("Важные факты диалога (key-value memory):\n")
	for _, k := range keys {
		b.WriteString("- ")
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(facts[k])
		b.WriteString("\n")
	}
	return b.String()
}

func (a *Agent) buildMessagesStrategy(req Request) ([]deepseek.Message, StrategyInfo) {
	info := a.strategySnapshot()
	history := req.History
	if a.memory != nil {
		history = a.memory.Messages()
	}

	out := make([]deepseek.Message, 0, len(history)+4)
	if a.systemPrompt != "" {
		out = append(out, deepseek.Message{Role: "system", Content: a.systemPrompt})
	}

	switch a.strategy.Kind {
	case StrategyFacts:
		facts := map[string]string{}
		if a.memory != nil {
			facts = a.memory.Facts()
		}
		info.Facts = facts
		out = append(out, deepseek.Message{Role: "system", Content: formatFactsBlock(facts)})
		tail := lastN(history, a.strategy.FactsWindowN)
		info.MessagesInPrompt = len(tail)
		info.WindowN = a.strategy.FactsWindowN
		if len(history) > len(tail) {
			info.DiscardedMessages = len(history) - len(tail)
		}
		for _, m := range tail {
			if m.Role == "system" {
				continue
			}
			out = append(out, m)
		}

	case StrategyBranch:
		n := a.strategy.BranchWindowN
		tail := history
		if n > 0 {
			tail = lastN(history, n)
		}
		info.MessagesInPrompt = len(tail)
		info.WindowN = n
		if len(history) > len(tail) {
			info.DiscardedMessages = len(history) - len(tail)
		}
		if info.ActiveBranch != "" {
			out = append(out, deepseek.Message{
				Role: "system",
				Content: fmt.Sprintf(
					"Режим branching: активная ветка %q (checkpoint @%d). Учитывай только контекст этой ветки.",
					info.ActiveBranch, info.CheckpointAt,
				),
			})
		}
		for _, m := range tail {
			if m.Role == "system" {
				continue
			}
			out = append(out, m)
		}

	default: // sliding
		n := a.strategy.SlidingWindowN
		tail := lastN(history, n)
		info.Kind = StrategySliding
		info.WindowN = n
		info.MessagesInPrompt = len(tail)
		if len(history) > len(tail) {
			info.DiscardedMessages = len(history) - len(tail)
		}
		for _, m := range tail {
			if m.Role == "system" {
				continue
			}
			out = append(out, m)
		}
	}

	out = append(out, deepseek.Message{Role: "user", Content: req.Message})
	return out, info
}

// applySlidingPersist отбрасывает старые сообщения из store (стратегия sliding).
func (a *Agent) applySlidingPersist() (discarded int, err error) {
	if a.memory == nil || a.strategy.Kind != StrategySliding {
		return 0, nil
	}
	return a.memory.TruncateKeepLast(a.strategy.SlidingWindowN)
}

// updateStickyFacts извлекает факты из реплики пользователя и мержит в memory.
func (a *Agent) updateStickyFacts(ctx context.Context, providerID, userMsg string) (FactUpdateEvent, error) {
	ev := FactUpdateEvent{Kind: "facts_update"}
	if a.memory == nil {
		return ev, nil
	}
	prev := a.memory.Facts()
	prevJSON, _ := json.Marshal(prev)

	prompt := strings.Builder{}
	prompt.WriteString("Обнови словарь важных фактов диалога (цель, ограничения, предпочтения, решения, договорённости, имена, числа).\n")
	prompt.WriteString("Верни ТОЛЬКО JSON-объект вида {\"ключ\":\"значение\"}. Без markdown.\n")
	prompt.WriteString("Пустое значение удаляет ключ. Сохрани старые факты, если они всё ещё верны.\n")
	prompt.WriteString("Текущие facts:\n")
	prompt.Write(prevJSON)
	prompt.WriteString("\n\nНовое сообщение пользователя:\n")
	prompt.WriteString(userMsg)

	res, err := a.Complete(ctx, providerID,
		"Ты извлекаешь structured facts из диалога. Отвечай только валидным JSON-объектом.",
		[]deepseek.Message{{Role: "user", Content: prompt.String()}},
	)
	ev.DurationMs = res.DurationMs
	if res.Tokens != nil {
		ev.PromptTokens = res.Tokens.Prompt
		ev.CompletionTokens = res.Tokens.Completion
		ev.TotalTokens = res.Tokens.Total
		ev.CostUSD = res.Tokens.CostUSD
		a.addSession(*res.Tokens)
	}
	if err != nil {
		return ev, err
	}

	raw := strings.TrimSpace(res.Reply)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var patch map[string]string
	if err := json.Unmarshal([]byte(raw), &patch); err != nil {
		// мягкий fallback: не валим чат
		return ev, fmt.Errorf("facts json: %w (raw=%q)", err, truncateRunes(raw, 120))
	}
	if err := a.memory.MergeFacts(patch); err != nil {
		return ev, err
	}
	ev.Facts = a.memory.Facts()
	return ev, nil
}

// ForkBranches — checkpoint + две ветки.
func (a *Agent) ForkBranches(titleA, titleB string) error {
	if a.memory == nil {
		return fmt.Errorf("agent %q: no memory", a.name)
	}
	return a.memory.ForkTwo(titleA, titleB)
}

// SwitchBranch переключает активную ветку.
func (a *Agent) SwitchBranch(id string) error {
	if a.memory == nil {
		return fmt.Errorf("agent %q: no memory", a.name)
	}
	return a.memory.SwitchBranch(id)
}

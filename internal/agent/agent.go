// Package agent — простой LLM-агент как отдельная сущность.
// Инкапсулирует подготовку запроса, выбор бэкенда, вызов модели и разбор ответа.
package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/memory"
	"github.com/tosh17/deepseek-service/internal/tokens"
)

const (
	ProviderDeepSeek = "deepseek"
	ProviderLocal    = "local"
)

// LLM — минимальный контракт модели, который использует агент.
type LLM interface {
	Chat(ctx context.Context, messages []deepseek.Message) (deepseek.ChatResult, error)
}

// Provider — доступный бэкенд для UI-переключателя.
type Provider struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Model        string `json:"model"`
	ContextLimit int    `json:"context_limit"`
	Default      bool   `json:"default,omitempty"`
}

// Request — вход агента от пользователя / UI.
type Request struct {
	Message  string
	History  []deepseek.Message // устарело: при наличии memory игнорируется
	Provider string             // deepseek | local
}

// Result — выход агента: ответ и служебные метаданные.
type Result struct {
	Reply      string
	Provider   string
	Model      string
	DurationMs int64
	Tokens     *tokens.Usage         `json:"tokens,omitempty"`
	Session    *tokens.SessionTotals `json:"session,omitempty"`
	Debug      *deepseek.DebugInfo
}

// ErrContextOverflow — prompt превышает лимит контекста.
type ErrContextOverflow struct {
	Usage tokens.Usage
}

func (e *ErrContextOverflow) Error() string {
	return fmt.Sprintf(
		"context overflow: prompt≈%d tokens exceeds limit %d (history=%d, request=%d)",
		e.Usage.Prompt, e.Usage.ContextLimit, e.Usage.History, e.Usage.Request,
	)
}

type backend struct {
	title        string
	model        string
	llm          LLM
	contextLimit int // 0 = agent default
}

// Agent — самостоятельная сущность: принимает запрос, ходит в выбранный LLM, отдаёт результат.
type Agent struct {
	name         string
	systemPrompt string
	defaultID    string
	backends     map[string]backend
	order        []string
	memory       *memory.Store

	contextLimit      int
	forceContextLimit bool
	pricing           tokens.Pricing

	mu      sync.Mutex
	session tokens.SessionTotals
}

// New создаёт агента без бэкендов — добавляйте через WithBackend.
func New(name string) *Agent {
	if name == "" {
		name = "chat-agent"
	}
	return &Agent{
		name:         name,
		systemPrompt: "Ты полезный ассистент. Отвечай кратко и по делу на языке пользователя.",
		backends:     make(map[string]backend),
		order:        nil,
		defaultID:    "",
		contextLimit: 1_000_000, // DeepSeek V4 Flash context window
		pricing:      tokens.DefaultFlashPricing(),
	}
}

// WithBackend регистрирует LLM-бэкенд (deepseek, local, …).
func (a *Agent) WithBackend(id, title, model string, llm LLM) *Agent {
	return a.WithBackendLimit(id, title, model, llm, 0)
}

// WithBackendLimit регистрирует бэкенд с собственным лимитом контекста.
func (a *Agent) WithBackendLimit(id, title, model string, llm LLM, contextLimit int) *Agent {
	if id == "" || llm == nil {
		return a
	}
	if _, exists := a.backends[id]; !exists {
		a.order = append(a.order, id)
	}
	prev := a.backends[id]
	if contextLimit <= 0 {
		contextLimit = prev.contextLimit
	}
	a.backends[id] = backend{title: title, model: model, llm: llm, contextLimit: contextLimit}
	if a.defaultID == "" {
		a.defaultID = id
	}
	return a
}

// WithDefaultProvider выбирает бэкенд по умолчанию.
func (a *Agent) WithDefaultProvider(id string) *Agent {
	if _, ok := a.backends[id]; ok {
		a.defaultID = id
	}
	return a
}

// DefaultProvider — id бэкенда по умолчанию.
func (a *Agent) DefaultProvider() string {
	return a.defaultID
}

// Clone создаёт агента с теми же бэкендами/pricing (без memory и session).
func (a *Agent) Clone(name string) *Agent {
	c := New(name)
	c.backends = a.backends
	c.order = append([]string{}, a.order...)
	c.defaultID = a.defaultID
	c.contextLimit = a.contextLimit
	c.forceContextLimit = false
	c.pricing = a.pricing
	c.systemPrompt = a.systemPrompt
	return c
}

// WithMemory подключает JSON-хранилище истории; при старте уже загружено в store.
func (a *Agent) WithMemory(store *memory.Store) *Agent {
	a.memory = store
	return a
}

// WithContextLimit задаёт лимит токенов контекста.
// force=true — игнорировать per-backend лимиты (нужно для демо overflow).
func (a *Agent) WithContextLimit(limit int, force ...bool) *Agent {
	if limit > 0 {
		a.contextLimit = limit
		a.forceContextLimit = len(force) > 0 && force[0]
	}
	return a
}

// WithPricing задаёт тарифы $/1M.
func (a *Agent) WithPricing(p tokens.Pricing) *Agent {
	a.pricing = p
	return a
}

// Memory возвращает хранилище истории (может быть nil).
func (a *Agent) Memory() *memory.Store {
	return a.memory
}

// History — сохранённые user/assistant сообщения (без system).
func (a *Agent) History() []deepseek.Message {
	if a.memory == nil {
		return nil
	}
	return a.memory.Messages()
}

// ClearHistory очищает персистентный контекст и сбрасывает session totals.
func (a *Agent) ClearHistory() error {
	a.mu.Lock()
	a.session = tokens.SessionTotals{}
	a.mu.Unlock()
	if a.memory == nil {
		return nil
	}
	return a.memory.Clear()
}

// SessionTotals — накопленные токены/стоимость с момента старта (или clear).
func (a *Agent) SessionTotals() tokens.SessionTotals {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.session
}

// ContextLimit — лимит контекста для провайдера (пустой id = default).
func (a *Agent) ContextLimit(providerID ...string) int {
	if a.forceContextLimit && a.contextLimit > 0 {
		return a.contextLimit
	}
	id := a.defaultID
	if len(providerID) > 0 && providerID[0] != "" {
		id = providerID[0]
	}
	if b, ok := a.backends[id]; ok && b.contextLimit > 0 {
		return b.contextLimit
	}
	return a.contextLimit
}

// Name возвращает имя агента.
func (a *Agent) Name() string {
	return a.name
}

// Providers — список бэкендов для UI.
func (a *Agent) Providers() []Provider {
	out := make([]Provider, 0, len(a.order))
	for _, id := range a.order {
		b := a.backends[id]
		limit := b.contextLimit
		if limit <= 0 {
			limit = a.contextLimit
		}
		out = append(out, Provider{
			ID:           id,
			Title:        b.title,
			Model:        b.model,
			ContextLimit: limit,
			Default:      id == a.defaultID,
		})
	}
	return out
}

// TokenSnapshot считает токены для текущего запроса без вызова LLM.
func (a *Agent) TokenSnapshot(message string, providerID ...string) tokens.Usage {
	pid := a.defaultID
	if len(providerID) > 0 && providerID[0] != "" {
		pid = providerID[0]
	}
	model := ""
	if b, ok := a.backends[pid]; ok {
		model = b.model
	}
	limit := a.ContextLimit(pid)

	if message == "" {
		msgs := make([]deepseek.Message, 0)
		if a.systemPrompt != "" {
			msgs = append(msgs, deepseek.Message{Role: "system", Content: a.systemPrompt})
		}
		for _, m := range a.History() {
			if m.Role == "system" {
				continue
			}
			msgs = append(msgs, m)
		}
		return a.buildTokenUsage(msgs, "", model, nil, limit)
	}
	return a.buildTokenUsage(a.buildMessages(Request{Message: message}), message, model, nil, limit)
}

// Complete — разовый вызов LLM с произвольным system и историей (без memory агента).
func (a *Agent) Complete(ctx context.Context, providerID, system string, history []deepseek.Message) (Result, error) {
	if providerID == "" {
		providerID = a.defaultID
	}
	b, ok := a.backends[providerID]
	if !ok {
		return Result{}, fmt.Errorf("agent %q: unknown provider %q", a.name, providerID)
	}

	messages := make([]deepseek.Message, 0, len(history)+1)
	if system != "" {
		messages = append(messages, deepseek.Message{Role: "system", Content: system})
	}
	for _, m := range history {
		if m.Role == "system" || m.Content == "" {
			continue
		}
		messages = append(messages, m)
	}
	if len(messages) == 0 || messages[len(messages)-1].Role == "system" {
		return Result{}, fmt.Errorf("agent %q: empty completion request", a.name)
	}

	start := time.Now()
	chat, err := b.llm.Chat(ctx, messages)
	duration := time.Since(start).Milliseconds()
	limit := a.ContextLimit(providerID)
	lastUser := ""
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			lastUser = messages[i].Content
			break
		}
	}
	usage := a.buildTokenUsage(messages, lastUser, b.model, nil, limit)

	if err != nil {
		debug := chat.Debug
		return Result{
			Provider:   providerID,
			Model:      b.model,
			DurationMs: duration,
			Tokens:     &usage,
			Debug:      &debug,
		}, fmt.Errorf("agent %q [%s]: %w", a.name, providerID, err)
	}

	usage = a.buildTokenUsage(messages, lastUser, b.model, chat.Usage, limit)
	if chat.Usage == nil {
		usage.Completion = tokens.EstimateText(chat.Reply)
		usage.Total = usage.Prompt + usage.Completion
		usage.CostUSD = a.pricing.CostUSD(usage.Prompt, usage.Completion, 0, usage.Prompt)
		usage.Source = "estimate"
	}

	debug := chat.Debug
	return Result{
		Reply:      chat.Reply,
		Provider:   providerID,
		Model:      b.model,
		DurationMs: duration,
		Tokens:     &usage,
		Debug:      &debug,
	}, nil
}

// Handle обрабатывает один ход диалога через выбранный бэкенд.
func (a *Agent) Handle(ctx context.Context, req Request) (Result, error) {
	if req.Message == "" {
		return Result{}, fmt.Errorf("agent %q: message is required", a.name)
	}

	providerID := req.Provider
	if providerID == "" {
		providerID = a.defaultID
	}
	b, ok := a.backends[providerID]
	if !ok {
		return Result{}, fmt.Errorf("agent %q: unknown provider %q", a.name, providerID)
	}

	messages := a.buildMessages(req)
	limit := a.ContextLimit(providerID)
	usage := a.buildTokenUsage(messages, req.Message, b.model, nil, limit)
	session := a.SessionTotals()

	if usage.OverLimit {
		return Result{
			Provider: providerID,
			Model:    b.model,
			Tokens:   &usage,
			Session:  &session,
		}, &ErrContextOverflow{Usage: usage}
	}

	start := time.Now()
	chat, err := b.llm.Chat(ctx, messages)
	duration := time.Since(start).Milliseconds()
	if err != nil {
		debug := chat.Debug
		return Result{
			Provider:   providerID,
			Model:      b.model,
			DurationMs: duration,
			Tokens:     &usage,
			Session:    &session,
			Debug:      &debug,
		}, fmt.Errorf("agent %q [%s]: %w", a.name, providerID, err)
	}

	usage = a.buildTokenUsage(messages, req.Message, b.model, chat.Usage, limit)
	if chat.Usage == nil {
		usage.Completion = tokens.EstimateText(chat.Reply)
		usage.Total = usage.Prompt + usage.Completion
		usage.CostUSD = a.pricing.CostUSD(usage.Prompt, usage.Completion, 0, usage.Prompt)
		usage.Source = "estimate"
	}

	sess := a.addSession(usage)

	if a.memory != nil {
		if saveErr := a.memory.Append(
			deepseek.Message{Role: "user", Content: req.Message},
			deepseek.Message{Role: "assistant", Content: chat.Reply},
		); saveErr != nil {
			return Result{
				Reply:      chat.Reply,
				Provider:   providerID,
				Model:      b.model,
				DurationMs: duration,
				Tokens:     &usage,
				Session:    &sess,
			}, fmt.Errorf("agent %q: save memory: %w", a.name, saveErr)
		}
	}

	debug := chat.Debug
	return Result{
		Reply:      chat.Reply,
		Provider:   providerID,
		Model:      b.model,
		DurationMs: duration,
		Tokens:     &usage,
		Session:    &sess,
		Debug:      &debug,
	}, nil
}

func (a *Agent) addSession(u tokens.Usage) tokens.SessionTotals {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.session.Turns++
	a.session.PromptTokens += u.Prompt
	a.session.CompletionTokens += u.Completion
	a.session.TotalTokens += u.Total
	a.session.EstimatedCostUSD += u.CostUSD
	out := a.session
	return out
}

func (a *Agent) buildTokenUsage(messages []deepseek.Message, request, model string, api *deepseek.Usage, contextLimit int) tokens.Usage {
	var historyTok, systemTok int
	for _, m := range messages {
		switch m.Role {
		case "system":
			systemTok += tokens.EstimateMessage(m.Role, m.Content)
		case "user", "assistant":
			historyTok += tokens.EstimateMessage(m.Role, m.Content)
		}
	}
	reqTok := tokens.EstimateMessage("user", request)
	if request == "" {
		reqTok = 0
	}
	if request != "" && historyTok >= reqTok {
		historyTok -= reqTok
	}

	promptEst := 0
	for _, m := range messages {
		promptEst += tokens.EstimateMessage(m.Role, m.Content)
	}

	if contextLimit <= 0 {
		contextLimit = a.contextLimit
	}

	u := tokens.Usage{
		Request:      reqTok,
		History:      historyTok,
		System:       systemTok,
		Prompt:       promptEst,
		Completion:   0,
		Total:        promptEst,
		ContextLimit: contextLimit,
		Source:       "estimate",
		Model:        model,
		CostNote:     "flash rates: in $0.14 / out $0.28 per 1M (cache-hit in $0.0028); local cost is estimate-only",
	}
	if contextLimit > 0 {
		u.ContextPct = float64(u.Prompt) / float64(contextLimit) * 100
		u.OverLimit = u.Prompt > contextLimit
	}
	u.CostUSD = a.pricing.CostUSD(u.Prompt, 0, 0, u.Prompt)

	if api != nil {
		u.Source = "api"
		u.Prompt = api.PromptTokens
		u.Completion = api.CompletionTokens
		u.Total = api.TotalTokens
		if u.Total == 0 {
			u.Total = u.Prompt + u.Completion
		}
		u.PromptCacheHit = api.PromptCacheHitTok
		u.PromptCacheMiss = api.PromptCacheMissTok
		if contextLimit > 0 {
			u.ContextPct = float64(u.Prompt) / float64(contextLimit) * 100
			u.OverLimit = u.Prompt > contextLimit
		}
		u.CostUSD = a.pricing.CostUSD(u.Prompt, u.Completion, u.PromptCacheHit, u.PromptCacheMiss)
	}
	return u
}

func (a *Agent) buildMessages(req Request) []deepseek.Message {
	history := req.History
	if a.memory != nil {
		history = a.memory.Messages()
	}

	out := make([]deepseek.Message, 0, len(history)+2)
	if a.systemPrompt != "" {
		out = append(out, deepseek.Message{
			Role:    "system",
			Content: a.systemPrompt,
		})
	}
	for _, m := range history {
		if m.Role == "system" {
			continue
		}
		out = append(out, m)
	}
	out = append(out, deepseek.Message{
		Role:    "user",
		Content: req.Message,
	})
	return out
}

package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"

	"github.com/tosh17/deepseek-service/internal/agent"
	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/tokens"
)

type Handler struct {
	agent *agent.Agent // со сжатием (основной / ChatAI)
	full  *agent.Agent // без сжатия
	model string
}

func New(a *agent.Agent, model string) *Handler {
	return &Handler{agent: a, model: model}
}

func NewDual(compress, full *agent.Agent, model string) *Handler {
	return &Handler{agent: compress, full: full, model: model}
}

type chatRequestBody struct {
	Message  string             `json:"message"`
	History  []deepseek.Message `json:"history,omitempty"`
	Provider string             `json:"provider,omitempty"`
	Compress *bool              `json:"compress,omitempty"`
}

type chatResponseBody struct {
	Reply           string                  `json:"reply"`
	Agent           string                  `json:"agent"`
	Provider        string                  `json:"provider,omitempty"`
	Model           string                  `json:"model,omitempty"`
	DurationMs      int64                   `json:"duration_ms,omitempty"`
	Tokens          *tokens.Usage           `json:"tokens,omitempty"`
	Session         *tokens.SessionTotals   `json:"session,omitempty"`
	Compression     *agent.CompressionInfo  `json:"compression,omitempty"`
	SummarizeEvents []agent.SummarizeEvent  `json:"summarize_events,omitempty"`
	Debug           *deepseek.DebugInfo     `json:"debug,omitempty"`
}

type dualPaneBody struct {
	Reply           string                 `json:"reply"`
	Agent           string                 `json:"agent"`
	Provider        string                 `json:"provider,omitempty"`
	Model           string                 `json:"model,omitempty"`
	DurationMs      int64                  `json:"duration_ms,omitempty"`
	Tokens          *tokens.Usage          `json:"tokens,omitempty"`
	Session         *tokens.SessionTotals  `json:"session,omitempty"`
	Compression     *agent.CompressionInfo `json:"compression,omitempty"`
	SummarizeEvents []agent.SummarizeEvent `json:"summarize_events,omitempty"`
	Error           string                 `json:"error,omitempty"`
	Debug           *deepseek.DebugInfo    `json:"debug,omitempty"`
}

type dualChatResponseBody struct {
	Message  string       `json:"message"`
	Compress dualPaneBody `json:"compress"`
	Full     dualPaneBody `json:"full"`
}

type providerOnlyBody struct {
	Provider string `json:"provider,omitempty"`
}

type errorResponseBody struct {
	Error   string                `json:"error"`
	Tokens  *tokens.Usage         `json:"tokens,omitempty"`
	Session *tokens.SessionTotals `json:"session,omitempty"`
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	payload := map[string]any{
		"status":           "ok",
		"agent":            h.agent.Name(),
		"providers":        h.agent.Providers(),
		"context_limit":    h.agent.ContextLimit(),
		"default_provider": h.agent.DefaultProvider(),
		"session_tokens":   h.agent.SessionTotals(),
		"dual":             h.full != nil,
	}
	if mem := h.agent.Memory(); mem != nil {
		payload["memory_path"] = mem.Path()
		payload["memory_messages"] = mem.Len()
		payload["summary"] = mem.Summary()
		payload["summarized_up_to"] = mem.SummarizedUpTo()
	}
	cfg := h.agent.Compression()
	payload["compression"] = map[string]any{
		"enabled":         cfg.Enabled,
		"keep_last_n":     cfg.KeepLastN,
		"summarize_every": cfg.SummarizeEvery,
	}
	if h.full != nil {
		payload["full_agent"] = h.full.Name()
		payload["full_session_tokens"] = h.full.SessionTotals()
		if mem := h.full.Memory(); mem != nil {
			payload["full_memory_path"] = mem.Path()
			payload["full_memory_messages"] = mem.Len()
		}
	}
	writeJSON(w, http.StatusOK, payload)
}

func (h *Handler) History(w http.ResponseWriter, r *http.Request) {
	msgs := h.agent.History()
	if msgs == nil {
		msgs = []deepseek.Message{}
	}
	snap := h.agent.TokenSnapshot("")
	writeJSON(w, http.StatusOK, map[string]any{
		"messages":      msgs,
		"count":         len(msgs),
		"tokens":        snap,
		"session":       h.agent.SessionTotals(),
		"context_limit": h.agent.ContextLimit(),
		"summary":       h.agent.Summary(),
		"compression":   h.agent.Compression(),
	})
}

func (h *Handler) HistoryDual(w http.ResponseWriter, r *http.Request) {
	if h.full == nil {
		writeJSON(w, http.StatusNotFound, errorResponseBody{Error: "dual mode not enabled"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"compress": historySide(h.agent),
		"full":     historySide(h.full),
	})
}

func historySide(a *agent.Agent) map[string]any {
	msgs := a.History()
	if msgs == nil {
		msgs = []deepseek.Message{}
	}
	return map[string]any{
		"messages":      msgs,
		"count":         len(msgs),
		"tokens":        a.TokenSnapshot(""),
		"session":       a.SessionTotals(),
		"context_limit": a.ContextLimit(),
		"summary":       a.Summary(),
		"compression":   a.Compression(),
		"agent":         a.Name(),
	}
}

func (h *Handler) ClearHistory(w http.ResponseWriter, r *http.Request) {
	if err := h.agent.ClearHistory(); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponseBody{Error: err.Error()})
		return
	}
	if h.full != nil {
		if err := h.full.ClearHistory(); err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponseBody{Error: err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "messages": []deepseek.Message{}})
}

func (h *Handler) Providers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"providers":        h.agent.Providers(),
		"default_provider": h.agent.DefaultProvider(),
	})
}

func (h *Handler) Chat(w http.ResponseWriter, r *http.Request) {
	var req chatRequestBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "invalid json body"})
		return
	}

	result, err := h.agent.Handle(r.Context(), agent.Request{
		Message:  req.Message,
		History:  req.History,
		Provider: req.Provider,
		Compress: req.Compress,
	})
	if err != nil {
		var overflow *agent.ErrContextOverflow
		if errors.As(err, &overflow) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{
				"error":             err.Error(),
				"agent":             h.agent.Name(),
				"tokens":            result.Tokens,
				"session":           result.Session,
				"compression":       result.Compression,
				"summarize_events":  result.SummarizeEvents,
			})
			return
		}
		payload := map[string]any{
			"error":             err.Error(),
			"agent":             h.agent.Name(),
			"provider":          result.Provider,
			"model":             result.Model,
			"duration_ms":       result.DurationMs,
			"tokens":            result.Tokens,
			"session":           result.Session,
			"compression":       result.Compression,
			"summarize_events":  result.SummarizeEvents,
		}
		if r.Header.Get("X-Debug") == "true" {
			payload["debug"] = result.Debug
		}
		writeJSON(w, http.StatusBadGateway, payload)
		return
	}

	body := chatResponseBody{
		Reply:           result.Reply,
		Agent:           h.agent.Name(),
		Provider:        result.Provider,
		Model:           result.Model,
		DurationMs:      result.DurationMs,
		Tokens:          result.Tokens,
		Session:         result.Session,
		Compression:     result.Compression,
		SummarizeEvents: result.SummarizeEvents,
	}
	if r.Header.Get("X-Debug") == "true" {
		body.Debug = result.Debug
	}

	writeJSON(w, http.StatusOK, body)
}

// ChatDual — один ввод → два параллельных ответа (со сжатием / без).
func (h *Handler) ChatDual(w http.ResponseWriter, r *http.Request) {
	if h.full == nil {
		writeJSON(w, http.StatusNotFound, errorResponseBody{Error: "dual mode not enabled"})
		return
	}

	var req chatRequestBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "invalid json body"})
		return
	}
	if req.Message == "" {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "message is required"})
		return
	}

	debug := r.Header.Get("X-Debug") == "true"
	var (
		wg       sync.WaitGroup
		compPane dualPaneBody
		fullPane dualPaneBody
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		compPane = runPane(r, h.agent, req.Message, req.Provider, debug)
	}()
	go func() {
		defer wg.Done()
		fullPane = runPane(r, h.full, req.Message, req.Provider, debug)
	}()
	wg.Wait()

	status := http.StatusOK
	if compPane.Error != "" && fullPane.Error != "" {
		status = http.StatusBadGateway
	}
	writeJSON(w, status, dualChatResponseBody{
		Message:  req.Message,
		Compress: compPane,
		Full:     fullPane,
	})
}

func runPane(r *http.Request, a *agent.Agent, message, provider string, debug bool) dualPaneBody {
	result, err := a.Handle(r.Context(), agent.Request{
		Message:  message,
		Provider: provider,
	})
	pane := dualPaneBody{
		Reply:           result.Reply,
		Agent:           a.Name(),
		Provider:        result.Provider,
		Model:           result.Model,
		DurationMs:      result.DurationMs,
		Tokens:          result.Tokens,
		Session:         result.Session,
		Compression:     result.Compression,
		SummarizeEvents: result.SummarizeEvents,
	}
	if debug {
		pane.Debug = result.Debug
	}
	if err != nil {
		pane.Error = err.Error()
		if pane.Reply == "" {
			pane.Reply = err.Error()
		}
	}
	return pane
}

// ChatAIRun — один шаг диалога двух персонажей (клиент крутит цикл до Стоп).
func (h *Handler) ChatAIRun(w http.ResponseWriter, r *http.Request) {
	var req agent.DialogueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "invalid json body"})
		return
	}

	result, err := h.agent.RunDialogueStep(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errorResponseBody{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type compressionSettingsBody struct {
	Enabled *bool `json:"enabled"`
}

// CompressionGet — текущие настройки сжатия.
func (h *Handler) CompressionGet(w http.ResponseWriter, r *http.Request) {
	cfg := h.agent.Compression()
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":         cfg.Enabled,
		"keep_last_n":     cfg.KeepLastN,
		"summarize_every": cfg.SummarizeEvery,
		"summary":         h.agent.Summary(),
	})
}

// CompressionSet — включить/выключить сжатие (только у compress-агента).
func (h *Handler) CompressionSet(w http.ResponseWriter, r *http.Request) {
	var req compressionSettingsBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "invalid json body"})
		return
	}
	if req.Enabled != nil {
		h.agent.SetCompressionEnabled(*req.Enabled)
	}
	h.CompressionGet(w, r)
}

// CompressDemo — сравнение качества и токенов с/без сжатия.
func (h *Handler) CompressDemo(w http.ResponseWriter, r *http.Request) {
	var req providerOnlyBody
	_ = json.NewDecoder(r.Body).Decode(&req)
	result, err := h.agent.RunCompressCompare(r.Context(), req.Provider)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errorResponseBody{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

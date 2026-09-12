package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/tosh17/deepseek-service/internal/agent"
	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/tokens"
	"github.com/tosh17/deepseek-service/internal/tokendemo"
)

type Handler struct {
	agent *agent.Agent
	crew  *agent.DesignCrew
	model string
}

func New(a *agent.Agent, crew *agent.DesignCrew, model string) *Handler {
	return &Handler{agent: a, crew: crew, model: model}
}

type chatRequestBody struct {
	Message  string             `json:"message"`
	History  []deepseek.Message `json:"history,omitempty"`
	Provider string             `json:"provider,omitempty"`
}

type chatResponseBody struct {
	Reply      string                `json:"reply"`
	Agent      string                `json:"agent"`
	Provider   string                `json:"provider,omitempty"`
	Model      string                `json:"model,omitempty"`
	DurationMs int64                 `json:"duration_ms,omitempty"`
	Tokens     *tokens.Usage         `json:"tokens,omitempty"`
	Session    *tokens.SessionTotals `json:"session,omitempty"`
	Debug      *deepseek.DebugInfo   `json:"debug,omitempty"`
}

type designRequestBody struct {
	Wish     string `json:"wish"`
	Provider string `json:"provider,omitempty"`
}

type tokenDemoRequestBody struct {
	Provider string `json:"provider,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

type errorResponseBody struct {
	Error   string                `json:"error"`
	Tokens  *tokens.Usage         `json:"tokens,omitempty"`
	Session *tokens.SessionTotals `json:"session,omitempty"`
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	payload := map[string]any{
		"status":         "ok",
		"agent":          h.agent.Name(),
		"providers":      h.agent.Providers(),
		"context_limit":     h.agent.ContextLimit(),
		"default_provider":  h.agent.DefaultProvider(),
		"session_tokens":    h.agent.SessionTotals(),
	}
	if mem := h.agent.Memory(); mem != nil {
		payload["memory_path"] = mem.Path()
		payload["memory_messages"] = mem.Len()
	}
	writeJSON(w, http.StatusOK, payload)
}

func (h *Handler) History(w http.ResponseWriter, r *http.Request) {
	msgs := h.agent.History()
	if msgs == nil {
		msgs = []deepseek.Message{}
	}
	snap := h.agent.TokenSnapshot("")
	// TokenSnapshot with empty message still counts system+history; fix request=0
	writeJSON(w, http.StatusOK, map[string]any{
		"messages":       msgs,
		"count":          len(msgs),
		"tokens":         snap,
		"session":        h.agent.SessionTotals(),
		"context_limit":  h.agent.ContextLimit(),
	})
}

func (h *Handler) ClearHistory(w http.ResponseWriter, r *http.Request) {
	if err := h.agent.ClearHistory(); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponseBody{Error: err.Error()})
		return
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
	})
	if err != nil {
		var overflow *agent.ErrContextOverflow
		if errors.As(err, &overflow) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{
				"error":   err.Error(),
				"agent":   h.agent.Name(),
				"tokens":  result.Tokens,
				"session": result.Session,
			})
			return
		}
		payload := map[string]any{
			"error":       err.Error(),
			"agent":       h.agent.Name(),
			"provider":    result.Provider,
			"model":       result.Model,
			"duration_ms": result.DurationMs,
			"tokens":      result.Tokens,
			"session":     result.Session,
		}
		if r.Header.Get("X-Debug") == "true" {
			payload["debug"] = result.Debug
		}
		writeJSON(w, http.StatusBadGateway, payload)
		return
	}

	body := chatResponseBody{
		Reply:      result.Reply,
		Agent:      h.agent.Name(),
		Provider:   result.Provider,
		Model:      result.Model,
		DurationMs: result.DurationMs,
		Tokens:     result.Tokens,
		Session:    result.Session,
	}
	if r.Header.Get("X-Debug") == "true" {
		body.Debug = result.Debug
	}

	writeJSON(w, http.StatusOK, body)
}

// Design запускает цепочку: prompt-agent → designer-agent → apply-agent.
func (h *Handler) Design(w http.ResponseWriter, r *http.Request) {
	if h.crew == nil {
		writeJSON(w, http.StatusServiceUnavailable, errorResponseBody{Error: "design crew is not configured"})
		return
	}

	var req designRequestBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "invalid json body"})
		return
	}

	result, err := h.crew.Run(r.Context(), req.Wish, req.Provider)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errorResponseBody{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, result)
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

// TokenDemo — short / long / overflow сравнение токенов (отдельная memory, основной чат не трогает).
func (h *Handler) TokenDemo(w http.ResponseWriter, r *http.Request) {
	var req tokenDemoRequestBody
	_ = json.NewDecoder(r.Body).Decode(&req)

	limit := req.Limit
	if limit <= 0 {
		limit = 4096
	}

	result, err := tokendemo.Run(r.Context(), h.agent, tokendemo.Options{
		Provider: req.Provider,
		Limit:    limit,
	})
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

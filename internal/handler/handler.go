package handler

import (
	"encoding/json"
	"net/http"

	"github.com/tosh17/deepseek-service/internal/agent"
	"github.com/tosh17/deepseek-service/internal/deepseek"
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
	Reply      string              `json:"reply"`
	Agent      string              `json:"agent"`
	Provider   string              `json:"provider,omitempty"`
	Model      string              `json:"model,omitempty"`
	DurationMs int64               `json:"duration_ms,omitempty"`
	Debug      *deepseek.DebugInfo `json:"debug,omitempty"`
}

type designRequestBody struct {
	Wish     string `json:"wish"`
	Provider string `json:"provider,omitempty"`
}

type errorResponseBody struct {
	Error string `json:"error"`
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"agent":     h.agent.Name(),
		"providers": h.agent.Providers(),
	})
}

func (h *Handler) Providers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"providers": h.agent.Providers(),
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
		if r.Header.Get("X-Debug") == "true" {
			writeJSON(w, http.StatusBadGateway, map[string]any{
				"error":       err.Error(),
				"agent":       h.agent.Name(),
				"provider":    result.Provider,
				"model":       result.Model,
				"duration_ms": result.DurationMs,
				"debug":       result.Debug,
			})
			return
		}
		writeJSON(w, http.StatusBadGateway, errorResponseBody{Error: err.Error()})
		return
	}

	body := chatResponseBody{
		Reply:      result.Reply,
		Agent:      h.agent.Name(),
		Provider:   result.Provider,
		Model:      result.Model,
		DurationMs: result.DurationMs,
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

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

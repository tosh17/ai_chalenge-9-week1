package handler

import (
	"encoding/json"
	"net/http"

	"github.com/tosh17/deepseek-service/internal/agent"
	"github.com/tosh17/deepseek-service/internal/deepseek"
)

type Handler struct {
	agent *agent.Agent
	model string
}

func New(a *agent.Agent, model string) *Handler {
	return &Handler{agent: a, model: model}
}

type chatRequestBody struct {
	Message string             `json:"message"`
	History []deepseek.Message `json:"history,omitempty"`
}

type chatResponseBody struct {
	Reply      string              `json:"reply"`
	Agent      string              `json:"agent"`
	DurationMs int64               `json:"duration_ms,omitempty"`
	Debug      *deepseek.DebugInfo `json:"debug,omitempty"`
}

type errorResponseBody struct {
	Error string `json:"error"`
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"agent":  h.agent.Name(),
	})
}

func (h *Handler) Chat(w http.ResponseWriter, r *http.Request) {
	var req chatRequestBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "invalid json body"})
		return
	}

	result, err := h.agent.Handle(r.Context(), agent.Request{
		Message: req.Message,
		History: req.History,
	})
	if err != nil {
		if r.Header.Get("X-Debug") == "true" {
			writeJSON(w, http.StatusBadGateway, map[string]any{
				"error":       err.Error(),
				"agent":       h.agent.Name(),
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
		DurationMs: result.DurationMs,
	}
	if r.Header.Get("X-Debug") == "true" {
		body.Debug = result.Debug
	}

	writeJSON(w, http.StatusOK, body)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

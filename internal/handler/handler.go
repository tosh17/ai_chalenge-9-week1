package handler

import (
	"encoding/json"
	"net/http"

	"github.com/tosh17/deepseek-service/internal/deepseek"
)

type Handler struct {
	client *deepseek.Client
	model  string
}

func New(client *deepseek.Client, model string) *Handler {
	return &Handler{client: client, model: model}
}

type chatRequestBody struct {
	Message string             `json:"message"`
	History []deepseek.Message `json:"history,omitempty"`
}

type chatResponseBody struct {
	Reply string              `json:"reply"`
	Debug *deepseek.DebugInfo `json:"debug,omitempty"`
}

type errorResponseBody struct {
	Error string `json:"error"`
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) Chat(w http.ResponseWriter, r *http.Request) {
	var req chatRequestBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "invalid json body"})
		return
	}

	if req.Message == "" {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "message is required"})
		return
	}

	messages := append([]deepseek.Message{}, req.History...)
	messages = append(messages, deepseek.Message{
		Role:    "user",
		Content: req.Message,
	})

	reply, err := h.client.Chat(r.Context(), messages)
	if err != nil {
		resp := errorResponseBody{Error: err.Error()}
		if r.Header.Get("X-Debug") == "true" {
			writeJSON(w, http.StatusBadGateway, map[string]any{
				"error": err.Error(),
				"debug": reply.Debug,
			})
			return
		}
		writeJSON(w, http.StatusBadGateway, resp)
		return
	}

	body := chatResponseBody{Reply: reply.Reply}
	if r.Header.Get("X-Debug") == "true" {
		body.Debug = &reply.Debug
	}

	writeJSON(w, http.StatusOK, body)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

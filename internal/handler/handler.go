package handler

import (
	"encoding/json"
	"fmt"
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
	Message       string             `json:"message"`
	History       []deepseek.Message `json:"history,omitempty"`
	Persona       string             `json:"persona,omitempty"`
	CustomPersona string             `json:"custom_persona,omitempty"`
	MaxTokens     int                `json:"max_tokens,omitempty"`
	MaxWords      int                `json:"max_words,omitempty"`
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

func (h *Handler) Personas(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"personas": deepseek.Personas()})
}

func (req chatRequestBody) options() deepseek.ChatOptions {
	return deepseek.ChatOptions{
		Persona:       req.Persona,
		CustomPersona: req.CustomPersona,
		MaxTokens:     req.MaxTokens,
		MaxWords:      req.MaxWords,
	}
}

func (req chatRequestBody) messages() ([]deepseek.Message, error) {
	if req.Message == "" {
		return nil, fmt.Errorf("message is required")
	}
	messages := append([]deepseek.Message{}, req.History...)
	messages = append(messages, deepseek.Message{
		Role:    "user",
		Content: req.Message,
	})
	return messages, nil
}

func (h *Handler) Chat(w http.ResponseWriter, r *http.Request) {
	var req chatRequestBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "invalid json body"})
		return
	}

	messages, err := req.messages()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: err.Error()})
		return
	}

	reply, err := h.client.Chat(r.Context(), messages, req.options())
	if err != nil {
		if r.Header.Get("X-Debug") == "true" {
			writeJSON(w, http.StatusBadGateway, map[string]any{
				"error": err.Error(),
				"debug": reply.Debug,
			})
			return
		}
		writeJSON(w, http.StatusBadGateway, errorResponseBody{Error: err.Error()})
		return
	}

	body := chatResponseBody{Reply: reply.Reply}
	if r.Header.Get("X-Debug") == "true" {
		body.Debug = &reply.Debug
	}
	writeJSON(w, http.StatusOK, body)
}

// ChatStream отдаёт SSE: event token / done / error.
func (h *Handler) ChatStream(w http.ResponseWriter, r *http.Request) {
	var req chatRequestBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "invalid json body"})
		return
	}

	messages, err := req.messages()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: err.Error()})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, errorResponseBody{Error: "streaming unsupported"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	debug := r.Header.Get("X-Debug") == "true"
	writeSSE := func(event string, payload any) {
		data, _ := json.Marshal(payload)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
		flusher.Flush()
	}

	result, err := h.client.ChatStream(r.Context(), messages, req.options(), func(delta string) error {
		writeSSE("token", map[string]string{"delta": delta})
		return nil
	})

	if err != nil {
		if r.Context().Err() != nil {
			writeSSE("aborted", map[string]string{"reply": result.Reply})
			return
		}
		payload := map[string]any{"error": err.Error()}
		if debug {
			payload["debug"] = result.Debug
		}
		writeSSE("error", payload)
		return
	}

	done := map[string]any{"reply": result.Reply}
	if debug {
		done["debug"] = result.Debug
	}
	writeSSE("done", done)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

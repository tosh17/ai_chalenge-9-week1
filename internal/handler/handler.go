package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

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

func (h *Handler) Models(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"models": deepseek.TierSpecs()})
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

type compareRequestBody struct {
	Message string `json:"message"`
}

// Compare стримит SSE: card_start / card_done / done / aborted.
func (h *Handler) Compare(w http.ResponseWriter, r *http.Request) {
	var req compareRequestBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "invalid json body"})
		return
	}
	if req.Message == "" {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "message is required"})
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

	var writeMu sync.Mutex
	writeSSE := func(event string, payload any) {
		data, _ := json.Marshal(payload)
		writeMu.Lock()
		defer writeMu.Unlock()
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
		flusher.Flush()
	}

	for _, meta := range deepseek.TierSpecs() {
		writeSSE("card_start", meta)
	}

	results := h.client.Compare(r.Context(), req.Message, func(result deepseek.ModelCompareResult) {
		writeSSE("card_done", result)
	})

	if r.Context().Err() != nil {
		writeSSE("aborted", map[string]any{"results": results})
		return
	}

	writeSSE("done", map[string]any{"results": results})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/tosh17/deepseek-service/internal/agent"
	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/tokens"
)

type Handler struct {
	agent *agent.Agent
	full  *agent.Agent // optional day9 dual
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
	Reply           string                 `json:"reply"`
	Agent           string                 `json:"agent"`
	Provider        string                 `json:"provider,omitempty"`
	Model           string                 `json:"model,omitempty"`
	DurationMs      int64                  `json:"duration_ms,omitempty"`
	Tokens          *tokens.Usage          `json:"tokens,omitempty"`
	Session         *tokens.SessionTotals  `json:"session,omitempty"`
	Compression     *agent.CompressionInfo `json:"compression,omitempty"`
	SummarizeEvents []agent.SummarizeEvent `json:"summarize_events,omitempty"`
	Strategy        *agent.StrategyInfo    `json:"strategy,omitempty"`
	FactEvents      []agent.FactUpdateEvent `json:"fact_events,omitempty"`
	Debug           *deepseek.DebugInfo    `json:"debug,omitempty"`
}

type errorResponseBody struct {
	Error   string                `json:"error"`
	Tokens  *tokens.Usage         `json:"tokens,omitempty"`
	Session *tokens.SessionTotals `json:"session,omitempty"`
}

type providerOnlyBody struct {
	Provider string `json:"provider,omitempty"`
}

type strategyBody struct {
	Kind           string `json:"kind"`
	SlidingWindowN int    `json:"sliding_window_n"`
	FactsWindowN   int    `json:"facts_window_n"`
	BranchWindowN  int    `json:"branch_window_n"`
}

type branchBody struct {
	Branch string `json:"branch"`
	TitleA string `json:"title_a,omitempty"`
	TitleB string `json:"title_b,omitempty"`
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	payload := map[string]any{
		"status":           "ok",
		"agent":            h.agent.Name(),
		"providers":        h.agent.Providers(),
		"context_limit":    h.agent.ContextLimit(),
		"default_provider": h.agent.DefaultProvider(),
		"session_tokens":   h.agent.SessionTotals(),
		"strategy":         h.agent.Strategy(),
	}
	if mem := h.agent.Memory(); mem != nil {
		payload["memory_path"] = mem.Path()
		payload["memory_messages"] = mem.Len()
		payload["summary"] = mem.Summary()
		payload["facts"] = mem.Facts()
		payload["branches"] = mem.BranchesSnapshot()
		payload["active_branch"] = mem.ActiveBranchID()
		payload["checkpoint_at"] = mem.CheckpointAt()
	}
	writeJSON(w, http.StatusOK, payload)
}

func (h *Handler) History(w http.ResponseWriter, r *http.Request) {
	msgs := h.agent.History()
	if msgs == nil {
		msgs = []deepseek.Message{}
	}
	snap := h.agent.TokenSnapshot("")
	payload := map[string]any{
		"messages":      msgs,
		"count":         len(msgs),
		"tokens":        snap,
		"session":       h.agent.SessionTotals(),
		"context_limit": h.agent.ContextLimit(),
		"strategy":      h.agent.Strategy(),
	}
	if mem := h.agent.Memory(); mem != nil {
		payload["facts"] = mem.Facts()
		payload["branches"] = mem.BranchesSnapshot()
		payload["active_branch"] = mem.ActiveBranchID()
		payload["checkpoint_at"] = mem.CheckpointAt()
		payload["summary"] = mem.Summary()
	}
	writeJSON(w, http.StatusOK, payload)
}

func (h *Handler) ClearHistory(w http.ResponseWriter, r *http.Request) {
	if err := h.agent.ClearHistory(); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponseBody{Error: err.Error()})
		return
	}
	if h.full != nil {
		_ = h.full.ClearHistory()
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
				"error":            err.Error(),
				"agent":            h.agent.Name(),
				"tokens":           result.Tokens,
				"session":          result.Session,
				"strategy":         result.Strategy,
				"fact_events":      result.FactEvents,
				"summarize_events": result.SummarizeEvents,
			})
			return
		}
		payload := map[string]any{
			"error":            err.Error(),
			"agent":            h.agent.Name(),
			"provider":         result.Provider,
			"model":            result.Model,
			"duration_ms":      result.DurationMs,
			"tokens":           result.Tokens,
			"session":          result.Session,
			"strategy":         result.Strategy,
			"fact_events":      result.FactEvents,
			"summarize_events": result.SummarizeEvents,
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
		Strategy:        result.Strategy,
		FactEvents:      result.FactEvents,
	}
	if r.Header.Get("X-Debug") == "true" {
		body.Debug = result.Debug
	}
	writeJSON(w, http.StatusOK, body)
}

func (h *Handler) StrategyGet(w http.ResponseWriter, r *http.Request) {
	st := h.agent.Strategy()
	payload := map[string]any{
		"strategy": st,
		"kinds": []map[string]string{
			{"id": agent.StrategySliding, "title": "Sliding Window", "desc": "Только последние N сообщений"},
			{"id": agent.StrategyFacts, "title": "Sticky Facts", "desc": "Facts KV + последние N"},
			{"id": agent.StrategyBranch, "title": "Branching", "desc": "Checkpoint и независимые ветки"},
		},
	}
	if mem := h.agent.Memory(); mem != nil {
		payload["facts"] = mem.Facts()
		payload["branches"] = mem.BranchesSnapshot()
		payload["active_branch"] = mem.ActiveBranchID()
		payload["checkpoint_at"] = mem.CheckpointAt()
	}
	writeJSON(w, http.StatusOK, payload)
}

func (h *Handler) StrategySet(w http.ResponseWriter, r *http.Request) {
	var req strategyBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "invalid json body"})
		return
	}
	h.agent.SetStrategy(agent.ContextStrategy{
		Kind:           req.Kind,
		SlidingWindowN: req.SlidingWindowN,
		FactsWindowN:   req.FactsWindowN,
		BranchWindowN:  req.BranchWindowN,
	})
	h.StrategyGet(w, r)
}

func (h *Handler) BranchFork(w http.ResponseWriter, r *http.Request) {
	var req branchBody
	_ = json.NewDecoder(r.Body).Decode(&req)
	if err := h.agent.ForkBranches(req.TitleA, req.TitleB); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: err.Error()})
		return
	}
	h.StrategyGet(w, r)
}

func (h *Handler) BranchSwitch(w http.ResponseWriter, r *http.Request) {
	var req branchBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Branch == "" {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "branch is required"})
		return
	}
	if err := h.agent.SwitchBranch(req.Branch); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: err.Error()})
		return
	}
	h.History(w, r)
}

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

func (h *Handler) StrategyCompare(w http.ResponseWriter, r *http.Request) {
	var req providerOnlyBody
	_ = json.NewDecoder(r.Body).Decode(&req)
	result, err := h.agent.RunStrategyCompare(r.Context(), req.Provider)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errorResponseBody{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// CompressDemo — legacy day9 compare (optional).
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

func (h *Handler) CompressionGet(w http.ResponseWriter, r *http.Request) {
	cfg := h.agent.Compression()
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":         cfg.Enabled,
		"keep_last_n":     cfg.KeepLastN,
		"summarize_every": cfg.SummarizeEvery,
		"summary":         h.agent.Summary(),
	})
}

func (h *Handler) CompressionSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "invalid json body"})
		return
	}
	if req.Enabled != nil {
		h.agent.SetCompressionEnabled(*req.Enabled)
	}
	h.CompressionGet(w, r)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

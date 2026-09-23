package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/tosh17/deepseek-service/internal/agent"
	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/memory"
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
	Message          string             `json:"message"`
	History          []deepseek.Message `json:"history,omitempty"`
	Provider         string             `json:"provider,omitempty"`
	Compress         *bool              `json:"compress,omitempty"`
	InjectSTM        *bool              `json:"inject_stm,omitempty"`
	InjectWM         *bool              `json:"inject_wm,omitempty"`
	InjectLTM        *bool              `json:"inject_ltm,omitempty"`
	InjectProfile    *bool              `json:"inject_profile,omitempty"`
	InjectInvariants *bool              `json:"inject_invariants,omitempty"`
}

type chatResponseBody struct {
	Reply           string                     `json:"reply"`
	Agent           string                     `json:"agent"`
	Provider        string                     `json:"provider,omitempty"`
	Model           string                     `json:"model,omitempty"`
	DurationMs      int64                      `json:"duration_ms,omitempty"`
	Tokens          *tokens.Usage              `json:"tokens,omitempty"`
	Session         *tokens.SessionTotals      `json:"session,omitempty"`
	Compression     *agent.CompressionInfo     `json:"compression,omitempty"`
	SummarizeEvents []agent.SummarizeEvent     `json:"summarize_events,omitempty"`
	Strategy        *agent.StrategyInfo        `json:"strategy,omitempty"`
	FactEvents      []agent.FactUpdateEvent    `json:"fact_events,omitempty"`
	Memory          *agent.MemoryInfo          `json:"memory,omitempty"`
	RouteEvents     []agent.MemoryRouteEvent   `json:"route_events,omitempty"`
	Conflicts       []memory.InvariantConflict `json:"conflicts,omitempty"`
	Skips           []memory.IllegalShift      `json:"skips,omitempty"`
	ToolEvents      []agent.ToolEvent          `json:"tool_events,omitempty"`
	Steps           []agent.TurnStep           `json:"steps,omitempty"`
	Debug           *deepseek.DebugInfo        `json:"debug,omitempty"`
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

func mcpStatus(a *agent.Agent) map[string]any {
	if a == nil || a.Tools() == nil {
		return map[string]any{"connected": false}
	}
	tools := a.Tools().Tools()
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return map[string]any{"connected": true, "tools": names}
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
		"memory_policy":    h.agent.MemoryPolicy(),
		"mcp":              mcpStatus(h.agent),
	}
	if layers := h.agent.Layers(); layers != nil {
		snap := layers.Snapshot()
		payload["memory_layers"] = snap
		payload["memory_dir"] = layers.Dir()
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
		"memory":        h.agent.MemorySnapshot(),
		"memory_policy": h.agent.MemoryPolicy(),
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
		Message:          req.Message,
		History:          req.History,
		Provider:         req.Provider,
		Compress:         req.Compress,
		InjectSTM:        req.InjectSTM,
		InjectWM:         req.InjectWM,
		InjectLTM:        req.InjectLTM,
		InjectProfile:    req.InjectProfile,
		InjectInvariants: req.InjectInvariants,
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
				"memory":           result.Memory,
				"route_events":     result.RouteEvents,
				"steps":            result.Steps,
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
			"memory":           result.Memory,
			"route_events":     result.RouteEvents,
			"steps":            result.Steps,
		}
		if result.Debug != nil {
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
		Memory:          result.Memory,
		RouteEvents:     result.RouteEvents,
		Conflicts:       result.Conflicts,
		Skips:           result.Skips,
		ToolEvents:      result.ToolEvents,
		Steps:           result.Steps,
		Debug:           result.Debug,
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

func (h *Handler) MemoryGet(w http.ResponseWriter, r *http.Request) {
	payload := map[string]any{
		"policy":     h.agent.MemoryPolicy(),
		"memory":     h.agent.MemorySnapshot(),
		"profiles":   h.profilePayload(),
		"invariants": h.invariantPayload(),
		"layers": []map[string]string{
			{"id": "short_term", "title": "Краткосрочная", "desc": "Текущий диалог. Окно STM вытесняет старые реплики."},
			{"id": "working", "title": "Рабочая", "desc": "Данные текущей задачи и конечный автомат: этап, шаг, ожидание, пауза."},
			{"id": "long_term", "title": "Долговременная", "desc": "Профиль-факты, решения, знания между сессиями."},
		},
	}
	writeJSON(w, http.StatusOK, payload)
}

func (h *Handler) profilePayload() map[string]any {
	book := h.agent.Profiles()
	if book == nil {
		return map[string]any{"active_id": "", "active": memory.UserProfile{}, "profiles": []memory.UserProfile{}}
	}
	return map[string]any{
		"active_id": book.ActiveID(),
		"active":    book.Active(),
		"profiles":  book.List(),
		"path":      book.Path(),
	}
}

func (h *Handler) ProfileGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.profilePayload())
}

func (h *Handler) ProfileSave(w http.ResponseWriter, r *http.Request) {
	var p memory.UserProfile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "invalid json body"})
		return
	}
	book := h.agent.Profiles()
	if book == nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "profiles not enabled"})
		return
	}
	if err := book.Upsert(p); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: err.Error()})
		return
	}
	if p.ID != "" {
		_ = book.Activate(p.ID)
	}
	writeJSON(w, http.StatusOK, h.profilePayload())
}

func (h *Handler) ProfileActivate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "id is required"})
		return
	}
	book := h.agent.Profiles()
	if book == nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "profiles not enabled"})
		return
	}
	if err := book.Activate(req.ID); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, h.profilePayload())
}

func (h *Handler) ProfileDemo(w http.ResponseWriter, r *http.Request) {
	var req providerOnlyBody
	_ = json.NewDecoder(r.Body).Decode(&req)
	result, err := h.agent.RunProfileDemo(r.Context(), req.Provider)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errorResponseBody{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) invariantPayload() map[string]any {
	book := h.agent.Invariants()
	if book == nil {
		return map[string]any{"items": []memory.Invariant{}, "kinds": invariantKindOptions()}
	}
	return map[string]any{
		"items": book.List(),
		"path":  book.Path(),
		"kinds": invariantKindOptions(),
	}
}

func invariantKindOptions() []map[string]string {
	return []map[string]string{
		{"id": memory.InvariantArchitecture, "title": "архитектура"},
		{"id": memory.InvariantDecision, "title": "решение"},
		{"id": memory.InvariantStack, "title": "стек"},
		{"id": memory.InvariantBusiness, "title": "бизнес-правило"},
	}
}

func (h *Handler) InvariantGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.invariantPayload())
}

func (h *Handler) InvariantSave(w http.ResponseWriter, r *http.Request) {
	var inv memory.Invariant
	if err := json.NewDecoder(r.Body).Decode(&inv); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "invalid json body"})
		return
	}
	book := h.agent.Invariants()
	if book == nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "invariants not enabled"})
		return
	}
	inv.Enabled = true
	if err := book.Upsert(inv); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, h.invariantPayload())
}

func (h *Handler) InvariantToggle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      string `json:"id"`
		Enabled *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" || req.Enabled == nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "id and enabled are required"})
		return
	}
	book := h.agent.Invariants()
	if book == nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "invariants not enabled"})
		return
	}
	if err := book.SetEnabled(req.ID, *req.Enabled); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, h.invariantPayload())
}

func (h *Handler) InvariantDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		var req struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		id = req.ID
	}
	book := h.agent.Invariants()
	if book == nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "invariants not enabled"})
		return
	}
	if err := book.Delete(id); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, h.invariantPayload())
}

func (h *Handler) InvariantDemo(w http.ResponseWriter, r *http.Request) {
	var req providerOnlyBody
	_ = json.NewDecoder(r.Body).Decode(&req)
	result, err := h.agent.RunInvariantDemo(r.Context(), req.Provider)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errorResponseBody{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) TaskEvent(w http.ResponseWriter, r *http.Request) {
	var req memory.WorkingPatch
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "invalid json body"})
		return
	}
	layers := h.agent.Layers()
	if layers == nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "layers not enabled"})
		return
	}
	if req.Event == "" && req.Stage == "" && req.Step == "" && req.Expect == "" {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "event, stage, step or expect is required"})
		return
	}
	if err := layers.ApplyWorking(req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": err.Error(),
			"skip": memory.IllegalShift{
				From:    layers.Working().Task.Stage,
				Want:    memory.NormalizeStage(req.Stage),
				Allowed: "",
				Event:   req.Event,
				Reason:  err.Error(),
			},
			"task": layers.Working().Task,
		})
		return
	}
	h.MemoryGet(w, r)
}

func (h *Handler) TaskSeed(w http.ResponseWriter, r *http.Request) {
	layers := h.agent.Layers()
	if layers == nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "layers not enabled"})
		return
	}
	if err := agent.SeedExampleTask(layers); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponseBody{Error: err.Error()})
		return
	}
	h.MemoryGet(w, r)
}

func (h *Handler) TaskDemo(w http.ResponseWriter, r *http.Request) {
	var req providerOnlyBody
	_ = json.NewDecoder(r.Body).Decode(&req)
	result, err := h.agent.RunTaskDemo(r.Context(), req.Provider)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errorResponseBody{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) TaskLifecycle(w http.ResponseWriter, r *http.Request) {
	var req providerOnlyBody
	_ = json.NewDecoder(r.Body).Decode(&req)
	result, err := h.agent.RunLifecycleDemo(r.Context(), req.Provider)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errorResponseBody{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) MemoryPolicySet(w http.ResponseWriter, r *http.Request) {
	var req agent.MemoryPolicy
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "invalid json body"})
		return
	}
	h.agent.SetMemoryPolicy(req)
	h.MemoryGet(w, r)
}

func (h *Handler) MemoryClear(w http.ResponseWriter, r *http.Request) {
	layer := r.PathValue("layer")
	if layer == "" {
		var req struct {
			Layer string `json:"layer"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		layer = req.Layer
	}
	if err := h.agent.ClearLayer(layer); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: err.Error()})
		return
	}
	h.MemoryGet(w, r)
}

func (h *Handler) MemoryWrite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Layer string `json:"layer"`
		Text  string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Text == "" {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "layer and text are required"})
		return
	}
	layers := h.agent.Layers()
	if layers == nil {
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "layers not enabled"})
		return
	}
	var err error
	switch req.Layer {
	case memory.LayerWorking, "wm":
		err = layers.ApplyWorking(memory.WorkingPatch{Goal: req.Text, Status: memory.WorkingActive, AddNotes: []string{req.Text}})
	case memory.LayerLongTerm, "ltm":
		err = layers.ApplyLongTerm(memory.LongTermPatch{Knowledge: []memory.KnowledgeItem{{Topic: "manual", Fact: req.Text}}})
	default:
		writeJSON(w, http.StatusBadRequest, errorResponseBody{Error: "use layer=working or layer=long_term"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponseBody{Error: err.Error()})
		return
	}
	h.MemoryGet(w, r)
}

func (h *Handler) MemoryDemo(w http.ResponseWriter, r *http.Request) {
	var req providerOnlyBody
	_ = json.NewDecoder(r.Body).Decode(&req)
	result, err := h.agent.RunMemoryDemo(r.Context(), req.Provider)
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

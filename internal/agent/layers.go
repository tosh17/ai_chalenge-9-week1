package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/memory"
	"github.com/tosh17/deepseek-service/internal/tokens"
)

const memorySystemPrompt = `Ты агент с явной трёхслойной памятью и конечным автоматом задачи.
Используй слои так:
- ДОЛГОВРЕМЕННАЯ: профиль, решения, знания — считай их истиной между сессиями.
- РАБОЧАЯ: данные текущей задачи — цель, ограничения, черновики. Если слоя нет, задачи нет.
- СОСТОЯНИЕ ЗАДАЧИ: фаза строго по порядку планирование → выполнение → проверка → готово.
- КРАТКОСРОЧНАЯ: только недавний диалог; старые реплики могли быть вытеснены окном.
Работай только в текущей фазе. Не перескакивай. Этап ставит система по собранным данным, пользователь его не выбирает.
Если в слое нет факта — не выдумывай его из «общей эрудиции диалога». Отвечай на языке пользователя.`

const memoryRouterSystem = `Ты маршрутизатор памяти. Отвечай только валидным JSON-объектом.`

// MemoryPolicy — что агент кладёт в промпт и размер STM-окна.
type MemoryPolicy struct {
	STMWindowN    int  `json:"stm_window_n"`
	InjectSTM     bool `json:"inject_stm"`
	InjectWM      bool `json:"inject_wm"`
	InjectLTM     bool `json:"inject_ltm"`
	InjectProfile bool `json:"inject_profile"`
}

func defaultMemoryPolicy() MemoryPolicy {
	return MemoryPolicy{
		STMWindowN:    8,
		InjectSTM:     true,
		InjectWM:      true,
		InjectLTM:     true,
		InjectProfile: true,
	}
}

func normalizeMemoryPolicy(p MemoryPolicy) MemoryPolicy {
	if p.STMWindowN <= 0 {
		p.STMWindowN = 8
	}
	return p
}

// MemoryInfo — что ушло в промпт и состояние слоёв после хода.
type MemoryInfo struct {
	Policy            MemoryPolicy         `json:"policy"`
	ShortTermCount    int                  `json:"short_term_count"`
	ShortTermInPrompt int                  `json:"short_term_in_prompt"`
	Discarded         int                  `json:"discarded,omitempty"`
	WorkingActive     bool                 `json:"working_active"`
	Working           memory.WorkingState  `json:"working"`
	LongTerm          memory.LongTermState `json:"long_term"`
	Profile           memory.UserProfile   `json:"profile,omitempty"`
	ProfileInPrompt   bool                 `json:"profile_in_prompt,omitempty"`
	STMPreview        []deepseek.Message   `json:"stm_preview,omitempty"`
	Paths             map[string]string    `json:"paths,omitempty"`
}

// MemoryRouteEvent — явный выбор, что и в какой слой записали.
type MemoryRouteEvent struct {
	Kind             string                   `json:"kind"`   // "route"
	Source           string                   `json:"source"` // prefix | heuristic | llm
	Reasons          []string                 `json:"reasons"`
	WroteSTM         bool                     `json:"wrote_stm"`
	WroteWM          bool                     `json:"wrote_wm"`
	WroteLTM         bool                     `json:"wrote_ltm"`
	WroteProfile     bool                     `json:"wrote_profile,omitempty"`
	Working          *memory.WorkingPatch     `json:"working,omitempty"`
	LongTerm         *memory.LongTermPatch    `json:"long_term,omitempty"`
	Profile          *memory.UserProfilePatch `json:"profile,omitempty"`
	PromptTokens     int                      `json:"prompt_tokens,omitempty"`
	CompletionTokens int                      `json:"completion_tokens,omitempty"`
	TotalTokens      int                      `json:"total_tokens,omitempty"`
	CostUSD          float64                  `json:"cost_usd,omitempty"`
	DurationMs       int64                    `json:"duration_ms,omitempty"`
	System           string                   `json:"system,omitempty"`
	Prompt           string                   `json:"prompt,omitempty"`
	RawReply         string                   `json:"raw_reply,omitempty"`
	Error            string                   `json:"error,omitempty"`
}

type routeDecision struct {
	Working  *memory.WorkingPatch     `json:"working,omitempty"`
	LongTerm *memory.LongTermPatch    `json:"long_term,omitempty"`
	Profile  *memory.UserProfilePatch `json:"profile,omitempty"`
	Reasons  []string                 `json:"reasons"`
}

// WithLayers подключает трёхслойную память. При наличии layers Handle идёт по модели слоёв.
func (a *Agent) WithLayers(layers *memory.Layers) *Agent {
	a.layers = layers
	if a.memoryPolicy.STMWindowN == 0 {
		a.memoryPolicy = defaultMemoryPolicy()
	}
	return a
}

// Layers возвращает слои (может быть nil).
func (a *Agent) Layers() *memory.Layers {
	return a.layers
}

// WithMemoryPolicy задаёт окно STM и флаги подмешивания слоёв в промпт.
func (a *Agent) WithMemoryPolicy(p MemoryPolicy) *Agent {
	a.memoryPolicy = normalizeMemoryPolicy(p)
	return a
}

// MemoryPolicy возвращает текущую политику.
func (a *Agent) MemoryPolicy() MemoryPolicy {
	return a.memoryPolicy
}

// SetMemoryPolicy обновляет политику на лету.
func (a *Agent) SetMemoryPolicy(p MemoryPolicy) {
	a.memoryPolicy = normalizeMemoryPolicy(p)
}

// CloneWithLayers копирует бэкенды в нового агента с отдельными слоями памяти.
func (a *Agent) CloneWithLayers(name string, layers *memory.Layers) *Agent {
	clone := a.Clone(name)
	clone.memoryPolicy = a.memoryPolicy
	clone.layers = layers
	clone.profiles = a.profiles
	clone.systemPrompt = a.systemPrompt
	return clone
}

// WithProfiles подключает книгу персонализации.
func (a *Agent) WithProfiles(book *memory.ProfileBook) *Agent {
	a.profiles = book
	return a
}

// Profiles возвращает книгу профилей (может быть nil).
func (a *Agent) Profiles() *memory.ProfileBook {
	return a.profiles
}

func (a *Agent) handleWithLayers(ctx context.Context, req Request, providerID string, b backend) (Result, error) {
	policy := a.effectivePolicy(req)

	route, routeErr := a.routeToLayers(ctx, providerID, req.Message)
	if routeErr != nil {
		// роутинг не должен ронять чат: STM всё равно запишем после ответа
		route.Reasons = append(route.Reasons, "route_error: "+routeErr.Error())
	}

	messages, memInfo := a.buildMessagesLayers(req.Message, policy)
	limit := a.ContextLimit(providerID)
	usage := a.buildTokenUsage(messages, req.Message, b.model, nil, limit)
	session := a.SessionTotals()

	if usage.OverLimit {
		return Result{
			Provider:    providerID,
			Model:       b.model,
			Tokens:      &usage,
			Session:     &session,
			Memory:      &memInfo,
			RouteEvents: []MemoryRouteEvent{route},
		}, &ErrContextOverflow{Usage: usage}
	}

	start := time.Now()
	chat, err := b.llm.Chat(ctx, messages)
	duration := time.Since(start).Milliseconds()
	if err != nil {
		debug := chat.Debug
		return Result{
			Provider:    providerID,
			Model:       b.model,
			DurationMs:  duration,
			Tokens:      &usage,
			Session:     &session,
			Memory:      &memInfo,
			RouteEvents: []MemoryRouteEvent{route},
			Debug:       &debug,
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

	discarded := 0
	if a.layers != nil {
		n, saveErr := a.layers.AppendShortTerm(policy.STMWindowN,
			deepseek.Message{Role: "user", Content: req.Message},
			deepseek.Message{Role: "assistant", Content: chat.Reply},
		)
		if saveErr != nil {
			return Result{
				Reply:       chat.Reply,
				Provider:    providerID,
				Model:       b.model,
				DurationMs:  duration,
				Tokens:      &usage,
				Session:     &sess,
				Memory:      &memInfo,
				RouteEvents: []MemoryRouteEvent{route},
			}, fmt.Errorf("agent %q: save stm: %w", a.name, saveErr)
		}
		discarded = n
		route.WroteSTM = true
	}

	memInfo = a.memorySnapshot(policy)
	memInfo.Discarded = discarded
	memInfo.ShortTermInPrompt = countSTMInPrompt(messages)
	debug := chat.Debug
	return Result{
		Reply:       chat.Reply,
		Provider:    providerID,
		Model:       b.model,
		DurationMs:  duration,
		Tokens:      &usage,
		Session:     &sess,
		Memory:      &memInfo,
		RouteEvents: []MemoryRouteEvent{route},
		Debug:       &debug,
	}, nil
}

func (a *Agent) effectivePolicy(req Request) MemoryPolicy {
	p := a.memoryPolicy
	if p.STMWindowN <= 0 {
		p = defaultMemoryPolicy()
	}
	if req.InjectSTM != nil {
		p.InjectSTM = *req.InjectSTM
	}
	if req.InjectWM != nil {
		p.InjectWM = *req.InjectWM
	}
	if req.InjectLTM != nil {
		p.InjectLTM = *req.InjectLTM
	}
	if req.InjectProfile != nil {
		p.InjectProfile = *req.InjectProfile
	}
	return p
}

func (a *Agent) memorySnapshot(policy MemoryPolicy) MemoryInfo {
	info := MemoryInfo{Policy: policy}
	if a.layers == nil {
		return info
	}
	snap := a.layers.Snapshot()
	info.ShortTermCount = len(snap.ShortTerm)
	info.STMPreview = lastN(snap.ShortTerm, policy.STMWindowN)
	info.Working = snap.Working
	info.LongTerm = snap.LongTerm
	st := memory.NormalizeStage(snap.Working.Task.Stage)
	info.WorkingActive = snap.Working.Goal != "" && st != memory.StageIdle && st != memory.StageDone
	info.Paths = snap.Paths
	if a.profiles != nil {
		info.Profile = a.profiles.Active()
	}
	return info
}

func countSTMInPrompt(messages []deepseek.Message) int {
	n := 0
	for _, m := range messages {
		if m.Role == "user" || m.Role == "assistant" {
			n++
		}
	}
	return n
}

func (a *Agent) buildMessagesLayers(userMsg string, policy MemoryPolicy) ([]deepseek.Message, MemoryInfo) {
	info := a.memorySnapshot(policy)
	out := make([]deepseek.Message, 0, 8)
	system := a.systemPrompt
	if system == "" {
		system = memorySystemPrompt
	} else {
		system = system + "\n\n" + memorySystemPrompt
	}
	out = append(out, deepseek.Message{Role: "system", Content: system})

	if policy.InjectProfile && a.profiles != nil {
		prof := a.profiles.Active()
		info.Profile = prof
		info.ProfileInPrompt = true
		out = append(out, deepseek.Message{Role: "system", Content: memory.FormatProfileBlock(prof)})
	}

	if policy.InjectLTM && a.layers != nil {
		out = append(out, deepseek.Message{Role: "system", Content: formatLongTermBlock(info.LongTerm)})
	}
	if policy.InjectWM && a.layers != nil {
		out = append(out, deepseek.Message{Role: "system", Content: formatWorkingBlock(info.Working)})
		out = append(out, deepseek.Message{Role: "system", Content: memory.FormatTaskBlock(info.Working.Task)})
	}

	stmCount := 0
	if policy.InjectSTM && a.layers != nil {
		tail := lastN(a.layers.ShortTerm(), policy.STMWindowN)
		info.STMPreview = tail
		for _, m := range tail {
			if m.Role == "system" {
				continue
			}
			out = append(out, m)
			stmCount++
		}
	}
	out = append(out, deepseek.Message{Role: "user", Content: userMsg})
	info.ShortTermInPrompt = stmCount + 1
	return out, info
}

func formatLongTermBlock(lt memory.LongTermState) string {
	var b strings.Builder
	b.WriteString("=== ДОЛГОВРЕМЕННАЯ ПАМЯТЬ (профиль, решения, знания) ===\n")
	empty := lt.Profile.Name == "" && lt.Profile.Language == "" &&
		len(lt.Profile.Preferences) == 0 && len(lt.Decisions) == 0 && len(lt.Knowledge) == 0
	if empty {
		b.WriteString("Пусто. Устойчивых фактов о пользователе пока нет.\n")
		return b.String()
	}
	if lt.Profile.Name != "" {
		b.WriteString("Профиль.имя: ")
		b.WriteString(lt.Profile.Name)
		b.WriteByte('\n')
	}
	if lt.Profile.Language != "" {
		b.WriteString("Профиль.язык: ")
		b.WriteString(lt.Profile.Language)
		b.WriteByte('\n')
	}
	for k, v := range lt.Profile.Preferences {
		b.WriteString("Профиль.предпочтение.")
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(v)
		b.WriteByte('\n')
	}
	for _, d := range lt.Decisions {
		b.WriteString("Решение ")
		b.WriteString(d.Key)
		b.WriteString(": ")
		b.WriteString(d.Value)
		if d.Why != "" {
			b.WriteString(" (")
			b.WriteString(d.Why)
			b.WriteByte(')')
		}
		b.WriteByte('\n')
	}
	for _, k := range lt.Knowledge {
		b.WriteString("Знание [")
		b.WriteString(k.Topic)
		b.WriteString("]: ")
		b.WriteString(k.Fact)
		b.WriteByte('\n')
	}
	return b.String()
}

func formatWorkingBlock(w memory.WorkingState) string {
	var b strings.Builder
	b.WriteString("=== РАБОЧАЯ ПАМЯТЬ (текущая задача) ===\n")
	if w.Status == "" || w.Status == memory.WorkingIdle || (w.Goal == "" && len(w.Notes) == 0 && len(w.Artifacts) == 0) {
		b.WriteString("Нет активной задачи.\n")
		return b.String()
	}
	if w.TaskID != "" {
		b.WriteString("task_id: ")
		b.WriteString(w.TaskID)
		b.WriteByte('\n')
	}
	b.WriteString("статус: ")
	b.WriteString(w.Status)
	b.WriteByte('\n')
	if w.Goal != "" {
		b.WriteString("цель: ")
		b.WriteString(w.Goal)
		b.WriteByte('\n')
	}
	for _, c := range w.Constraints {
		b.WriteString("- ограничение: ")
		b.WriteString(c)
		b.WriteByte('\n')
	}
	for _, n := range w.Notes {
		b.WriteString("- заметка: ")
		b.WriteString(n)
		b.WriteByte('\n')
	}
	for k, v := range w.Artifacts {
		b.WriteString("- артефакт ")
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(v)
		b.WriteByte('\n')
	}
	return b.String()
}

func (a *Agent) routeToLayers(ctx context.Context, providerID, userMsg string) (MemoryRouteEvent, error) {
	ev := MemoryRouteEvent{Kind: "route", Reasons: []string{"STM: реплика текущего диалога (после ответа)"}}
	if a.layers == nil {
		return ev, nil
	}

	decision, source := parseExplicitRoute(userMsg)
	if source == "prefix" {
		ev.Source = source
		if err := a.applyRoute(decision, &ev); err != nil {
			return ev, err
		}
		a.autoAdvanceTask(userMsg, &ev)
		return ev, nil
	}

	heur, _ := heuristicRoute(userMsg)
	llmDec, llmEv, llmErr := a.llmRoute(ctx, providerID, userMsg)
	if llmErr == nil {
		ev = llmEv
		decision = mergeRoute(llmDec, heur)
		ev.Source = "llm"
		if heurFillsGaps(llmDec, heur) {
			ev.Source = "llm+heuristic"
		}
	} else {
		ev = llmEv
		decision = heur
		ev.Source = "heuristic"
		ev.Error = llmErr.Error()
		decision.Reasons = append(decision.Reasons, "llm_fallback: "+llmErr.Error())
	}
	if err := a.applyRoute(decision, &ev); err != nil {
		return ev, err
	}
	a.autoAdvanceTask(userMsg, &ev)
	return ev, nil
}

func (a *Agent) autoAdvanceTask(userMsg string, ev *MemoryRouteEvent) {
	if a.layers == nil {
		return
	}
	before := a.layers.Working().Task
	patch := memory.InferTaskPatch(a.layers.Working(), userMsg)
	if !workingPatchUseful(patch) {
		return
	}
	if err := a.layers.ApplyWorking(patch); err != nil {
		ev.Reasons = append(ev.Reasons, "автоэтап: "+err.Error())
		return
	}
	after := a.layers.Working().Task
	ev.WroteWM = true
	if ev.Working == nil {
		ev.Working = &patch
	}
	if before.Stage != after.Stage || before.Paused != after.Paused {
		ev.Reasons = append(ev.Reasons, "автоэтап: "+memory.StageTitle(before.Stage)+" → "+memory.StageTitle(after.Stage)+" · шаг «"+after.Step+"»")
	}
}

func heurFillsGaps(llm, h routeDecision) bool {
	llmWM := llm.Working != nil && workingPatchUseful(*llm.Working)
	llmLTM := llm.LongTerm != nil && longTermPatchUseful(*llm.LongTerm)
	llmP := llm.Profile != nil && llm.Profile.Useful()
	hWM := h.Working != nil && workingPatchUseful(*h.Working)
	hLTM := h.LongTerm != nil && longTermPatchUseful(*h.LongTerm)
	hP := h.Profile != nil && h.Profile.Useful()
	return (hWM && !llmWM) || (hLTM && !llmLTM) || (hP && !llmP)
}

func mergeRoute(llm, h routeDecision) routeDecision {
	out := llm
	if out.Working == nil || !workingPatchUseful(*out.Working) {
		out.Working = h.Working
	}
	if h.LongTerm != nil && longTermPatchUseful(*h.LongTerm) {
		out.LongTerm = mergeLongTermPatch(out.LongTerm, h.LongTerm)
	}
	if h.Profile != nil && h.Profile.Useful() {
		out.Profile = mergeProfilePatch(out.Profile, h.Profile)
	}
	out.Reasons = mergeReasons(out.Reasons, h.Reasons)
	return out
}

func mergeLongTermPatch(dst, src *memory.LongTermPatch) *memory.LongTermPatch {
	if src == nil {
		return dst
	}
	if dst == nil {
		cp := *src
		return &cp
	}
	if dst.ProfileName == "" {
		dst.ProfileName = src.ProfileName
	}
	if dst.Language == "" {
		dst.Language = src.Language
	}
	if len(dst.Preferences) == 0 && len(src.Preferences) > 0 {
		dst.Preferences = src.Preferences
	}
	if len(dst.Decisions) == 0 && len(src.Decisions) > 0 {
		dst.Decisions = src.Decisions
	}
	if len(dst.Knowledge) == 0 && len(src.Knowledge) > 0 {
		dst.Knowledge = src.Knowledge
	}
	return dst
}

func mergeProfilePatch(dst, src *memory.UserProfilePatch) *memory.UserProfilePatch {
	if src == nil || !src.Useful() {
		return dst
	}
	if dst == nil {
		cp := *src
		return &cp
	}
	if dst.Name == "" {
		dst.Name = src.Name
	}
	if dst.Role == "" {
		dst.Role = src.Role
	}
	if dst.Language == "" {
		dst.Language = src.Language
	}
	if dst.Style == "" {
		dst.Style = src.Style
	}
	if dst.Format == "" {
		dst.Format = src.Format
	}
	dst.AddConstraints = append(dst.AddConstraints, src.AddConstraints...)
	return dst
}

func (a *Agent) applyRoute(d routeDecision, ev *MemoryRouteEvent) error {
	if len(d.Reasons) > 0 {
		ev.Reasons = mergeReasons(ev.Reasons, d.Reasons)
	}
	if d.Working != nil && workingPatchUseful(*d.Working) {
		if err := a.layers.ApplyWorking(*d.Working); err != nil {
			return err
		}
		ev.WroteWM = true
		ev.Working = d.Working
	}
	if d.LongTerm != nil && longTermPatchUseful(*d.LongTerm) {
		if err := a.layers.ApplyLongTerm(*d.LongTerm); err != nil {
			return err
		}
		ev.WroteLTM = true
		ev.LongTerm = d.LongTerm
	}
	if d.Profile != nil && d.Profile.Useful() && a.profiles != nil {
		if err := a.profiles.PatchActive(*d.Profile); err != nil {
			return err
		}
		ev.WroteProfile = true
		ev.Profile = d.Profile
	}
	return nil
}

func mergeReasons(base, extra []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(base)+len(extra))
	for _, s := range append(base, extra...) {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func workingPatchUseful(p memory.WorkingPatch) bool {
	return p.Complete || p.Goal != "" || p.Status != "" || p.Event != "" || p.Stage != "" || p.Step != "" || p.Expect != "" ||
		p.StepIndex > 0 || len(p.AddDone) > 0 || len(p.Constraints) > 0 || len(p.AddNotes) > 0 || len(p.Artifacts) > 0
}

func longTermPatchUseful(p memory.LongTermPatch) bool {
	return p.ProfileName != "" || p.Language != "" || len(p.Preferences) > 0 || len(p.Decisions) > 0 || len(p.Knowledge) > 0
}

var (
	rePrefixWM         = regexp.MustCompile(`(?i)^#(?:wm|задача|task)\s+`)
	rePrefixLTM        = regexp.MustCompile(`(?i)^#(?:ltm|запомни|remember)\s+`)
	rePrefixProfile    = regexp.MustCompile(`(?i)^#профиль\s+`)
	rePrefixDec        = regexp.MustCompile(`(?i)^#решение\s+`)
	rePrefixKnow       = regexp.MustCompile(`(?i)^#знание\s+`)
	rePrefixStyle      = regexp.MustCompile(`(?i)^#стиль\s+`)
	rePrefixFormat     = regexp.MustCompile(`(?i)^#формат\s+`)
	rePrefixConstraint = regexp.MustCompile(`(?i)^#ограничение\s+`)
	rePrefixPause      = regexp.MustCompile(`(?i)^#(?:пауза|pause)(?:\s|$)`)
	rePrefixResume     = regexp.MustCompile(`(?i)^#(?:продолжи|продолжай|resume)(?:\s|$)`)
	rePrefixAdvance    = regexp.MustCompile(`(?i)^#(?:дальше|этап\+|advance)(?:\s|$)`)
	rePrefixStage      = regexp.MustCompile(`(?i)^#этап\s+`)
	rePrefixStep       = regexp.MustCompile(`(?i)^#шаг\s+`)
	rePrefixExpect     = regexp.MustCompile(`(?i)^#ожидаю\s+`)
	reName             = regexp.MustCompile(`(?i)(?:меня зовут|зови меня|моё имя|мое имя)\s+([A-Za-zА-Яа-яЁё-]{2,40})`)
	reNameYa           = regexp.MustCompile(`(?i)(?:^|[\s,.;!?])я\s+([A-Za-zА-Яа-яЁё-]{2,40})(?:$|[\s,.;!?])`)
	rePrefers          = regexp.MustCompile(`(?i)(?:предпочитаю|давай всегда|отвечай)\s+(.{3,80})`)
	reOccupation       = regexp.MustCompile(`(?i)занимаюсь\s+(.{3,80}?)(?:\.|$|\n|хочу)`)
	nameStopwords      = map[string]struct{}{
		"я": {}, "привет": {}, "хочу": {}, "хотел": {}, "буду": {}, "делаю": {},
		"работаю": {}, "занимаюсь": {}, "живу": {}, "люблю": {}, "просто": {},
		"очень": {}, "тут": {}, "здесь": {}, "сейчас": {}, "уже": {}, "бы": {},
		"не": {}, "в": {}, "на": {}, "к": {}, "и": {}, "а": {}, "это": {},
		"супер": {}, "сделать": {}, "свой": {}, "свойм": {},
	}
)

func parseExplicitRoute(msg string) (routeDecision, string) {
	trim := strings.TrimSpace(msg)
	switch {
	case rePrefixPause.MatchString(trim):
		return routeDecision{
			Working: &memory.WorkingPatch{Event: memory.TaskPause},
			Reasons: []string{"задача: явный префикс #пауза — стоп на текущем этапе"},
		}, "prefix"
	case rePrefixResume.MatchString(trim):
		return routeDecision{
			Working: &memory.WorkingPatch{Event: memory.TaskResume},
			Reasons: []string{"задача: явный префикс #продолжи — снять паузу без смены этапа"},
		}, "prefix"
	case rePrefixAdvance.MatchString(trim):
		return routeDecision{
			Working: &memory.WorkingPatch{Event: memory.TaskAdvance},
			Reasons: []string{"задача: явный префикс #дальше — следующий этап автомата"},
		}, "prefix"
	case rePrefixStage.MatchString(trim):
		body := strings.TrimSpace(rePrefixStage.ReplaceAllString(trim, ""))
		return routeDecision{
			Working: &memory.WorkingPatch{Event: memory.TaskSet, Stage: body},
			Reasons: []string{"задача: явный префикс #этап"},
		}, "prefix"
	case rePrefixStep.MatchString(trim):
		body := strings.TrimSpace(rePrefixStep.ReplaceAllString(trim, ""))
		return routeDecision{
			Working: &memory.WorkingPatch{Event: memory.TaskSet, Step: body},
			Reasons: []string{"задача: явный префикс #шаг"},
		}, "prefix"
	case rePrefixExpect.MatchString(trim):
		body := strings.TrimSpace(rePrefixExpect.ReplaceAllString(trim, ""))
		return routeDecision{
			Working: &memory.WorkingPatch{Event: memory.TaskSet, Expect: body},
			Reasons: []string{"задача: явный префикс #ожидаю"},
		}, "prefix"
	case rePrefixWM.MatchString(trim):
		body := strings.TrimSpace(rePrefixWM.ReplaceAllString(trim, ""))
		return routeDecision{
			Working: &memory.WorkingPatch{Goal: body, Status: memory.WorkingActive, AddNotes: []string{body}},
			Reasons: []string{"WM: явный префикс #задача — данные текущей задачи"},
		}, "prefix"
	case rePrefixLTM.MatchString(trim):
		body := strings.TrimSpace(rePrefixLTM.ReplaceAllString(trim, ""))
		return routeDecision{
			LongTerm: &memory.LongTermPatch{Knowledge: []memory.KnowledgeItem{{Topic: "note", Fact: body}}},
			Reasons:  []string{"LTM: явный префикс #запомни — устойчивое знание"},
		}, "prefix"
	case rePrefixProfile.MatchString(trim):
		body := strings.TrimSpace(rePrefixProfile.ReplaceAllString(trim, ""))
		return routeDecision{
			LongTerm: &memory.LongTermPatch{ProfileName: body},
			Profile:  &memory.UserProfilePatch{Name: body},
			Reasons:  []string{"LTM+профиль: явный префикс #профиль"},
		}, "prefix"
	case rePrefixStyle.MatchString(trim):
		body := strings.TrimSpace(rePrefixStyle.ReplaceAllString(trim, ""))
		return routeDecision{
			LongTerm: &memory.LongTermPatch{Preferences: map[string]string{"style": body}},
			Profile:  &memory.UserProfilePatch{Style: body},
			Reasons:  []string{"профиль: явный префикс #стиль"},
		}, "prefix"
	case rePrefixFormat.MatchString(trim):
		body := strings.TrimSpace(rePrefixFormat.ReplaceAllString(trim, ""))
		return routeDecision{
			LongTerm: &memory.LongTermPatch{Preferences: map[string]string{"format": body}},
			Profile:  &memory.UserProfilePatch{Format: body},
			Reasons:  []string{"профиль: явный префикс #формат"},
		}, "prefix"
	case rePrefixConstraint.MatchString(trim):
		body := strings.TrimSpace(rePrefixConstraint.ReplaceAllString(trim, ""))
		return routeDecision{
			Profile: &memory.UserProfilePatch{AddConstraints: []string{body}},
			Reasons: []string{"профиль: явный префикс #ограничение"},
		}, "prefix"
	case rePrefixDec.MatchString(trim):
		body := strings.TrimSpace(rePrefixDec.ReplaceAllString(trim, ""))
		key, val := splitKV(body)
		return routeDecision{
			LongTerm: &memory.LongTermPatch{Decisions: []memory.Decision{{Key: key, Value: val}}},
			Reasons:  []string{"LTM: явный префикс #решение"},
		}, "prefix"
	case rePrefixKnow.MatchString(trim):
		body := strings.TrimSpace(rePrefixKnow.ReplaceAllString(trim, ""))
		topic, fact := splitKV(body)
		return routeDecision{
			LongTerm: &memory.LongTermPatch{Knowledge: []memory.KnowledgeItem{{Topic: topic, Fact: fact}}},
			Reasons:  []string{"LTM: явный префикс #знание"},
		}, "prefix"
	}
	return routeDecision{}, ""
}

func splitKV(s string) (key, val string) {
	for _, sep := range []string{":", "=", "—", "-"} {
		if i := strings.Index(s, sep); i > 0 {
			key = strings.TrimSpace(s[:i])
			val = strings.TrimSpace(s[i+len(sep):])
			if key != "" && val != "" {
				return key, val
			}
		}
	}
	return "note", s
}

func heuristicRoute(msg string) (routeDecision, string) {
	d := routeDecision{Reasons: []string{"STM: реплика текущего диалога"}}
	low := strings.ToLower(msg)

	if name := extractPersonName(msg); name != "" {
		d.LongTerm = ensureLTM(d.LongTerm)
		d.LongTerm.ProfileName = name
		d.Profile = ensureProfile(d.Profile)
		d.Profile.Name = name
		d.Reasons = append(d.Reasons, "LTM: имя пользователя → профиль")
	}
	if strings.Contains(low, "предпочитаю") || strings.Contains(low, "отвечай кратко") || strings.Contains(low, "коротко") {
		d.LongTerm = ensureLTM(d.LongTerm)
		pref := "кратко"
		if m := rePrefers.FindStringSubmatch(msg); len(m) == 2 {
			pref = strings.TrimSpace(m[1])
		}
		d.LongTerm.Preferences = map[string]string{"style": pref}
		d.Profile = ensureProfile(d.Profile)
		d.Profile.Style = pref
		if strings.Contains(low, "списк") {
			d.Profile.Format = "маркированные списки"
		}
		d.Reasons = append(d.Reasons, "профиль: предпочтение стиля")
	}
	if strings.Contains(low, "формально") || strings.Contains(low, "на вы") {
		d.Profile = ensureProfile(d.Profile)
		if d.Profile.Style == "" {
			d.Profile.Style = "формально, на «вы»"
		}
		d.Reasons = append(d.Reasons, "профиль: формальный стиль")
	}
	if strings.Contains(low, "без воды") {
		d.Profile = ensureProfile(d.Profile)
		d.Profile.AddConstraints = append(d.Profile.AddConstraints, "без воды")
		d.Reasons = append(d.Reasons, "профиль: ограничение без воды")
	}
	if strings.Contains(low, "без эмодзи") || strings.Contains(low, "без emoji") {
		d.Profile = ensureProfile(d.Profile)
		d.Profile.AddConstraints = append(d.Profile.AddConstraints, "без эмодзи")
		d.Reasons = append(d.Reasons, "профиль: ограничение без эмодзи")
	}
	if strings.Contains(low, "запомни") || strings.Contains(low, "навсегда") || strings.Contains(low, "в профиль") {
		d.LongTerm = ensureLTM(d.LongTerm)
		d.LongTerm.Knowledge = append(d.LongTerm.Knowledge, memory.KnowledgeItem{Topic: "note", Fact: msg})
		d.Reasons = append(d.Reasons, "LTM: пользователь просит запомнить")
	}
	if strings.Contains(low, "решение:") || strings.Contains(low, "договорились") || strings.Contains(low, "договорённость") || strings.Contains(low, "договоренность") {
		d.LongTerm = ensureLTM(d.LongTerm)
		key, val := splitKV(msg)
		d.LongTerm.Decisions = append(d.LongTerm.Decisions, memory.Decision{Key: key, Value: val, Why: "из реплики"})
		d.Reasons = append(d.Reasons, "LTM: решение / договорённость")
	}
	if m := reOccupation.FindStringSubmatch(msg); len(m) == 2 {
		fact := strings.TrimSpace(m[1])
		if fact != "" {
			d.LongTerm = ensureLTM(d.LongTerm)
			d.LongTerm.Knowledge = append(d.LongTerm.Knowledge, memory.KnowledgeItem{Topic: "occupation", Fact: fact})
			d.Reasons = append(d.Reasons, "LTM: род занятий → знание")
		}
	}

	looksPause := strings.Contains(low, "на паузу") || strings.Contains(low, "поставь паузу") ||
		strings.HasPrefix(low, "пауза") || strings.HasPrefix(low, "подожди") ||
		strings.HasPrefix(low, "остановись") || strings.Contains(low, "сделаем паузу")
	if looksPause {
		d.Working = &memory.WorkingPatch{Event: memory.TaskPause, AddNotes: []string{"пауза: " + msg}}
		d.Reasons = append(d.Reasons, "задача: пауза на текущем этапе")
	}
	looksResume := strings.HasPrefix(low, "продолж") || strings.Contains(low, "снимай паузу") ||
		strings.Contains(low, "без повтора") || strings.Contains(low, "не повторяй") && strings.Contains(low, "дальше")
	if looksResume && !looksPause {
		d.Working = &memory.WorkingPatch{Event: memory.TaskResume, AddNotes: []string{"resume: " + msg}}
		d.Reasons = append(d.Reasons, "задача: продолжить с текущего шага")
	}

	looksTask := strings.Contains(low, "задача") || strings.Contains(low, "тз") ||
		strings.Contains(low, "цель") || strings.Contains(low, "бюджет") ||
		strings.Contains(low, "срок") || strings.Contains(low, "проект") ||
		strings.Contains(low, "ограничен") || strings.Contains(low, "mvp") ||
		strings.Contains(low, "хочу сделать") || strings.Contains(low, "хочу собрать") ||
		strings.Contains(low, "хочу чтобы") || strings.Contains(low, "планирую") ||
		strings.Contains(low, "давай сделаем")
	if looksTask {
		d.Working = &memory.WorkingPatch{Status: memory.WorkingActive, AddNotes: []string{msg}}
		if strings.Contains(low, "цель") || strings.Contains(low, "тз") || strings.Contains(low, "проект") ||
			strings.Contains(low, "задача") || strings.Contains(low, "хочу сделать") || strings.Contains(low, "хочу собрать") {
			d.Working.Goal = msg
		}
		if strings.Contains(low, "бюджет") || strings.Contains(low, "срок") || strings.Contains(low, "ограничен") {
			d.Working.Constraints = []string{msg}
		}
		d.Reasons = append(d.Reasons, "WM: данные текущей задачи")
	}
	return d, "heuristic"
}

func extractPersonName(msg string) string {
	if m := reName.FindStringSubmatch(msg); len(m) == 2 {
		return m[1]
	}
	if m := reNameYa.FindStringSubmatch(msg); len(m) == 2 {
		name := m[1]
		if _, stop := nameStopwords[strings.ToLower(name)]; stop {
			return ""
		}
		return name
	}
	return ""
}

func ensureLTM(p *memory.LongTermPatch) *memory.LongTermPatch {
	if p == nil {
		return &memory.LongTermPatch{}
	}
	return p
}

func ensureProfile(p *memory.UserProfilePatch) *memory.UserProfilePatch {
	if p == nil {
		return &memory.UserProfilePatch{}
	}
	return p
}

func buildMemoryRouterPrompt(userMsg string, wm memory.WorkingState, lt memory.LongTermState) string {
	wmJSON, _ := json.Marshal(wm)
	ltJSON, _ := json.Marshal(lt)
	var prompt strings.Builder
	prompt.WriteString("Разложи реплику пользователя по слоям памяти агента.\n")
	prompt.WriteString("Верни ТОЛЬКО JSON без markdown:\n")
	prompt.WriteString(`{"working":{"goal":"","status":"","event":"","stage":"","step":"","expect":"","add_done":[],"constraints":[],"add_notes":[],"artifacts":{},"complete":false},"long_term":{"profile_name":"","language":"","preferences":{},"decisions":[{"key":"","value":"","why":""}],"knowledge":[{"topic":"","fact":""}]},"reasons":["слой: почему"]}` + "\n")
	prompt.WriteString("Правила выбора слоя:\n")
	prompt.WriteString("- working: только данные ТЕКУЩЕЙ задачи (цель, ограничения, черновик, статус). Этап НЕ ставь: stage и event=advance/start оставь пустыми — автомат выставит фазу сам.\n")
	prompt.WriteString("- event только pause | resume. complete=true только при явном «закрываем / принимаю / всё сделано».\n")
	prompt.WriteString("- «пауза/подожди» → event=pause. «продолжи» → event=resume (этап и шаг не менять).\n")
	prompt.WriteString("- черновики и результаты клади в artifacts, не в stage.\n")
	prompt.WriteString("- working: только данные ТЕКУЩЕЙ задачи (цель, ограничения, черновик, статус). Не профиль.\n")
	prompt.WriteString("- long_term: профиль (имя, стиль, чем занимается), устойчивые решения, знания на другие задачи.\n")
	prompt.WriteString("- Пример: «Привет, я Антон, занимаюсь умным домом, хочу сделать дом умнее» → long_term.profile_name=Антон, knowledge occupation, working.goal=сделать умный дом.\n")
	prompt.WriteString("- «Я Имя» без слов «меня зовут» всё равно имя в профиль. «Хочу сделать X» — цель в working.\n")
	prompt.WriteString("- Пустые поля опускай или оставь пустыми. complete=true только если пользователь явно завершил задачу.\n")
	prompt.WriteString("- Реплика диалога в short_term пишется отдельно, её сюда не клади.\n")
	prompt.WriteString("Текущая working:\n")
	prompt.Write(wmJSON)
	prompt.WriteString("\nТекущая long_term:\n")
	prompt.Write(ltJSON)
	prompt.WriteString("\nРеплика:\n")
	prompt.WriteString(userMsg)
	return prompt.String()
}

func (a *Agent) llmRoute(ctx context.Context, providerID, userMsg string) (routeDecision, MemoryRouteEvent, error) {
	ev := MemoryRouteEvent{
		Kind:   "route",
		Source: "llm",
		System: memoryRouterSystem,
	}
	if a.layers != nil {
		ev.Prompt = buildMemoryRouterPrompt(userMsg, a.layers.Working(), a.layers.LongTerm())
	}

	res, err := a.Complete(ctx, providerID,
		memoryRouterSystem,
		[]deepseek.Message{{Role: "user", Content: ev.Prompt}},
	)
	ev.DurationMs = res.DurationMs
	if res.Tokens != nil {
		ev.PromptTokens = res.Tokens.Prompt
		ev.CompletionTokens = res.Tokens.Completion
		ev.TotalTokens = res.Tokens.Total
		ev.CostUSD = res.Tokens.CostUSD
		a.addSession(*res.Tokens)
	}
	if err != nil {
		ev.Error = err.Error()
		ev.RawReply = strings.TrimSpace(res.Reply)
		return routeDecision{}, ev, err
	}

	raw := strings.TrimSpace(res.Reply)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	ev.RawReply = raw

	var d routeDecision
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		ev.Error = fmt.Sprintf("route json: %v", err)
		return routeDecision{}, ev, fmt.Errorf("route json: %w (raw=%q)", err, truncateRunes(raw, 160))
	}
	d.Working = sanitizeWorking(d.Working)
	d.LongTerm = sanitizeLongTerm(d.LongTerm)
	return d, ev, nil
}

func sanitizeWorking(p *memory.WorkingPatch) *memory.WorkingPatch {
	if p == nil {
		return nil
	}
	ev := strings.ToLower(strings.TrimSpace(p.Event))
	switch ev {
	case memory.TaskPause, memory.TaskResume:
		p.Stage = ""
		p.Complete = false
	default:
		// Этап принадлежит автомату, не маршрутизатору.
		p.Event = ""
		p.Stage = ""
		p.Step = ""
		p.Expect = ""
		p.AddDone = nil
		p.Complete = false
	}
	if !workingPatchUseful(*p) {
		return nil
	}
	return p
}

func sanitizeLongTerm(p *memory.LongTermPatch) *memory.LongTermPatch {
	if p == nil {
		return nil
	}
	if !longTermPatchUseful(*p) {
		return nil
	}
	return p
}

// MemorySnapshot — состояние слоёв для API.
func (a *Agent) MemorySnapshot() MemoryInfo {
	return a.memorySnapshot(normalizeMemoryPolicy(a.memoryPolicy))
}

// ClearLayer очищает один слой или все.
func (a *Agent) ClearLayer(layer string) error {
	if a.layers == nil {
		return fmt.Errorf("agent %q: no layers", a.name)
	}
	switch strings.TrimSpace(strings.ToLower(layer)) {
	case memory.LayerShortTerm, "stm", "short", "dialogue":
		return a.layers.ClearShortTerm()
	case memory.LayerWorking, "wm", "task":
		return a.layers.ClearWorking()
	case memory.LayerLongTerm, "ltm", "profile":
		return a.layers.ClearLongTerm()
	case "all", "":
		a.mu.Lock()
		a.session = tokens.SessionTotals{}
		a.mu.Unlock()
		return a.layers.ClearAll()
	default:
		return fmt.Errorf("unknown layer %q", layer)
	}
}

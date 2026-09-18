package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tosh17/deepseek-service/internal/deepseek"
)

const (
	LayerShortTerm = "short_term"
	LayerWorking   = "working"
	LayerLongTerm  = "long_term"

	WorkingIdle   = "idle"
	WorkingActive = "active"
	WorkingDone   = "done"
)

// Profile — устойчивые сведения о пользователе.
type Profile struct {
	Name        string            `json:"name,omitempty"`
	Language    string            `json:"language,omitempty"`
	Preferences map[string]string `json:"preferences,omitempty"`
}

// Decision — решение, которое должно жить дольше одной задачи.
type Decision struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Why   string `json:"why,omitempty"`
}

// KnowledgeItem — переиспользуемый факт.
type KnowledgeItem struct {
	Topic string `json:"topic"`
	Fact  string `json:"fact"`
}

// WorkingState — данные текущей задачи (рабочая память).
type WorkingState struct {
	TaskID      string            `json:"task_id,omitempty"`
	Goal        string            `json:"goal,omitempty"`
	Status      string            `json:"status"`
	Constraints []string          `json:"constraints,omitempty"`
	Notes       []string          `json:"notes,omitempty"`
	Artifacts   map[string]string `json:"artifacts,omitempty"`
	Task        TaskState         `json:"task"`
	UpdatedAt   string            `json:"updated_at,omitempty"`
}

// LongTermState — профиль, решения, знания.
type LongTermState struct {
	Profile   Profile         `json:"profile"`
	Decisions []Decision      `json:"decisions,omitempty"`
	Knowledge []KnowledgeItem `json:"knowledge,omitempty"`
}

type shortTermFile struct {
	Messages []deepseek.Message `json:"messages"`
}

type workingFile struct {
	WorkingState
}

type longTermFile struct {
	LongTermState
}

// Layers — три независимых хранилища памяти агента.
type Layers struct {
	mu        sync.RWMutex
	dir       string
	stmPath   string
	wmPath    string
	ltmPath   string
	shortTerm []deepseek.Message
	working   WorkingState
	longTerm  LongTermState
}

// OpenLayers загружает три JSON-файла из каталога (или создаёт пустые слои).
func OpenLayers(dir string) (*Layers, error) {
	if dir == "" {
		return nil, fmt.Errorf("memory layers: dir is required")
	}
	l := &Layers{
		dir:     dir,
		stmPath: filepath.Join(dir, "short-term.json"),
		wmPath:  filepath.Join(dir, "working.json"),
		ltmPath: filepath.Join(dir, "long-term.json"),
		working: WorkingState{Status: WorkingIdle, Artifacts: map[string]string{}, Task: TaskState{Stage: StageIdle}},
		longTerm: LongTermState{
			Profile: Profile{Preferences: map[string]string{}},
		},
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("memory layers: mkdir: %w", err)
	}
	if err := l.loadAll(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *Layers) loadAll() error {
	if err := l.loadShortTerm(); err != nil {
		return err
	}
	if err := l.loadWorking(); err != nil {
		return err
	}
	return l.loadLongTerm()
}

func (l *Layers) loadShortTerm() error {
	var payload shortTermFile
	if err := readJSONFile(l.stmPath, &payload); err != nil {
		return err
	}
	l.shortTerm = filterDialog(payload.Messages)
	return nil
}

func (l *Layers) loadWorking() error {
	var payload workingFile
	if err := readJSONFile(l.wmPath, &payload); err != nil {
		return err
	}
	if payload.Status == "" && payload.Goal == "" && len(payload.Notes) == 0 && len(payload.Artifacts) == 0 && payload.Task.Empty() {
		l.working = WorkingState{Status: WorkingIdle, Artifacts: map[string]string{}, Task: TaskState{Stage: StageIdle}}
		return nil
	}
	if payload.Status == "" {
		payload.Status = WorkingIdle
	}
	if payload.Artifacts == nil {
		payload.Artifacts = map[string]string{}
	}
	payload.Task.Stage = NormalizeStage(payload.Task.Stage)
	if payload.Goal != "" && payload.Task.Stage == StageIdle {
		payload.Task.Stage = StagePlanning
		if payload.Task.Expect == "" {
			payload.Task.Expect = defaultExpect(StagePlanning)
		}
	}
	l.working = payload.WorkingState
	return nil
}

func (l *Layers) loadLongTerm() error {
	var payload longTermFile
	if err := readJSONFile(l.ltmPath, &payload); err != nil {
		return err
	}
	if payload.Profile.Preferences == nil {
		payload.Profile.Preferences = map[string]string{}
	}
	l.longTerm = payload.LongTermState
	return nil
}

func readJSONFile(path string, dest any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("memory layers: read %s: %w", path, err)
	}
	if len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("memory layers: parse %s: %w", path, err)
	}
	return nil
}

func writeJSONFile(path string, payload any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("memory layers: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("memory layers: marshal: %w", err)
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("memory layers: write temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("memory layers: rename: %w", err)
	}
	return nil
}

// Dir — каталог слоёв.
func (l *Layers) Dir() string {
	return l.dir
}

// Paths — отдельные файлы каждого слоя.
func (l *Layers) Paths() map[string]string {
	return map[string]string{
		LayerShortTerm: l.stmPath,
		LayerWorking:   l.wmPath,
		LayerLongTerm:  l.ltmPath,
	}
}

// ShortTerm — копия текущего диалога.
func (l *Layers) ShortTerm() []deepseek.Message {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]deepseek.Message, len(l.shortTerm))
	copy(out, l.shortTerm)
	return out
}

// ShortTermLen — число реплик в STM.
func (l *Layers) ShortTermLen() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.shortTerm)
}

// Working — снимок рабочей памяти.
func (l *Layers) Working() WorkingState {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return cloneWorking(l.working)
}

// LongTerm — снимок долговременной памяти.
func (l *Layers) LongTerm() LongTermState {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return cloneLongTerm(l.longTerm)
}

// Snapshot — все три слоя сразу (для API/UI).
func (l *Layers) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return Snapshot{
		ShortTerm: l.shortTermCopyLocked(),
		Working:   cloneWorking(l.working),
		LongTerm:  cloneLongTerm(l.longTerm),
		Paths:     l.Paths(),
	}
}

// Snapshot — публичный срез слоёв.
type Snapshot struct {
	ShortTerm []deepseek.Message `json:"short_term"`
	Working   WorkingState       `json:"working"`
	LongTerm  LongTermState      `json:"long_term"`
	Paths     map[string]string  `json:"paths,omitempty"`
}

func (l *Layers) shortTermCopyLocked() []deepseek.Message {
	out := make([]deepseek.Message, len(l.shortTerm))
	copy(out, l.shortTerm)
	return out
}

// AppendShortTerm пишет реплики только в краткосрочный слой и обрезает окно.
func (l *Layers) AppendShortTerm(windowN int, msgs ...deepseek.Message) (discarded int, err error) {
	clean := filterDialog(msgs)
	if len(clean) == 0 {
		return 0, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.shortTerm = append(l.shortTerm, clean...)
	if windowN > 0 && len(l.shortTerm) > windowN {
		discarded = len(l.shortTerm) - windowN
		kept := make([]deepseek.Message, windowN)
		copy(kept, l.shortTerm[len(l.shortTerm)-windowN:])
		l.shortTerm = kept
	}
	return discarded, writeJSONFile(l.stmPath, shortTermFile{Messages: l.shortTerm})
}

// ApplyWorking мержит патч в рабочую память. complete=true закрывает задачу (этап done).
func (l *Layers) ApplyWorking(patch WorkingPatch) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.working.Artifacts == nil {
		l.working.Artifacts = map[string]string{}
	}
	ev := strings.TrimSpace(patch.Event)
	if patch.Complete {
		ev = TaskFinish
	}
	started := false
	if patch.Goal != "" {
		l.working.Goal = patch.Goal
		if l.working.TaskID == "" {
			l.working.TaskID = "task-" + fmt.Sprintf("%d", time.Now().Unix())
		}
		st := NormalizeStage(l.working.Task.Stage)
		if st == StageIdle || st == StageDone {
			if ev == "" {
				ev = TaskStart
			}
			started = true
		}
	}
	if ev != "" || patch.Stage != "" || patch.Step != "" || patch.Expect != "" || patch.StepIndex > 0 || len(patch.AddDone) > 0 {
		next, err := ApplyTaskEvent(l.working.Task, ev, patch)
		if err != nil {
			return err
		}
		l.working.Task = next
		if started && l.working.Task.Stage == StageIdle {
			l.working.Task.Stage = StagePlanning
		}
	}
	if patch.Status != "" {
		l.working.Status = patch.Status
	} else {
		l.working.Status = statusFromTask(l.working.Task)
	}
	if len(patch.Constraints) > 0 {
		l.working.Constraints = appendUnique(l.working.Constraints, patch.Constraints...)
	}
	if len(patch.AddNotes) > 0 {
		l.working.Notes = appendUnique(l.working.Notes, patch.AddNotes...)
	}
	for k, v := range patch.Artifacts {
		if k == "" || v == "" {
			continue
		}
		l.working.Artifacts[k] = v
	}
	if l.working.Status == "" {
		l.working.Status = WorkingIdle
	}
	l.working.UpdatedAt = nowISO()
	return writeJSONFile(l.wmPath, workingFile{WorkingState: l.working})
}

func statusFromTask(t TaskState) string {
	switch NormalizeStage(t.Stage) {
	case StageDone:
		return WorkingDone
	case StageIdle:
		return WorkingIdle
	default:
		if t.Paused {
			return "paused"
		}
		return WorkingActive
	}
}

// ApplyTask — явное событие автомата (пауза, продолжение, смена этапа).
func (l *Layers) ApplyTask(ev string, patch WorkingPatch) error {
	patch.Event = ev
	return l.ApplyWorking(patch)
}

// WorkingPatch — явное обновление рабочей памяти и автомата задачи.
type WorkingPatch struct {
	Goal        string            `json:"goal,omitempty"`
	Status      string            `json:"status,omitempty"`
	Constraints []string          `json:"constraints,omitempty"`
	AddNotes    []string          `json:"add_notes,omitempty"`
	Artifacts   map[string]string `json:"artifacts,omitempty"`
	Complete    bool              `json:"complete,omitempty"`
	Event       string            `json:"event,omitempty"`
	Stage       string            `json:"stage,omitempty"`
	Step        string            `json:"step,omitempty"`
	StepIndex   int               `json:"step_index,omitempty"`
	Expect      string            `json:"expect,omitempty"`
	AddDone     []string          `json:"add_done,omitempty"`
}

// LongTermPatch — явное обновление долговременной памяти.
type LongTermPatch struct {
	ProfileName string            `json:"profile_name,omitempty"`
	Language    string            `json:"language,omitempty"`
	Preferences map[string]string `json:"preferences,omitempty"`
	Decisions   []Decision        `json:"decisions,omitempty"`
	Knowledge   []KnowledgeItem   `json:"knowledge,omitempty"`
}

// ApplyLongTerm мержит патч в LTM.
func (l *Layers) ApplyLongTerm(patch LongTermPatch) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.longTerm.Profile.Preferences == nil {
		l.longTerm.Profile.Preferences = map[string]string{}
	}
	if patch.ProfileName != "" {
		l.longTerm.Profile.Name = patch.ProfileName
	}
	if patch.Language != "" {
		l.longTerm.Profile.Language = patch.Language
	}
	for k, v := range patch.Preferences {
		if k == "" || v == "" {
			continue
		}
		l.longTerm.Profile.Preferences[k] = v
	}
	for _, d := range patch.Decisions {
		if d.Key == "" || d.Value == "" {
			continue
		}
		l.longTerm.Decisions = upsertDecision(l.longTerm.Decisions, d)
	}
	for _, k := range patch.Knowledge {
		if k.Topic == "" || k.Fact == "" {
			continue
		}
		l.longTerm.Knowledge = upsertKnowledge(l.longTerm.Knowledge, k)
	}
	return writeJSONFile(l.ltmPath, longTermFile{LongTermState: l.longTerm})
}

// ClearShortTerm очищает только диалог.
func (l *Layers) ClearShortTerm() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.shortTerm = nil
	return writeJSONFile(l.stmPath, shortTermFile{Messages: []deepseek.Message{}})
}

// ClearWorking очищает только задачу.
func (l *Layers) ClearWorking() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.working = WorkingState{Status: WorkingIdle, Artifacts: map[string]string{}, Task: TaskState{Stage: StageIdle}}
	return writeJSONFile(l.wmPath, workingFile{WorkingState: l.working})
}

// ClearLongTerm очищает только профиль/решения/знания.
func (l *Layers) ClearLongTerm() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.longTerm = LongTermState{Profile: Profile{Preferences: map[string]string{}}}
	return writeJSONFile(l.ltmPath, longTermFile{LongTermState: l.longTerm})
}

// ClearAll очищает все три слоя.
func (l *Layers) ClearAll() error {
	if err := l.ClearShortTerm(); err != nil {
		return err
	}
	if err := l.ClearWorking(); err != nil {
		return err
	}
	return l.ClearLongTerm()
}

func cloneWorking(in WorkingState) WorkingState {
	out := in
	if in.Constraints != nil {
		out.Constraints = append([]string{}, in.Constraints...)
	}
	if in.Notes != nil {
		out.Notes = append([]string{}, in.Notes...)
	}
	out.Artifacts = map[string]string{}
	for k, v := range in.Artifacts {
		out.Artifacts[k] = v
	}
	out.Task = cloneTask(in.Task)
	return out
}

func cloneLongTerm(in LongTermState) LongTermState {
	out := in
	out.Profile.Preferences = map[string]string{}
	for k, v := range in.Profile.Preferences {
		out.Profile.Preferences[k] = v
	}
	if in.Decisions != nil {
		out.Decisions = append([]Decision{}, in.Decisions...)
	}
	if in.Knowledge != nil {
		out.Knowledge = append([]KnowledgeItem{}, in.Knowledge...)
	}
	return out
}

func appendUnique(dst []string, add ...string) []string {
	seen := map[string]struct{}{}
	for _, s := range dst {
		seen[s] = struct{}{}
	}
	for _, s := range add {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		dst = append(dst, s)
	}
	return dst
}

func upsertDecision(dst []Decision, d Decision) []Decision {
	for i, prev := range dst {
		if prev.Key == d.Key {
			dst[i] = d
			return dst
		}
	}
	return append(dst, d)
}

func upsertKnowledge(dst []KnowledgeItem, k KnowledgeItem) []KnowledgeItem {
	for i, prev := range dst {
		if prev.Topic == k.Topic {
			dst[i] = k
			return dst
		}
	}
	return append(dst, k)
}

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}

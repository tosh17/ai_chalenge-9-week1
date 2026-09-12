package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/tosh17/deepseek-service/internal/deepseek"
)

// Branch — независимый хвост диалога после checkpoint.
type Branch struct {
	ID       string             `json:"id"`
	Title    string             `json:"title"`
	Messages []deepseek.Message `json:"messages"`
}

// Store хранит историю, summary (day9), facts и ветки (day10).
type Store struct {
	mu             sync.RWMutex
	path           string
	messages       []deepseek.Message
	summary        string
	summarizedUpTo int
	facts          map[string]string
	checkpointAt   int
	branches       map[string]*Branch
	activeBranch   string
}

type filePayload struct {
	Messages       []deepseek.Message   `json:"messages"`
	Summary        string               `json:"summary,omitempty"`
	SummarizedUpTo int                  `json:"summarized_up_to,omitempty"`
	Facts          map[string]string    `json:"facts,omitempty"`
	CheckpointAt   int                  `json:"checkpoint_at,omitempty"`
	Branches       map[string]*Branch   `json:"branches,omitempty"`
	ActiveBranch   string               `json:"active_branch,omitempty"`
}

// Open загружает историю из JSON-файла (или создаёт пустое хранилище).
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("memory: path is required")
	}
	s := &Store{path: path, facts: map[string]string{}, branches: map[string]*Branch{}}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("memory: read %s: %w", s.path, err)
	}
	if len(data) == 0 {
		return nil
	}

	var payload filePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		var msgs []deepseek.Message
		if err2 := json.Unmarshal(data, &msgs); err2 != nil {
			return fmt.Errorf("memory: parse %s: %w", s.path, err)
		}
		payload.Messages = msgs
	}

	s.messages = filterDialog(payload.Messages)
	s.summary = payload.Summary
	s.summarizedUpTo = payload.SummarizedUpTo
	s.facts = payload.Facts
	if s.facts == nil {
		s.facts = map[string]string{}
	}
	s.checkpointAt = payload.CheckpointAt
	s.branches = payload.Branches
	if s.branches == nil {
		s.branches = map[string]*Branch{}
	}
	for id, b := range s.branches {
		if b == nil {
			delete(s.branches, id)
			continue
		}
		if b.ID == "" {
			b.ID = id
		}
		b.Messages = filterDialog(b.Messages)
	}
	s.activeBranch = payload.ActiveBranch
	if s.summarizedUpTo < 0 {
		s.summarizedUpTo = 0
	}
	if s.summarizedUpTo > len(s.messages) {
		s.summarizedUpTo = len(s.messages)
	}
	if s.checkpointAt < 0 {
		s.checkpointAt = 0
	}
	if s.checkpointAt > len(s.messages) {
		s.checkpointAt = len(s.messages)
	}
	if s.activeBranch != "" {
		if _, ok := s.branches[s.activeBranch]; !ok {
			s.activeBranch = ""
		}
	}
	return nil
}

func filterDialog(in []deepseek.Message) []deepseek.Message {
	out := make([]deepseek.Message, 0, len(in))
	for _, m := range in {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		if m.Content == "" {
			continue
		}
		out = append(out, deepseek.Message{Role: m.Role, Content: m.Content})
	}
	return out
}

// Messages — видимая история активной ветки (trunk + branch tail).
func (s *Store) Messages() []deepseek.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.visibleLocked()
}

func (s *Store) visibleLocked() []deepseek.Message {
	if s.activeBranch == "" || len(s.branches) == 0 {
		out := make([]deepseek.Message, len(s.messages))
		copy(out, s.messages)
		return out
	}
	b := s.branches[s.activeBranch]
	if b == nil {
		out := make([]deepseek.Message, len(s.messages))
		copy(out, s.messages)
		return out
	}
	trunk := s.messages
	if s.checkpointAt >= 0 && s.checkpointAt <= len(s.messages) {
		trunk = s.messages[:s.checkpointAt]
	}
	out := make([]deepseek.Message, 0, len(trunk)+len(b.Messages))
	out = append(out, trunk...)
	out = append(out, b.Messages...)
	return out
}

// Len — число видимых сообщений.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.visibleLocked())
}

// Path — путь к JSON-файлу.
func (s *Store) Path() string {
	return s.path
}

// Summary — текущее сжатое содержание старой части диалога.
func (s *Store) Summary() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.summary
}

// SummarizedUpTo — сколько первых сообщений покрыто summary.
func (s *Store) SummarizedUpTo() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.summarizedUpTo
}

// SetSummary сохраняет summary и границу сжатия.
func (s *Store) SetSummary(summary string, upTo int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if upTo < 0 {
		upTo = 0
	}
	if upTo > len(s.messages) {
		upTo = len(s.messages)
	}
	s.summary = summary
	s.summarizedUpTo = upTo
	return s.persistLocked()
}

// RawTail возвращает сообщения, ещё не вошедшие в summary (для LLM-запроса).
func (s *Store) RawTail() []deepseek.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	msgs := s.visibleLocked()
	if s.summarizedUpTo >= len(msgs) {
		return nil
	}
	out := make([]deepseek.Message, len(msgs)-s.summarizedUpTo)
	copy(out, msgs[s.summarizedUpTo:])
	return out
}

// PendingForSummary — сообщения после summarizedUpTo, кроме последних keepLast.
func (s *Store) PendingForSummary(keepLast int) []deepseek.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	msgs := s.visibleLocked()
	if keepLast < 0 {
		keepLast = 0
	}
	end := len(msgs) - keepLast
	if end <= s.summarizedUpTo {
		return nil
	}
	out := make([]deepseek.Message, end-s.summarizedUpTo)
	copy(out, msgs[s.summarizedUpTo:end])
	return out
}

// Facts — копия key-value памяти.
func (s *Store) Facts() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.facts))
	for k, v := range s.facts {
		out[k] = v
	}
	return out
}

// SetFacts полностью заменяет facts.
func (s *Store) SetFacts(facts map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.facts = map[string]string{}
	for k, v := range facts {
		if k == "" || v == "" {
			continue
		}
		s.facts[k] = v
	}
	return s.persistLocked()
}

// MergeFacts обновляет/добавляет факты (пустые значения удаляют ключ).
func (s *Store) MergeFacts(patch map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.facts == nil {
		s.facts = map[string]string{}
	}
	for k, v := range patch {
		if k == "" {
			continue
		}
		if v == "" {
			delete(s.facts, k)
			continue
		}
		s.facts[k] = v
	}
	return s.persistLocked()
}

// TruncateKeepLast оставляет только последние n видимых сообщений (sliding).
// При активных ветках чистит trunk+ветки до простого линейного хвоста.
func (s *Store) TruncateKeepLast(n int) (discarded int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n <= 0 {
		return 0, nil
	}
	vis := s.visibleLocked()
	if len(vis) <= n {
		return 0, nil
	}
	discarded = len(vis) - n
	kept := make([]deepseek.Message, n)
	copy(kept, vis[len(vis)-n:])
	s.messages = kept
	s.summary = ""
	s.summarizedUpTo = 0
	s.checkpointAt = 0
	s.branches = map[string]*Branch{}
	s.activeBranch = ""
	return discarded, s.persistLocked()
}

// BranchInfo — снимок веток для API/UI.
type BranchInfo struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Messages     int    `json:"messages"`
	Active       bool   `json:"active"`
	CheckpointAt int    `json:"checkpoint_at"`
}

// BranchesSnapshot — список веток.
func (s *Store) BranchesSnapshot() []BranchInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.branches))
	for id := range s.branches {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]BranchInfo, 0, len(ids))
	for _, id := range ids {
		b := s.branches[id]
		if b == nil {
			continue
		}
		out = append(out, BranchInfo{
			ID:           b.ID,
			Title:        b.Title,
			Messages:     len(b.Messages),
			Active:       id == s.activeBranch,
			CheckpointAt: s.checkpointAt,
		})
	}
	return out
}

// ActiveBranchID — id активной ветки (пусто = линейный режим).
func (s *Store) ActiveBranchID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeBranch
}

// CheckpointAt — индекс сообщения, на котором сделан fork.
func (s *Store) CheckpointAt() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.checkpointAt
}

// HasBranches — есть ли независимые ветки.
func (s *Store) HasBranches() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.branches) > 0 && s.activeBranch != ""
}

// ForkTwo создаёт checkpoint и две ветки A/B от текущего конца диалога.
func (s *Store) ForkTwo(titleA, titleB string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	vis := s.visibleLocked()
	// Сжимаем видимую историю в trunk и ветвим от неё.
	s.messages = append([]deepseek.Message{}, vis...)
	s.checkpointAt = len(s.messages)
	if titleA == "" {
		titleA = "Ветка A"
	}
	if titleB == "" {
		titleB = "Ветка B"
	}
	s.branches = map[string]*Branch{
		"a": {ID: "a", Title: titleA, Messages: nil},
		"b": {ID: "b", Title: titleB, Messages: nil},
	}
	s.activeBranch = "a"
	s.summary = ""
	s.summarizedUpTo = 0
	return s.persistLocked()
}

// SwitchBranch переключает активную ветку.
func (s *Store) SwitchBranch(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.branches[id]; !ok {
		return fmt.Errorf("memory: unknown branch %q", id)
	}
	s.activeBranch = id
	return s.persistLocked()
}

// Append добавляет сообщения в активную ветку (или в линейную историю).
func (s *Store) Append(msgs ...deepseek.Message) error {
	clean := filterDialog(msgs)
	if len(clean) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeBranch != "" && len(s.branches) > 0 {
		b := s.branches[s.activeBranch]
		if b == nil {
			return fmt.Errorf("memory: active branch %q missing", s.activeBranch)
		}
		b.Messages = append(b.Messages, clean...)
		return s.persistLocked()
	}
	s.messages = append(s.messages, clean...)
	return s.persistLocked()
}

// Clear очищает историю, summary, facts и ветки.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = nil
	s.summary = ""
	s.summarizedUpTo = 0
	s.facts = map[string]string{}
	s.checkpointAt = 0
	s.branches = map[string]*Branch{}
	s.activeBranch = ""
	return s.persistLocked()
}

func (s *Store) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("memory: mkdir: %w", err)
	}

	payload := filePayload{
		Messages:       s.messages,
		Summary:        s.summary,
		SummarizedUpTo: s.summarizedUpTo,
		Facts:          s.facts,
		CheckpointAt:   s.checkpointAt,
		Branches:       s.branches,
		ActiveBranch:   s.activeBranch,
	}
	if payload.Messages == nil {
		payload.Messages = []deepseek.Message{}
	}
	if payload.Facts == nil {
		payload.Facts = map[string]string{}
	}
	if payload.Branches == nil {
		payload.Branches = map[string]*Branch{}
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("memory: marshal: %w", err)
	}
	data = append(data, '\n')

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("memory: write temp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("memory: rename: %w", err)
	}
	return nil
}

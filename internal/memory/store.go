// Package memory — устойчивое хранилище истории диалога агента (JSON).
package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/tosh17/deepseek-service/internal/deepseek"
)

// Store хранит полную историю + отдельно summary сжатой части.
type Store struct {
	mu             sync.RWMutex
	path           string
	messages       []deepseek.Message
	summary        string
	summarizedUpTo int // сколько первых сообщений уже вошло в summary
}

type filePayload struct {
	Messages       []deepseek.Message `json:"messages"`
	Summary        string             `json:"summary,omitempty"`
	SummarizedUpTo int                `json:"summarized_up_to,omitempty"`
}

// Open загружает историю из JSON-файла (или создаёт пустое хранилище).
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("memory: path is required")
	}
	s := &Store{path: path}
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
	if s.summarizedUpTo < 0 {
		s.summarizedUpTo = 0
	}
	if s.summarizedUpTo > len(s.messages) {
		s.summarizedUpTo = len(s.messages)
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

// Messages возвращает копию полной истории (для UI).
func (s *Store) Messages() []deepseek.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]deepseek.Message, len(s.messages))
	copy(out, s.messages)
	return out
}

// Len — число сохранённых сообщений.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.messages)
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
	if s.summarizedUpTo >= len(s.messages) {
		return nil
	}
	out := make([]deepseek.Message, len(s.messages)-s.summarizedUpTo)
	copy(out, s.messages[s.summarizedUpTo:])
	return out
}

// PendingForSummary — сообщения после summarizedUpTo, кроме последних keepLast.
// Их можно сжимать пачками.
func (s *Store) PendingForSummary(keepLast int) []deepseek.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if keepLast < 0 {
		keepLast = 0
	}
	end := len(s.messages) - keepLast
	if end <= s.summarizedUpTo {
		return nil
	}
	out := make([]deepseek.Message, end-s.summarizedUpTo)
	copy(out, s.messages[s.summarizedUpTo:end])
	return out
}

// Append добавляет сообщения и сразу пишет файл.
func (s *Store) Append(msgs ...deepseek.Message) error {
	clean := filterDialog(msgs)
	if len(clean) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, clean...)
	return s.persistLocked()
}

// Clear очищает историю и summary.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = nil
	s.summary = ""
	s.summarizedUpTo = 0
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
	}
	if payload.Messages == nil {
		payload.Messages = []deepseek.Message{}
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

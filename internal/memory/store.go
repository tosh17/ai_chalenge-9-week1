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

// Store хранит messages на диске и в памяти.
type Store struct {
	mu       sync.RWMutex
	path     string
	messages []deepseek.Message
}

type filePayload struct {
	Messages []deepseek.Message `json:"messages"`
}

// Open загружает историю из JSON-файла (или создаёт пустое хранилище).
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("memory: path is required")
	}
	s := &Store{path: path, messages: nil}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.messages = nil
			return nil
		}
		return fmt.Errorf("memory: read %s: %w", s.path, err)
	}
	if len(data) == 0 {
		s.messages = nil
		return nil
	}

	var payload filePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		// совместимость: голый массив messages
		var msgs []deepseek.Message
		if err2 := json.Unmarshal(data, &msgs); err2 != nil {
			return fmt.Errorf("memory: parse %s: %w", s.path, err)
		}
		payload.Messages = msgs
	}

	s.messages = filterDialog(payload.Messages)
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

// Messages возвращает копию текущей истории.
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

// Clear очищает историю на диске и в памяти.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = nil
	return s.persistLocked()
}

func (s *Store) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("memory: mkdir: %w", err)
	}

	payload := filePayload{Messages: s.messages}
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

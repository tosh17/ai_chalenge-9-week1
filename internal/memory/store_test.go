package memory_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/memory"
)

func TestPersistAcrossOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chat-history.json")

	s1, err := memory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s1.Append(
		deepseek.Message{Role: "user", Content: "секрет: синий попугай"},
		deepseek.Message{Role: "assistant", Content: "запомнил"},
	); err != nil {
		t.Fatal(err)
	}

	s2, err := memory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	msgs := s2.Messages()
	if len(msgs) != 2 {
		t.Fatalf("want 2 messages, got %d", len(msgs))
	}
	if msgs[0].Content != "секрет: синий попугай" {
		t.Fatalf("unexpected first message: %q", msgs[0].Content)
	}

	if err := s2.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file should still exist after clear: %v", err)
	}
	if s2.Len() != 0 {
		t.Fatalf("want empty after clear, got %d", s2.Len())
	}
}

package memory_test

import (
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
	if s2.Len() != 0 {
		t.Fatalf("want empty after clear, got %d", s2.Len())
	}
}

func TestSummaryAndPending(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hist.json")
	s, err := memory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		_ = s.Append(
			deepseek.Message{Role: "user", Content: "u"},
			deepseek.Message{Role: "assistant", Content: "a"},
		)
	}
	// 24 messages, keepLast=4 → pending = 20
	pending := s.PendingForSummary(4)
	if len(pending) != 20 {
		t.Fatalf("pending want 20, got %d", len(pending))
	}
	if err := s.SetSummary("кратко", 10); err != nil {
		t.Fatal(err)
	}
	if s.SummarizedUpTo() != 10 || s.Summary() != "кратко" {
		t.Fatalf("summary not saved")
	}
	if len(s.RawTail()) != 14 {
		t.Fatalf("raw tail want 14, got %d", len(s.RawTail()))
	}
	pending = s.PendingForSummary(4)
	if len(pending) != 10 {
		t.Fatalf("pending after summary want 10, got %d", len(pending))
	}

	s3, err := memory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if s3.Summary() != "кратко" || s3.SummarizedUpTo() != 10 {
		t.Fatalf("summary not persisted")
	}
}

package memory_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tosh17/deepseek-service/internal/deepseek"
	"github.com/tosh17/deepseek-service/internal/memory"
)

func TestLayersStoredSeparately(t *testing.T) {
	dir := t.TempDir()
	l, err := memory.OpenLayers(dir)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := l.AppendShortTerm(8,
		deepseek.Message{Role: "user", Content: "привет"},
		deepseek.Message{Role: "assistant", Content: "хай"},
	); err != nil {
		t.Fatal(err)
	}
	if err := l.ApplyWorking(memory.WorkingPatch{Goal: "ТЗ FoodDash", Status: memory.WorkingActive, Constraints: []string{"бюджет 2 млн"}}); err != nil {
		t.Fatal(err)
	}
	if err := l.ApplyLongTerm(memory.LongTermPatch{
		ProfileName: "Тоша",
		Preferences: map[string]string{"style": "кратко"},
		Decisions:   []memory.Decision{{Key: "stack", Value: "Go+Flutter"}},
		Knowledge:   []memory.KnowledgeItem{{Topic: "contact", Fact: "Анна"}},
	}); err != nil {
		t.Fatal(err)
	}

	paths := l.Paths()
	for _, p := range []string{paths[memory.LayerShortTerm], paths[memory.LayerWorking], paths[memory.LayerLongTerm]} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("layer file missing: %s: %v", p, err)
		}
	}

	if err := l.ClearShortTerm(); err != nil {
		t.Fatal(err)
	}
	if l.ShortTermLen() != 0 {
		t.Fatalf("STM should be empty")
	}
	if l.Working().Goal != "ТЗ FoodDash" {
		t.Fatalf("WM must survive STM clear: %+v", l.Working())
	}
	if l.LongTerm().Profile.Name != "Тоша" {
		t.Fatalf("LTM must survive STM clear: %+v", l.LongTerm())
	}

	if err := l.ClearWorking(); err != nil {
		t.Fatal(err)
	}
	if l.Working().Goal != "" {
		t.Fatalf("WM should be cleared")
	}
	if l.LongTerm().Profile.Name != "Тоша" {
		t.Fatalf("LTM must survive WM clear")
	}

	l2, err := memory.OpenLayers(dir)
	if err != nil {
		t.Fatal(err)
	}
	if l2.LongTerm().Profile.Name != "Тоша" || l2.LongTerm().Decisions[0].Value != "Go+Flutter" {
		t.Fatalf("LTM not persisted: %+v", l2.LongTerm())
	}
	if l2.ShortTermLen() != 0 {
		t.Fatalf("reopened STM want 0, got %d", l2.ShortTermLen())
	}
}

func TestSTMWindowDoesNotTouchOtherLayers(t *testing.T) {
	dir := t.TempDir()
	l, err := memory.OpenLayers(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = l.ApplyLongTerm(memory.LongTermPatch{ProfileName: "Тоша"})
	_ = l.ApplyWorking(memory.WorkingPatch{Goal: "keep me"})

	for i := 0; i < 10; i++ {
		_, err := l.AppendShortTerm(4,
			deepseek.Message{Role: "user", Content: "u"},
			deepseek.Message{Role: "assistant", Content: "a"},
		)
		if err != nil {
			t.Fatal(err)
		}
	}
	if l.ShortTermLen() != 4 {
		t.Fatalf("STM window want 4, got %d", l.ShortTermLen())
	}
	if l.LongTerm().Profile.Name != "Тоша" || l.Working().Goal != "keep me" {
		t.Fatalf("other layers mutated")
	}

	data, _ := os.ReadFile(filepath.Join(dir, "long-term.json"))
	if len(data) == 0 || !strings.Contains(string(data), "Тоша") {
		t.Fatalf("long-term.json should contain profile")
	}
}

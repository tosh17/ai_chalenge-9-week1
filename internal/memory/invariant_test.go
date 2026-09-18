package memory_test

import (
	"strings"
	"testing"

	"github.com/tosh17/deepseek-service/internal/memory"
)

func TestOpenInvariantBookSeedsDefaults(t *testing.T) {
	dir := t.TempDir()
	b, err := memory.OpenInvariantBook(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Enabled()) < 4 {
		t.Fatalf("want default invariants, got %d", len(b.List()))
	}
	b2, err := memory.OpenInvariantBook(dir)
	if err != nil {
		t.Fatal(err)
	}
	if b2.Path() != b.Path() || len(b2.List()) != len(b.List()) {
		t.Fatalf("reload mismatch")
	}
}

func TestFindConflictsDetectsStackRewrite(t *testing.T) {
	inv := memory.DefaultInvariants()
	hits := memory.FindConflicts(inv, "Давай перепишем бэкенд на Python и MongoDB")
	if len(hits) == 0 {
		t.Fatal("expected stack conflict")
	}
	joined := ""
	for _, h := range hits {
		joined += h.ID + " " + h.Kind + " "
	}
	if !strings.Contains(joined, "stack") {
		t.Fatalf("want stack invariant, got %s", joined)
	}

	ok := memory.FindConflicts(inv, "Как разложить handler и memory в этом Go-сервере?")
	if len(ok) != 0 {
		t.Fatalf("compliant question should not conflict: %+v", ok)
	}
}

func TestFormatInvariantBlockRequiresRefusal(t *testing.T) {
	blob := memory.FormatInvariantBlock(memory.DefaultInvariants())
	if !strings.Contains(blob, "ИНВАРИАНТЫ") || !strings.Contains(blob, "Стек Go") {
		t.Fatalf("block:\n%s", blob)
	}
	conflict := memory.FormatConflictBlock([]memory.InvariantConflict{{
		Title:  "Стек Go",
		Kind:   memory.InvariantStack,
		Rule:   "только Go",
		Reason: "запрос предлагает python",
	}})
	if !strings.Contains(conflict, "ОБЯЗАН отказать") || !strings.Contains(conflict, "Стек Go") {
		t.Fatalf("conflict block:\n%s", conflict)
	}
}

func TestInvariantUpsertAndDisable(t *testing.T) {
	b, err := memory.OpenInvariantBook(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	err = b.Upsert(memory.Invariant{
		ID:      "biz-test",
		Kind:    "бизнес",
		Title:   "Без скидок ниже себестоимости",
		Rule:    "Не предлагай цену ниже себестоимости.",
		Forbid:  []string{"ниже себестоимости", "в минус"},
		Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetEnabled("biz-test", false); err != nil {
		t.Fatal(err)
	}
	for _, it := range b.Enabled() {
		if it.ID == "biz-test" {
			t.Fatal("disabled invariant still enabled")
		}
	}
}

func TestForbidFromRuleExtractsTokens(t *testing.T) {
	got := memory.ForbidFromRule("только Go, без Python")
	joined := strings.ToLower(strings.Join(got, " "))
	if !strings.Contains(joined, "python") {
		t.Fatalf("want python in %v", got)
	}
}

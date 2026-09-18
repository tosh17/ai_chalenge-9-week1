package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenProfileBookSeedsDefaults(t *testing.T) {
	dir := t.TempDir()
	b, err := OpenProfileBook(dir)
	if err != nil {
		t.Fatal(err)
	}
	if b.ActiveID() != "brief" {
		t.Fatalf("active=%q", b.ActiveID())
	}
	if got := b.Active(); got.Name != "Антон" || got.Style == "" || len(got.Constraints) == 0 {
		t.Fatalf("brief profile: %+v", got)
	}
	if len(b.List()) < 3 {
		t.Fatalf("want 3 presets, got %d", len(b.List()))
	}
	if _, err := os.Stat(filepath.Join(dir, profilesFileName)); err != nil {
		t.Fatal(err)
	}

	b2, err := OpenProfileBook(dir)
	if err != nil {
		t.Fatal(err)
	}
	if b2.Active().Name != "Антон" {
		t.Fatalf("reload: %+v", b2.Active())
	}
}

func TestActivateAndPatchProfile(t *testing.T) {
	b, err := OpenProfileBook(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Activate("exec"); err != nil {
		t.Fatal(err)
	}
	if b.Active().Name != "Ирина" {
		t.Fatalf("exec: %+v", b.Active())
	}
	if err := b.PatchActive(UserProfilePatch{Style: "ещё короче", AddConstraints: []string{"только факты"}}); err != nil {
		t.Fatal(err)
	}
	p := b.Active()
	if p.Style != "ещё короче" {
		t.Fatalf("style=%q", p.Style)
	}
	found := false
	for _, c := range p.Constraints {
		if c == "только факты" {
			found = true
		}
	}
	if !found {
		t.Fatalf("constraint missing: %+v", p.Constraints)
	}
}

func TestFormatProfileBlockInstructsModel(t *testing.T) {
	p := DefaultProfiles()[0]
	blob := FormatProfileBlock(p)
	for _, need := range []string{"ПРОФИЛЬ ПОЛЬЗОВАТЕЛЯ", "Стиль ответа", "Формат", "ограничение", "Соблюдай стиль"} {
		if !strings.Contains(blob, need) {
			t.Fatalf("missing %q in:\n%s", need, blob)
		}
	}
	empty := FormatProfileBlock(UserProfile{})
	if !strings.Contains(empty, "не задан") {
		t.Fatalf("empty: %s", empty)
	}
}

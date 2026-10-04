package index

import "testing"

func TestSnapshotCoversAssignment(t *testing.T) {
	s := Snapshot()
	if s.Pages < 30 || s.Runes < 30000 {
		t.Fatalf("корпус слишком мал: %d стр., %d символов", s.Pages, s.Runes)
	}
	if s.Dim != 768 || s.Model == "" {
		t.Fatalf("эмбеддинги: %s %d", s.Model, s.Dim)
	}
	if s.Fixed.Chunks == 0 || s.Struct.Chunks == 0 {
		t.Fatal("пустая стратегия")
	}
	if s.Fixed.Chunks <= s.Struct.Chunks {
		t.Fatalf("фиксированных чанков %d, структурных %d", s.Fixed.Chunks, s.Struct.Chunks)
	}
	if s.HeadingsSplitFixed == 0 {
		t.Fatal("фиксированное окно не разрезало ни одной главы")
	}
	if len(s.Sections) < 7 {
		t.Fatalf("глав %d", len(s.Sections))
	}
	for _, side := range [][]Hit{s.FixedHits, s.StructHits} {
		if len(side) == 0 || side[0].ChunkID == "" || side[0].Section == "" || side[0].Preview == "" {
			t.Fatalf("пустой хит: %+v", side)
		}
	}
	if s.FixedHits[0].Preview == s.StructHits[0].Preview {
		t.Fatal("стратегии вернули один и тот же фрагмент")
	}
}

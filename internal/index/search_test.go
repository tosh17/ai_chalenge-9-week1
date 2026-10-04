package index

import "testing"

func TestTopChunksOrdersByCosine(t *testing.T) {
	chunks := []Chunk{
		{Section: "далеко", Embedding: []float32{0, 1}},
		{Section: "рядом", Embedding: []float32{1, 0}},
		{Section: "середина", Embedding: []float32{0.6, 0.4}},
	}
	got := topChunks(chunks, []float32{1, 0}, 2)
	if len(got) != 2 || got[0].Section != "рядом" || got[1].Section != "середина" {
		t.Fatalf("%+v", got)
	}
}

func TestMatchesKeepsTextBesideVector(t *testing.T) {
	s := &Searcher{chunks: []Chunk{
		{ChunkID: "a", Section: "глава А", Text: "про масло", Embedding: []float32{0, 1}},
		{ChunkID: "b", Section: "глава Б", Text: "про шины", Embedding: []float32{1, 0}},
	}}
	got := s.Matches([]float32{1, 0}, 1)
	if len(got) != 1 || got[0].ChunkID != "b" || got[0].Text != "про шины" {
		t.Fatalf("%+v", got)
	}
}

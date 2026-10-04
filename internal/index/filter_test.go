package index

import "testing"

func TestRerankPrefersTitleOverlap(t *testing.T) {
	hits := []Match{
		{Section: "Iced Tea", Score: 0.74},
		{Section: "Boiled Coffee", Score: 0.73},
	}
	got := Rerank(hits, "boiled coffee")
	if got[0].Section != "Boiled Coffee" {
		t.Fatalf("%s выше %s", got[0].Section, got[1].Section)
	}
	if got[0].Score <= 0.74 {
		t.Fatalf("заголовок не получил прибавку: %.3f", got[0].Score)
	}
}

func TestFilterDropsWeakHits(t *testing.T) {
	hits := []Match{
		{Section: "Boiled Coffee", Score: 0.89},
		{Section: "Iced Tea", Score: 0.74},
		{Section: "Filtered Coffee", Score: 0.80},
	}
	got := Filter(hits, MinScore, AnswerK)
	if len(got) != 2 || got[1].Section != "Filtered Coffee" {
		t.Fatalf("%+v", got)
	}
}

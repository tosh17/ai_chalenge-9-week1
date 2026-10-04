package index

import "testing"

func TestControlsCoverTenQuestions(t *testing.T) {
	got := Controls()
	if len(got) != 10 {
		t.Fatalf("вопросов %d", len(got))
	}
	for _, c := range got {
		if c.Question == "" || c.Expect == "" || c.Section == "" || len(c.Facts) == 0 {
			t.Fatalf("пустой контроль: %+v", c)
		}
	}
}

func TestSectionHitRejectsLongerNumber(t *testing.T) {
	hits := []Match{{Section: "Chocolate III"}, {Section: "Hollandaise Sauce II"}, {Section: "Milk and Water Bread"}}
	if SectionHit(hits, "Chocolate I") || SectionHit(hits, "Hollandaise Sauce I") || SectionHit(hits, "Water Bread") {
		t.Fatal("соседний рецепт не считается источником")
	}
	if !SectionHit([]Match{{Section: "Chocolate I"}}, "Chocolate I") {
		t.Fatal("точное имя должно совпасть")
	}
}

func TestFactsHitRequiresEveryGroup(t *testing.T) {
	groups := [][]string{{"3 минут", "три минут"}, {"яйц"}}
	if !FactsHit("Кипятить три минуты с яйцом", groups) {
		t.Fatal("должны найтись обе группы")
	}
	if FactsHit("Кипятить три минуты", groups) {
		t.Fatal("без яйца зачёт не ставится")
	}
}

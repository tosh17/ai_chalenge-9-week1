package index

import "testing"

func TestCitationsKeepChunkID(t *testing.T) {
	got := Citations([]Match{{
		ChunkID: "c12",
		Section: "Boiled Coffee",
		Text:    "1 cup coffee. Boil 3 minutes.",
		Score:   0.8,
	}})
	if len(got) != 1 || got[0].Source != BookFile || got[0].ChunkID != "c12" || got[0].Quote == "" {
		t.Fatalf("%+v", got)
	}
}

func TestGroundedAcceptsEnglishNumberWord(t *testing.T) {
	if !Grounded("настаивать пять минут, 3 ложки", "3 teaspoons. Let stand five minutes.") {
		t.Fatal("пять и 3 должны находиться в цитате")
	}
	if !Grounded("1. Ошпарьте чайник.\n2. Залейте 2 чашки.\n3. Настаивайте пять минут.", "2 cups. five minutes.") {
		t.Fatal("номера шагов не должны требовать цифры в цитате")
	}
	if !Grounded("по фрагменту [1] кипятить 3 минуты", "boil 3 minutes") {
		t.Fatal("номер цитаты в скобках не считается числом рецепта")
	}
	if Grounded("кипятить 9 минут", "boil 3 minutes") {
		t.Fatal("девятки в цитате нет")
	}
}

package index

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestStructKeepsSections(t *testing.T) {
	body := strings.Repeat("абвгд ", 30)
	doc := Document{
		ID:     "manual",
		Source: "manual.md",
		Title:  "manual",
		Text: Normalize(`# Эксплуатация
` + body + `
# Двигатель 2.0
` + body),
	}
	got := ChunkStruct(doc, 5000)
	if len(got) != 2 {
		t.Fatalf("чанков %d, хотели 2", len(got))
	}
	if got[0].Section != "Эксплуатация" || got[1].Section != "Двигатель 2.0" {
		t.Fatalf("секции: %q / %q", got[0].Section, got[1].Section)
	}
	if got[0].Strategy != "struct" || got[0].Source != "manual.md" || got[0].Title != "manual" {
		t.Fatalf("метаданные: %+v", got[0])
	}
	if !strings.HasPrefix(got[0].ChunkID, "struct:manual:") {
		t.Fatal(got[0].ChunkID)
	}
}

func TestFixedCutsAcrossHeading(t *testing.T) {
	doc := Document{
		ID:     "manual",
		Source: "manual.md",
		Title:  "manual",
		Text:   Normalize("# Эксплуатация\n" + strings.Repeat("а", 80) + "\n# Двигатель 2.0\n" + strings.Repeat("б", 80)),
	}
	got := ChunkFixed(doc, 100, 0)
	if len(got) < 2 {
		t.Fatalf("чанков %d", len(got))
	}
	split := headingsSplit(got)
	if split == 0 {
		t.Fatal("фиксированное окно не разрезало заголовок")
	}
	structed := ChunkStruct(doc, 5000)
	if headingsSplit(structed) != 0 {
		t.Fatal("структурная нарезка оставила заголовок в середине чанка")
	}
	for _, c := range got {
		if utf8.RuneCountInString(c.Text) > 100 {
			t.Fatalf("чанк длиннее окна: %d", utf8.RuneCountInString(c.Text))
		}
	}
}

func TestSkipsCrossReferences(t *testing.T) {
	for _, line := range []string{
		"глава 1).",
		"глава 1.",
		"1.5 dCi",
		"3.1.1 или более поздняя версия);",
		"7.14 – 7.15, 7.18, 7.22",
	} {
		if isHeading(line) {
			t.Fatalf("ложный заголовок: %q", line)
		}
	}
	for _, line := range []string{
		"Глава 1: Знакомство с автомобилем",
		"Глава 7. Аудиосистема",
	} {
		if !isHeading(line) {
			t.Fatalf("пропущен заголовок: %q", line)
		}
	}
}

func TestCookbookRecipeBecomesSection(t *testing.T) {
	doc := Document{
		ID:     "book",
		Source: "book.txt",
		Title:  "book",
		Text: Normalize(`
                              CHAPTER III
                               BEVERAGES

A beverage is any drink. Water is the beverage provided for man by Nature.

                             Boiled Coffee

                          1 cup coffee
                          1 egg
                          6 cups boiling water

Scald the pot and boil three minutes. Serve at once with sugar and cream.
`),
	}
	if !isHeading("CHAPTER III") {
		t.Fatal("глава поваренной книги не распознана")
	}
	got := ChunkStruct(doc, 5000)
	var titles []string
	for _, c := range got {
		titles = append(titles, c.Section)
	}
	joined := strings.Join(titles, " | ")
	if !strings.Contains(joined, "CHAPTER III") || !strings.Contains(joined, "Boiled Coffee") {
		t.Fatalf("секции: %s", joined)
	}
}

func TestPagesWhenNoHeadings(t *testing.T) {
	doc := Document{
		ID:     "pdf",
		Source: "a.pdf",
		Title:  "a",
		Text:   "первая страница текста достаточно длинная для чанка\fвторая страница текста тоже достаточно длинная",
	}
	got := ChunkStruct(doc, 5000)
	if len(got) != 2 {
		t.Fatalf("страниц-чанков %d", len(got))
	}
	if got[0].Section != "страница 1" || got[1].Section != "страница 2" {
		t.Fatalf("%q %q", got[0].Section, got[1].Section)
	}
}

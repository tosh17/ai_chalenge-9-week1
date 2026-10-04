package index

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	FixedSize    = 1000
	FixedOverlap = 150
	StructMax    = 1800
	minChunk     = 40
)

// Chunk — кусок документа с метаданными. Эмбеддинг заполняется отдельно.
type Chunk struct {
	ChunkID   string    `json:"chunk_id"`
	Source    string    `json:"source"`
	Title     string    `json:"title"`
	Section   string    `json:"section"`
	Strategy  string    `json:"strategy"`
	Text      string    `json:"text"`
	Embedding []float32 `json:"embedding,omitempty"`
}

// Document — уже извлечённый текст одного файла.
type Document struct {
	ID     string
	Source string
	Title  string
	Text   string
}

var (
	reMD          = regexp.MustCompile(`^#{1,6}\s+\S`)
	reChapter     = regexp.MustCompile(`^(Глава|Раздел|Chapter|Section)\s+\d+\s*[:.]\s*\p{L}`)
	reCookChapter = regexp.MustCompile(`(?i)^(chapter|глава|раздел)\s+([IVXLC]+|\d+)\s*$`)
	reIngredient  = regexp.MustCompile(`^(?:\d|½|¼|¾|⅓|⅔|⅛)`)
	reParenNote   = regexp.MustCompile(`^\(_.*_\)$`)
)

func isHeading(line string) bool {
	line = strings.TrimSpace(line)
	n := utf8.RuneCountInString(line)
	if n < 4 || n > 100 {
		return false
	}
	if strings.ContainsAny(line, "()") {
		return false
	}
	return reMD.MatchString(line) || reChapter.MatchString(line) || reCookChapter.MatchString(line)
}

func isRecipeHeading(lines []string, i int) bool {
	title := strings.TrimSpace(lines[i])
	if !recipeTitle(title) {
		return false
	}
	next := nextNonEmpty(lines, i)
	return reIngredient.MatchString(next) || reParenNote.MatchString(next)
}

func recipeTitle(s string) bool {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	if n < 3 || n > 48 {
		return false
	}
	if strings.ContainsAny(s, "0123456789") {
		return false
	}
	if strings.HasSuffix(s, ".") || strings.HasSuffix(s, ";") || strings.HasSuffix(s, ":") || strings.HasSuffix(s, ",") {
		return false
	}
	r, _ := utf8.DecodeRuneInString(s)
	if !unicode.IsUpper(r) {
		return false
	}
	if strings.HasPrefix(s, "[") || strings.HasPrefix(s, "(") || strings.HasPrefix(s, "_") {
		return false
	}
	if strings.HasPrefix(strings.ToLower(s), "few ") || strings.HasPrefix(strings.ToLower(s), "a few ") {
		return false
	}
	words := strings.Fields(s)
	if len(words) < 1 || len(words) > 8 {
		return false
	}
	if strings.ToUpper(s) == s && strings.Contains(s, ",") {
		return false
	}
	return true
}

func nextNonEmpty(lines []string, i int) string {
	for j := i + 1; j < len(lines) && j <= i+6; j++ {
		s := strings.TrimSpace(lines[j])
		if s != "" {
			return s
		}
	}
	return ""
}

// Normalize схлопывает пробелы PDF и сохраняет разрывы страниц.
func Normalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\f", "\n\f\n")
	var b strings.Builder
	blank := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) == "\f" {
			b.WriteString("\f\n")
			blank = 0
			continue
		}
		line = collapseSpaces(strings.TrimRight(line, " \t"))
		if strings.TrimSpace(line) == "" {
			blank++
			if blank <= 2 {
				b.WriteByte('\n')
			}
			continue
		}
		blank = 0
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

func collapseSpaces(line string) string {
	if strings.Contains(line, "\f") {
		return line
	}
	var b strings.Builder
	prev := false
	for _, r := range line {
		if r == ' ' || r == '\t' || r == '\u00a0' {
			if !prev {
				b.WriteByte(' ')
				prev = true
			}
			continue
		}
		prev = false
		b.WriteRune(r)
	}
	return b.String()
}

type heading struct {
	at    int
	title string
}

func headingOffsets(text string) []heading {
	var out []heading
	off := 0
	for _, line := range strings.Split(text, "\n") {
		if isHeading(line) {
			out = append(out, heading{at: off, title: strings.TrimSpace(strings.TrimLeft(line, "# "))})
		}
		off += utf8.RuneCountInString(line) + 1
	}
	return out
}

func sectionAt(heads []heading, runeAt int) string {
	title := ""
	for _, h := range heads {
		if h.at > runeAt {
			break
		}
		title = h.title
	}
	if title == "" {
		return "начало"
	}
	return title
}

// ChunkFixed режет текст окном фиксированной длины с перекрытием.
func ChunkFixed(doc Document, size, overlap int) []Chunk {
	if size <= 0 {
		size = FixedSize
	}
	if overlap < 0 || overlap >= size {
		overlap = 0
	}
	r := []rune(doc.Text)
	if len(r) == 0 {
		return nil
	}
	heads := headingOffsets(doc.Text)
	var out []Chunk
	for start := 0; start < len(r); {
		end := start + size
		if end > len(r) {
			end = len(r)
		}
		text := strings.TrimSpace(string(r[start:end]))
		if utf8.RuneCountInString(text) >= minChunk {
			out = append(out, Chunk{
				ChunkID:  fmt.Sprintf("fixed:%s:%03d", doc.ID, len(out)+1),
				Source:   doc.Source,
				Title:    doc.Title,
				Section:  sectionAt(heads, start),
				Strategy: "fixed",
				Text:     text,
			})
		}
		if end == len(r) {
			break
		}
		next := end - overlap
		if next <= start {
			next = end
		}
		start = next
	}
	return out
}

type part struct {
	Title string
	Body  string
}

// ChunkStruct режет по заголовкам, иначе по страницам, иначе по файлу.
func ChunkStruct(doc Document, max int) []Chunk {
	if max <= 0 {
		max = StructMax
	}
	parts := splitByStructure(doc.Text)
	var out []Chunk
	n := 0
	for _, p := range parts {
		for _, piece := range pack(p.Body, max) {
			text := strings.TrimSpace(piece)
			if utf8.RuneCountInString(text) < minChunk {
				continue
			}
			n++
			out = append(out, Chunk{
				ChunkID:  fmt.Sprintf("struct:%s:%03d", doc.ID, n),
				Source:   doc.Source,
				Title:    doc.Title,
				Section:  p.Title,
				Strategy: "struct",
				Text:     text,
			})
		}
	}
	return out
}

func splitByStructure(text string) []part {
	if countHeadings(text) >= 2 {
		return splitHeadings(text)
	}
	if strings.Contains(text, "\f") {
		pages := strings.Split(text, "\f")
		var parts []part
		for i, p := range pages {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			parts = append(parts, part{Title: fmt.Sprintf("страница %d", i+1), Body: p})
		}
		if len(parts) >= 2 {
			return parts
		}
	}
	body := strings.TrimSpace(text)
	if body == "" {
		return nil
	}
	return []part{{Title: "документ", Body: body}}
}

func sectionTitle(lines []string, i int) string {
	title := strings.TrimSpace(strings.TrimLeft(lines[i], "# "))
	if reCookChapter.MatchString(title) {
		next := nextNonEmpty(lines, i)
		if next != "" && utf8.RuneCountInString(next) <= 40 && !strings.Contains(next, ".") && !reIngredient.MatchString(next) {
			return title + ". " + next
		}
	}
	return title
}

func countHeadings(text string) int {
	lines := strings.Split(text, "\n")
	n := 0
	for i, line := range lines {
		if isHeading(line) || isRecipeHeading(lines, i) {
			n++
		}
	}
	return n
}

func splitHeadings(text string) []part {
	var parts []part
	var b strings.Builder
	title := "начало"
	flush := func() {
		body := strings.TrimSpace(b.String())
		if body != "" {
			parts = append(parts, part{Title: title, Body: body})
		}
		b.Reset()
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if isHeading(line) || isRecipeHeading(lines, i) {
			flush()
			title = sectionTitle(lines, i)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	flush()
	return parts
}

func pack(body string, max int) []string {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	if utf8.RuneCountInString(body) <= max {
		return []string{body}
	}
	paras := strings.Split(body, "\n\n")
	var out []string
	var cur strings.Builder
	curN := 0
	flush := func() {
		s := strings.TrimSpace(cur.String())
		if s != "" {
			out = append(out, s)
		}
		cur.Reset()
		curN = 0
	}
	for _, para := range paras {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		pn := utf8.RuneCountInString(para)
		if pn > max {
			flush()
			out = append(out, splitRunes(para, max)...)
			continue
		}
		if curN > 0 && curN+2+pn > max {
			flush()
		}
		if curN > 0 {
			cur.WriteString("\n\n")
			curN += 2
		}
		cur.WriteString(para)
		curN += pn
	}
	flush()
	return out
}

func splitRunes(s string, size int) []string {
	r := []rune(s)
	var out []string
	for start := 0; start < len(r); start += size {
		end := start + size
		if end > len(r) {
			end = len(r)
		}
		out = append(out, string(r[start:end]))
	}
	return out
}

package index

import (
	"regexp"
	"strings"
	"unicode"
)

// BookFile — файл корпуса, из которого взяты чанки.
const BookFile = "boston-cooking-school.txt"

// Citation — источник и дословный фрагмент чанка.
type Citation struct {
	Source  string  `json:"source"`
	Section string  `json:"section"`
	ChunkID string  `json:"chunk_id"`
	Quote   string  `json:"quote"`
	Score   float64 `json:"score"`
}

// Citations собирает источники и цитаты из уже отобранных чанков.
func Citations(hits []Match) []Citation {
	out := make([]Citation, 0, len(hits))
	for _, hit := range hits {
		quote := clipRunes(hit.Text, 900)
		if quote == "" {
			quote = hit.Preview
		}
		out = append(out, Citation{
			Source:  BookFile,
			Section: hit.Section,
			ChunkID: hit.ChunkID,
			Quote:   quote,
			Score:   hit.Score,
		})
	}
	return out
}

var (
	reAnswerNumber = regexp.MustCompile(`\d+(?:[.,]\d+)?|[½¼¾⅓⅔⅛]`)
	reCiteMark     = regexp.MustCompile(`\[\d+\]`)
	reStepNumber   = regexp.MustCompile(`(?:^|\n)\s*\d+\.(?:\s|$)`)
	wordNumbers    = []struct{ ru, en string }{
		{"пятнадцать", "fifteen"},
		{"двенадцать", "twelve"},
		{"полтор", "1 1/2"},
		{"тридцать", "thirty"},
		{"двадцать", "twenty"},
		{"шесть", "six"},
		{"пять", "five"},
		{"три", "three"},
		{"две", "two"},
		{"два", "two"},
	}
)

// Grounded — числа из ответа есть в цитатах, цифрой или английским словом.
func Grounded(answer, quotes string) bool {
	answer = strings.ToLower(reStepNumber.ReplaceAllString(reCiteMark.ReplaceAllString(answer, " "), " "))
	quotes = strings.ToLower(quotes)
	if strings.TrimSpace(answer) == "" || strings.TrimSpace(quotes) == "" {
		return false
	}
	found := false
	for _, raw := range reAnswerNumber.FindAllString(answer, -1) {
		found = true
		if !numberInQuotes(raw, quotes) {
			return false
		}
	}
	for _, pair := range wordNumbers {
		haystack := answer
		if pair.ru == "две" || pair.ru == "два" {
			haystack = strings.ReplaceAll(haystack, "двенадцат", "")
		}
		if strings.Contains(haystack, pair.ru) {
			found = true
			if !numberInQuotes(pair.en, quotes) && !strings.Contains(quotes, pair.ru) {
				return false
			}
		}
	}
	return found
}

func numberInQuotes(token, quotes string) bool {
	token = strings.ReplaceAll(token, ",", ".")
	switch token {
	case "½":
		return strings.Contains(quotes, "½") || strings.Contains(quotes, "1/2") || strings.Contains(quotes, "half")
	case "¼":
		return strings.Contains(quotes, "¼") || strings.Contains(quotes, "1/4")
	case "¾":
		return strings.Contains(quotes, "¾") || strings.Contains(quotes, "3/4")
	case "⅓":
		return strings.Contains(quotes, "⅓") || strings.Contains(quotes, "1/3")
	case "⅔":
		return strings.Contains(quotes, "⅔") || strings.Contains(quotes, "2/3")
	case "⅛":
		return strings.Contains(quotes, "⅛") || strings.Contains(quotes, "1/8")
	case "1 1/2", "1.5":
		return strings.Contains(quotes, "1½") || strings.Contains(quotes, "1 1/2") || strings.Contains(quotes, "1.5")
	}
	if strings.Contains(quotes, token) {
		return true
	}
	en := map[string]string{
		"1": "one", "2": "two", "3": "three", "4": "four", "5": "five",
		"6": "six", "8": "eight", "12": "twelve", "15": "fifteen",
		"20": "twenty", "30": "thirty",
	}
	if word, ok := en[token]; ok && containsWord(quotes, word) {
		return true
	}
	return false
}

func containsWord(text, word string) bool {
	i := strings.Index(text, word)
	if i < 0 {
		return false
	}
	if i > 0 && unicode.IsLetter(rune(text[i-1])) {
		return false
	}
	end := i + len(word)
	if end < len(text) && unicode.IsLetter(rune(text[end])) {
		return false
	}
	return true
}

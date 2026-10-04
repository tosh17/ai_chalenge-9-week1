package index

import (
	"sort"
	"strings"
	"unicode"
)

const (
	// RecallK — сколько чанков берём до фильтра.
	RecallK = 8
	// AnswerK — сколько чанков остаётся в ответе.
	AnswerK = 4
	// MinScore — порог после прибавки за совпадение заголовка.
	MinScore   = 0.75
	titleBoost = 0.08
)

// Rerank поднимает чанки, в заголовке которых есть слова запроса.
func Rerank(hits []Match, query string) []Match {
	words := queryWords(query)
	out := append([]Match(nil), hits...)
	for i := range out {
		title := strings.ToLower(out[i].Section)
		for _, word := range words {
			if strings.Contains(title, word) {
				out[i].Score += titleBoost
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Score > out[j].Score
	})
	return out
}

// Filter оставляет чанки не ниже порога и не больше k.
func Filter(hits []Match, minScore float64, k int) []Match {
	if k <= 0 {
		return nil
	}
	kept := make([]Match, 0, len(hits))
	for _, hit := range hits {
		if hit.Score < minScore {
			continue
		}
		kept = append(kept, hit)
		if len(kept) == k {
			break
		}
	}
	return kept
}

func queryWords(query string) []string {
	stop := map[string]bool{
		"with": true, "from": true, "that": true, "this": true,
		"make": true, "how": true, "what": true, "into": true,
	}
	var words []string
	seen := map[string]bool{}
	var b strings.Builder
	flush := func() {
		word := b.String()
		b.Reset()
		if len(word) < 4 || stop[word] || seen[word] {
			return
		}
		seen[word] = true
		words = append(words, word)
	}
	for _, r := range strings.ToLower(query) {
		if unicode.IsLetter(r) {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return words
}

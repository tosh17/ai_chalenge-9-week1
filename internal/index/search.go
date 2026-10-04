package index

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"unicode/utf8"
)

// Searcher ищет ближайшие чанки структурного индекса.
type Searcher struct {
	chunks []Chunk
	ollama *Ollama
}

// NewMemorySearcher ищет по уже загруженным чанкам, без файла и без Ollama.
func NewMemorySearcher(chunks []Chunk) *Searcher {
	return &Searcher{chunks: chunks}
}

// OpenSearcher читает JSON-индекс с эмбеддингами.
func OpenSearcher(path, ollamaURL, model string) (*Searcher, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var chunks []Chunk
	if err := json.Unmarshal(raw, &chunks); err != nil {
		return nil, err
	}
	kept := chunks[:0]
	for _, c := range chunks {
		if len(c.Embedding) > 0 && strings.TrimSpace(c.Text) != "" {
			kept = append(kept, c)
		}
	}
	if len(kept) == 0 {
		return nil, fmt.Errorf("в %s нет чанков с эмбеддингами", path)
	}
	return &Searcher{
		chunks: kept,
		ollama: &Ollama{BaseURL: ollamaURL, Model: model},
	}, nil
}

// Len — сколько чанков в индексе.
func (s *Searcher) Len() int {
	if s == nil {
		return 0
	}
	return len(s.chunks)
}

// Context возвращает несколько ближайших фрагментов для промпта.
func (s *Searcher) Context(ctx context.Context, query string) (string, error) {
	query = strings.TrimSpace(query)
	if s == nil || query == "" {
		return "", nil
	}
	vecs, err := s.ollama.embedOnce(ctx, []string{"search_query: " + query})
	if err != nil {
		return "", err
	}
	if len(vecs) == 0 || len(vecs[0]) == 0 {
		return "", fmt.Errorf("пустой вектор запроса")
	}
	hits := topChunks(s.chunks, vecs[0], 4)
	var b strings.Builder
	b.WriteString("Фрагменты поваренной книги. Отвечай только по ним.\n")
	for i, c := range hits {
		text := strings.TrimSpace(c.Text)
		if utf8.RuneCountInString(text) > 1200 {
			text = string([]rune(text)[:1200])
		}
		fmt.Fprintf(&b, "\n[%d] %s\n%s\n", i+1, c.Section, text)
	}
	return b.String(), nil
}

// Match — найденный чанк без вектора.
type Match struct {
	Score   float64 `json:"score"`
	ChunkID string  `json:"chunk_id"`
	Section string  `json:"section"`
	Preview string  `json:"preview"`
	Text    string  `json:"-"`
}

// QueryVector переводит вопрос в один вектор.
func (s *Searcher) QueryVector(ctx context.Context, query string) ([]float32, error) {
	query = strings.TrimSpace(query)
	if s == nil || s.ollama == nil || query == "" {
		return nil, fmt.Errorf("пустой запрос")
	}
	vecs, err := s.ollama.embedOnce(ctx, []string{"search_query: " + query})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 || len(vecs[0]) == 0 {
		return nil, fmt.Errorf("пустой вектор запроса")
	}
	return vecs[0], nil
}

// Matches возвращает k ближайших чанков к уже посчитанному вектору.
func (s *Searcher) Matches(vec []float32, k int) []Match {
	if s == nil || len(vec) == 0 || k <= 0 {
		return nil
	}
	hits := topChunks(s.chunks, vec, k)
	out := make([]Match, 0, len(hits))
	for _, c := range hits {
		text := clipRunes(c.Text, 1200)
		out = append(out, Match{
			Score:   cosine(vec, c.Embedding),
			ChunkID: c.ChunkID,
			Section: c.Section,
			Preview: clipRunes(text, 180),
			Text:    text,
		})
	}
	return out
}

// FormatMatches собирает фрагменты для промпта.
func FormatMatches(hits []Match) string {
	var b strings.Builder
	b.WriteString("Фрагменты поваренной книги. Отвечай только по ним.\n")
	for i, h := range hits {
		fmt.Fprintf(&b, "\n[%d] %s\n%s\n", i+1, h.Section, h.Text)
	}
	return b.String()
}

func clipRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	if n <= 0 || utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func topChunks(chunks []Chunk, query []float32, k int) []Chunk {
	type scored struct {
		c     Chunk
		score float64
	}
	best := make([]scored, 0, k)
	for _, c := range chunks {
		score := cosine(query, c.Embedding)
		if len(best) < k {
			best = append(best, scored{c, score})
			continue
		}
		worst := 0
		for i := 1; i < len(best); i++ {
			if best[i].score < best[worst].score {
				worst = i
			}
		}
		if score > best[worst].score {
			best[worst] = scored{c, score}
		}
	}
	for i := 0; i < len(best); i++ {
		for j := i + 1; j < len(best); j++ {
			if best[j].score > best[i].score {
				best[i], best[j] = best[j], best[i]
			}
		}
	}
	out := make([]Chunk, len(best))
	for i := range best {
		out[i] = best[i].c
	}
	return out
}

func cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		af := float64(a[i])
		bf := float64(b[i])
		dot += af * bf
		na += af * af
		nb += bf * bf
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

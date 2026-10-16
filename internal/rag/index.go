package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"
)

const cacheVersion = 1

// Embedder считает векторы. Пустой embedder оставляет лексический поиск.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// Hit — найденный фрагмент и его оценка.
type Hit struct {
	Path    string  `json:"path"`
	Heading string  `json:"heading"`
	Text    string  `json:"text"`
	Score   float64 `json:"score"`
}

// Status — состояние индекса для /api/help.
type Status struct {
	Ready      bool     `json:"ready"`
	Mode       string   `json:"mode"`
	Chunks     int      `json:"chunks"`
	Files      []string `json:"files"`
	Model      string   `json:"embed_model"`
	Ollama     string   `json:"ollama,omitempty"`
	TopK       int      `json:"top_k"`
	EmbedError string   `json:"embed_error,omitempty"`
}

// Options — откуда читать документацию и куда класть кэш векторов.
type Options struct {
	Root      string
	CachePath string
	Model     string
	OllamaURL string
	Embedder  Embedder
	TopK      int
}

// Index хранит чанки README и docs/.
type Index struct {
	mu         sync.Mutex
	root       string
	cachePath  string
	model      string
	ollamaURL  string
	embedder   Embedder
	topK       int
	chunks     []Chunk
	ready      bool
	mode       string
	embedError string
}

func New(opt Options) *Index {
	topK := opt.TopK
	if topK <= 0 {
		topK = 5
	}
	model := opt.Model
	if model == "" {
		model = "nomic-embed-text"
	}
	return &Index{
		root:      opt.Root,
		cachePath: opt.CachePath,
		model:     model,
		ollamaURL: opt.OllamaURL,
		embedder:  opt.Embedder,
		topK:      topK,
		mode:      "lexical",
	}
}

// Ensure строит индекс один раз. Повторный вызов ничего не пересчитывает.
func (idx *Index) Ensure(ctx context.Context) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if idx.ready {
		return nil
	}
	return idx.build(ctx)
}

// Search возвращает top-k фрагментов. Индекс строится при первом вызове.
func (idx *Index) Search(ctx context.Context, query string, k int) ([]Hit, error) {
	if err := idx.Ensure(ctx); err != nil {
		return nil, err
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if k <= 0 {
		k = idx.topK
	}
	query = strings.TrimSpace(query)
	if query == "" || len(idx.chunks) == 0 {
		return nil, nil
	}
	vector, err := idx.embedQuery(ctx, query)
	if err != nil || len(vector) == 0 {
		return lexicalSearch(idx.chunks, query, k), nil
	}
	hits := make([]Hit, 0, len(idx.chunks))
	for _, chunk := range idx.chunks {
		if len(chunk.Vector) == 0 {
			continue
		}
		score := cosine(vector, chunk.Vector)
		if score <= 0.05 {
			continue
		}
		hits = append(hits, Hit{
			Path:    chunk.Path,
			Heading: chunk.Heading,
			Text:    chunk.Text,
			Score:   score,
		})
	}
	if len(hits) == 0 {
		return lexicalSearch(idx.chunks, query, k), nil
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	return takeDiverse(hits, k), nil
}

func (idx *Index) Status() Status {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	files := make([]string, 0)
	seen := map[string]bool{}
	for _, chunk := range idx.chunks {
		if seen[chunk.Path] {
			continue
		}
		seen[chunk.Path] = true
		files = append(files, chunk.Path)
	}
	sort.Strings(files)
	return Status{
		Ready:      idx.ready,
		Mode:       idx.mode,
		Chunks:     len(idx.chunks),
		Files:      files,
		Model:      idx.model,
		Ollama:     idx.ollamaURL,
		TopK:       idx.topK,
		EmbedError: idx.embedError,
	}
}

func (idx *Index) build(ctx context.Context) error {
	docs, err := loadDocs(idx.root)
	if err != nil {
		return err
	}
	var chunks []Chunk
	for _, doc := range docs {
		chunks = append(chunks, ChunkMarkdown(doc.path, doc.body)...)
	}
	cached := idx.loadCache()
	missing := make([]int, 0)
	for i := range chunks {
		if vec, ok := cached[chunks[i].ID]; ok && len(vec) > 0 {
			chunks[i].Vector = vec
			continue
		}
		missing = append(missing, i)
	}
	if idx.embedder != nil && len(missing) > 0 {
		if err := idx.embedMissing(ctx, chunks, missing); err != nil {
			idx.embedError = err.Error()
		}
	}
	withVec := 0
	for _, chunk := range chunks {
		if len(chunk.Vector) > 0 {
			withVec++
		}
	}
	switch {
	case withVec == 0:
		idx.mode = "lexical"
	case withVec < len(chunks):
		idx.mode = "mixed"
	default:
		idx.mode = "embeddings"
	}
	idx.chunks = chunks
	idx.ready = true
	_ = idx.saveCache(chunks)
	return nil
}

func (idx *Index) embedMissing(ctx context.Context, chunks []Chunk, missing []int) error {
	const batch = 8
	var first error
	for start := 0; start < len(missing); start += batch {
		end := start + batch
		if end > len(missing) {
			end = len(missing)
		}
		part := missing[start:end]
		texts := make([]string, len(part))
		for i, at := range part {
			texts[i] = "search_document: " + chunks[at].Text
		}
		vectors, err := idx.embedder.Embed(ctx, texts)
		if err != nil || len(vectors) != len(texts) {
			if err == nil {
				err = fmt.Errorf("embedder returned %d vectors for %d texts", len(vectors), len(texts))
			}
			if first == nil {
				first = err
			}
			for _, at := range part {
				vec, oneErr := idx.embedder.Embed(ctx, []string{"search_document: " + chunks[at].Text})
				if oneErr != nil || len(vec) != 1 {
					continue
				}
				chunks[at].Vector = vec[0]
			}
			continue
		}
		for i, at := range part {
			chunks[at].Vector = vectors[i]
		}
	}
	return first
}

func (idx *Index) embedQuery(ctx context.Context, query string) ([]float32, error) {
	if idx.embedder == nil || idx.mode == "lexical" {
		return nil, nil
	}
	vectors, err := idx.embedder.Embed(ctx, []string{"search_query: " + query})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return nil, fmt.Errorf("empty query embedding")
	}
	return vectors[0], nil
}

type rawDoc struct {
	path string
	body string
}

func loadDocs(root string) ([]rawDoc, error) {
	if root == "" {
		return nil, fmt.Errorf("rag: root is required")
	}
	var docs []rawDoc
	readme := filepath.Join(root, "README.md")
	if body, err := os.ReadFile(readme); err == nil && strings.TrimSpace(string(body)) != "" {
		docs = append(docs, rawDoc{path: "README.md", body: string(body)})
	}
	docsDir := filepath.Join(root, "docs")
	err := filepath.WalkDir(docsDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil || strings.TrimSpace(string(body)) == "" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		docs = append(docs, rawDoc{path: filepath.ToSlash(rel), body: string(body)})
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].path < docs[j].path })
	if len(docs) == 0 {
		return nil, fmt.Errorf("rag: no markdown in %s", root)
	}
	return docs, nil
}

type cacheFile struct {
	Version int     `json:"version"`
	Model   string  `json:"model"`
	Chunks  []Chunk `json:"chunks"`
}

func (idx *Index) loadCache() map[string][]float32 {
	out := map[string][]float32{}
	if idx.cachePath == "" {
		return out
	}
	raw, err := os.ReadFile(idx.cachePath)
	if err != nil {
		return out
	}
	var file cacheFile
	if json.Unmarshal(raw, &file) != nil || file.Version != cacheVersion || file.Model != idx.model {
		return out
	}
	for _, chunk := range file.Chunks {
		if chunk.ID != "" && len(chunk.Vector) > 0 {
			out[chunk.ID] = chunk.Vector
		}
	}
	return out
}

func (idx *Index) saveCache(chunks []Chunk) error {
	if idx.cachePath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(idx.cachePath), 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(cacheFile{Version: cacheVersion, Model: idx.model, Chunks: chunks})
	if err != nil {
		return err
	}
	tmp := idx.cachePath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, idx.cachePath)
}

func cosine(a, b []float32) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	if n == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := 0; i < n; i++ {
		av, bv := float64(a[i]), float64(b[i])
		dot += av * bv
		na += av * av
		nb += bv * bv
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func lexicalSearch(chunks []Chunk, query string, k int) []Hit {
	terms := tokenize(query)
	if len(terms) == 0 {
		return nil
	}
	hits := make([]Hit, 0, len(chunks))
	for _, chunk := range chunks {
		score := lexicalScore(terms, chunk.Heading+" "+chunk.Text)
		if score <= 0 {
			continue
		}
		hits = append(hits, Hit{
			Path:    chunk.Path,
			Heading: chunk.Heading,
			Text:    chunk.Text,
			Score:   score,
		})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	return takeDiverse(hits, k)
}

// takeDiverse не даёт одному файлу занять весь ответ: сначала не больше двух чанков с пути.
func takeDiverse(hits []Hit, k int) []Hit {
	if k <= 0 || len(hits) <= 1 {
		return hits
	}
	picked := make([]Hit, 0, k)
	used := make([]bool, len(hits))
	perPath := map[string]int{}
	for i, hit := range hits {
		if perPath[hit.Path] >= 2 {
			continue
		}
		perPath[hit.Path]++
		picked = append(picked, hit)
		used[i] = true
		if len(picked) == k {
			return picked
		}
	}
	for i, hit := range hits {
		if used[i] {
			continue
		}
		picked = append(picked, hit)
		if len(picked) == k {
			break
		}
	}
	return picked
}

func lexicalScore(terms []string, text string) float64 {
	body := map[string]int{}
	for _, tok := range tokenize(text) {
		body[tok]++
	}
	if len(body) == 0 {
		return 0
	}
	hit := 0
	for _, term := range terms {
		if body[term] > 0 {
			hit++
		}
	}
	return float64(hit) / float64(len(terms))
}

func tokenize(s string) []string {
	s = strings.ToLower(s)
	var b strings.Builder
	var out []string
	flush := func() {
		tok := b.String()
		b.Reset()
		if len([]rune(tok)) < 3 || stopword(tok) {
			return
		}
		out = append(out, tok)
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}

func stopword(s string) bool {
	switch s {
	case "для", "это", "как", "что", "или", "при", "the", "and", "над", "под", "его", "она", "они", "все", "про":
		return true
	default:
		return false
	}
}

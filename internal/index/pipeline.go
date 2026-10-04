package index

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Options — вход пайплайна индексации.
type Options struct {
	Corpus   string
	OutDir   string
	Ollama   string
	Model    string
	Batch    int
	Embedder Embedder
}

// Report — сравнение двух стратегий на одном корпусе.
type Report struct {
	Files              int          `json:"files"`
	Runes              int          `json:"runes"`
	PagesEstimate      int          `json:"pages_estimate"`
	Fixed              StrategyStat `json:"fixed"`
	Struct             StrategyStat `json:"struct"`
	HeadingsSplitFixed int          `json:"headings_split_by_fixed"`
	FilesUsed          []string     `json:"files_used"`
}

// StrategyStat — размеры чанков одной стратегии.
type StrategyStat struct {
	Chunks   int `json:"chunks"`
	AvgRunes int `json:"avg_runes"`
	MinRunes int `json:"min_runes"`
	MaxRunes int `json:"max_runes"`
	Sections int `json:"sections"`
}

// Run читает корпус, режет двумя способами, считает эмбеддинги и пишет JSON.
func Run(ctx context.Context, opt Options) (Report, error) {
	if opt.Batch <= 0 {
		opt.Batch = 8
	}
	docs, err := LoadDir(opt.Corpus)
	if err != nil {
		return Report{}, err
	}
	if len(docs) == 0 {
		return Report{}, fmt.Errorf("в %s нет pdf, txt, md или go", opt.Corpus)
	}
	var fixed, structured []Chunk
	runes := 0
	names := make([]string, 0, len(docs))
	for _, doc := range docs {
		names = append(names, doc.Source)
		runes += utf8.RuneCountInString(doc.Text)
		fixed = append(fixed, ChunkFixed(doc, FixedSize, FixedOverlap)...)
		structured = append(structured, ChunkStruct(doc, StructMax)...)
	}
	emb := opt.Embedder
	if emb == nil {
		emb = &Ollama{BaseURL: opt.Ollama, Model: opt.Model}
	}
	if err := fillEmbeddings(ctx, emb, opt.Batch, fixed); err != nil {
		return Report{}, fmt.Errorf("эмбеддинги fixed: %w", err)
	}
	if err := fillEmbeddings(ctx, emb, opt.Batch, structured); err != nil {
		return Report{}, fmt.Errorf("эмбеддинги struct: %w", err)
	}
	if err := os.MkdirAll(opt.OutDir, 0o755); err != nil {
		return Report{}, err
	}
	if err := writeJSON(filepath.Join(opt.OutDir, "fixed.json"), fixed); err != nil {
		return Report{}, err
	}
	if err := writeJSON(filepath.Join(opt.OutDir, "struct.json"), structured); err != nil {
		return Report{}, err
	}
	report := buildReport(docs, names, runes, fixed, structured)
	if err := writeJSON(filepath.Join(opt.OutDir, "compare.json"), report); err != nil {
		return Report{}, err
	}
	return report, nil
}

func fillEmbeddings(ctx context.Context, emb Embedder, batch int, chunks []Chunk) error {
	for i := 0; i < len(chunks); i += batch {
		end := i + batch
		if end > len(chunks) {
			end = len(chunks)
		}
		texts := make([]string, 0, end-i)
		for _, c := range chunks[i:end] {
			texts = append(texts, c.Text)
		}
		vecs, err := emb.Embed(ctx, texts)
		if err != nil {
			return err
		}
		for j := range vecs {
			chunks[i+j].Embedding = vecs[j]
		}
	}
	return nil
}

// LoadReport читает compare.json, который пишет Run.
func LoadReport(path string) (Report, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Report{}, err
	}
	var report Report
	if err := json.Unmarshal(raw, &report); err != nil {
		return Report{}, err
	}
	return report, nil
}

func writeJSON(path string, v any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func buildReport(docs []Document, names []string, runes int, fixed, structured []Chunk) Report {
	pages := runes / 1800
	if pages < 1 && runes > 0 {
		pages = 1
	}
	return Report{
		Files:              len(docs),
		Runes:              runes,
		PagesEstimate:      pages,
		Fixed:              stat(fixed),
		Struct:             stat(structured),
		HeadingsSplitFixed: headingsSplit(fixed),
		FilesUsed:          names,
	}
}

func stat(chunks []Chunk) StrategyStat {
	if len(chunks) == 0 {
		return StrategyStat{}
	}
	sum, min, max := 0, 0, 0
	sections := map[string]struct{}{}
	for i, c := range chunks {
		n := utf8.RuneCountInString(c.Text)
		sum += n
		if i == 0 || n < min {
			min = n
		}
		if n > max {
			max = n
		}
		sections[c.Source+"\x00"+c.Section] = struct{}{}
	}
	return StrategyStat{
		Chunks:   len(chunks),
		AvgRunes: sum / len(chunks),
		MinRunes: min,
		MaxRunes: max,
		Sections: len(sections),
	}
}

// headingsSplit считает чанки, внутри которых заголовок стоит не в первой строке.
func headingsSplit(chunks []Chunk) int {
	n := 0
	for _, c := range chunks {
		lines := strings.Split(c.Text, "\n")
		for i, line := range lines {
			if i == 0 {
				continue
			}
			if isHeading(line) || isRecipeHeading(lines, i) {
				n++
				break
			}
		}
	}
	return n
}

// LoadDir собирает документы из каталога.
func LoadDir(dir string) ([]Document, error) {
	var docs []Document
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".pdf", ".txt", ".md", ".markdown", ".go":
		default:
			return nil
		}
		doc, err := LoadFile(dir, path)
		if err != nil {
			return err
		}
		if strings.TrimSpace(doc.Text) == "" {
			return nil
		}
		docs = append(docs, doc)
		return nil
	})
	return docs, err
}

// LoadFile извлекает текст. PDF через pdftotext.
func LoadFile(root, path string) (Document, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	var raw []byte
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".pdf" {
		cmd := exec.Command("pdftotext", "-enc", "UTF-8", "-q", path, "-")
		out, err := cmd.Output()
		if err != nil {
			return Document{}, fmt.Errorf("pdftotext %s: %w", rel, err)
		}
		raw = out
	} else {
		raw, err = os.ReadFile(path)
		if err != nil {
			return Document{}, err
		}
	}
	text := Normalize(string(raw))
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	id := sanitizeID(base)
	return Document{
		ID:     id,
		Source: filepath.ToSlash(rel),
		Title:  base,
		Text:   text,
	}, nil
}

func sanitizeID(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'а' && r <= 'я', r == 'ё':
			b.WriteRune(r)
		default:
			if b.Len() > 0 {
				last := b.String()
				if !strings.HasSuffix(last, "-") {
					b.WriteByte('-')
				}
			}
		}
	}
	id := strings.Trim(b.String(), "-")
	if id == "" {
		return "doc"
	}
	return id
}

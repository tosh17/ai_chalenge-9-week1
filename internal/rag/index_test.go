package rag

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type bagEmbedder struct{}

func (bagEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		low := strings.ToLower(text)
		vec := make([]float32, 4)
		if strings.Contains(low, "api") || strings.Contains(low, "/api/") {
			vec[0] = 1
		}
		if strings.Contains(low, "памят") || strings.Contains(low, "memory") {
			vec[1] = 1
		}
		if strings.Contains(low, "ветк") || strings.Contains(low, "git") {
			vec[2] = 1
		}
		if strings.Contains(low, "структур") || strings.Contains(low, "каталог") {
			vec[3] = 1
		}
		out[i] = vec
	}
	return out, nil
}

func TestSearchRanksAPIChunk(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("README.md", "# Проект\n\nЧат и ассистент разработчика.\n")
	write("docs/api.md", "# API\n\nPOST /api/chat принимает message. Команда /help отвечает про проект.\n")
	write("docs/memory.md", "# Память\n\nСлои лежат в data/memory: short-term, working, long-term.\n")

	idx := New(Options{Root: root, Model: "test", Embedder: bagEmbedder{}, TopK: 2})
	hits, err := idx.Search(context.Background(), "какие есть API", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Path != "docs/api.md" {
		t.Fatalf("hits: %#v", hits)
	}
	if idx.Status().Mode != "embeddings" {
		t.Fatalf("mode=%s", idx.Status().Mode)
	}
}

func TestSearchSpreadsFiles(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	api := "# API\n\nPOST /api/chat.\n\n## История\n\nGET /api/history.\n\n## Память ручки\n\nGET /api/memory.\n"
	if err := os.WriteFile(filepath.Join(docs, "api.md"), []byte(api), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Проект\n\nЕсть HTTP API чата.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := New(Options{Root: root, Model: "test", Embedder: bagEmbedder{}, TopK: 3})
	hits, err := idx.Search(context.Background(), "какие API", 3)
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]int{}
	for _, hit := range hits {
		paths[hit.Path]++
	}
	if paths["README.md"] == 0 || paths["docs/api.md"] > 2 {
		t.Fatalf("diversity: %#v", hits)
	}
}

func TestLexicalSearchWithoutEmbedder(t *testing.T) {
	root := filepath.Join("..", "..")
	idx := New(Options{Root: root, Model: "lexical-test"})
	hits, err := idx.Search(context.Background(), "nomic-embed-text команда help", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("expected hits from README and docs")
	}
	found := false
	for _, hit := range hits {
		if strings.Contains(hit.Path, "assistant") || hit.Path == "README.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("docs not retrieved: %#v", hits)
	}
}

func TestLiveOllama(t *testing.T) {
	if os.Getenv("RAG_LIVE") == "" {
		t.Skip("set RAG_LIVE=1 to embed the real corpus")
	}
	base := os.Getenv("RAG_OLLAMA_URL")
	if base == "" {
		base = "http://10.10.1.101:11434"
	}
	idx := New(Options{
		Root:      filepath.Join("..", ".."),
		Model:     "nomic-embed-text",
		OllamaURL: base,
		Embedder:  NewOllama(base, "nomic-embed-text"),
		TopK:      3,
	})
	hits, err := idx.Search(context.Background(), "как устроена структура каталогов проекта", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || idx.Status().Mode != "embeddings" {
		t.Fatalf("mode=%s hits=%d", idx.Status().Mode, len(hits))
	}
	t.Logf("top %s %.2f", hits[0].Path, hits[0].Score)
}

package rag

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestChunkMarkdownSplitsHeadings(t *testing.T) {
	body := "# Проект\n\nКорень сервиса.\n\n## API\n\nPOST /api/chat принимает message.\n\n## Память\n\nФайлы лежат в data/memory.\n"
	chunks := ChunkMarkdown("docs/api.md", body)
	if len(chunks) != 3 {
		t.Fatalf("chunks=%d, want 3: %#v", len(chunks), headings(chunks))
	}
	if chunks[1].Heading != "API" || !strings.Contains(chunks[1].Text, "/api/chat") {
		t.Fatalf("api chunk: %#v", chunks[1])
	}
	if chunks[0].ID == chunks[1].ID {
		t.Fatal("chunk ids must differ")
	}
}

func TestChunkMarkdownSkipsFenceHeadings(t *testing.T) {
	body := "# Пример\n\nТекст.\n\n```\n# не заголовок\n```\n\nХвост.\n"
	chunks := ChunkMarkdown("README.md", body)
	if len(chunks) != 1 {
		t.Fatalf("chunks=%d, want 1", len(chunks))
	}
	if !strings.Contains(chunks[0].Text, "не заголовок") {
		t.Fatalf("fence dropped: %s", chunks[0].Text)
	}
}

func TestChunkMarkdownSplitsLongSection(t *testing.T) {
	para := strings.Repeat("абзац про схему данных. ", 80)
	body := "# Схема\n\n" + para + "\n\n" + para + "\n\n" + para
	chunks := ChunkMarkdown("docs/data-schemas.md", body)
	if len(chunks) < 2 {
		t.Fatalf("expected split, got %d", len(chunks))
	}
	for _, chunk := range chunks {
		if utf8.RuneCountInString(chunk.Text) > maxChunkRunes+400 {
			t.Fatalf("chunk too long: %d", utf8.RuneCountInString(chunk.Text))
		}
	}
}

func headings(chunks []Chunk) []string {
	out := make([]string, len(chunks))
	for i, chunk := range chunks {
		out[i] = chunk.Heading
	}
	return out
}

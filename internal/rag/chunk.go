package rag

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode/utf8"
)

const (
	maxChunkRunes = 1400
	overlapRunes  = 180
)

// Chunk — кусок документации, который попадает в индекс.
type Chunk struct {
	ID      string    `json:"id"`
	Path    string    `json:"path"`
	Heading string    `json:"heading"`
	Text    string    `json:"text"`
	Vector  []float32 `json:"vector,omitempty"`
}

// ChunkFile режет markdown по заголовкам, а исходники — по функциям.
func ChunkFile(path, body string) []Chunk {
	if strings.HasSuffix(strings.ToLower(path), ".md") {
		return ChunkMarkdown(path, body)
	}
	return ChunkCode(path, body)
}

// ChunkCode делит исходник по func/type и длинные куски — по размеру.
func ChunkCode(path, body string) []Chunk {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	var sections []string
	var buf strings.Builder
	flush := func() {
		text := strings.TrimSpace(buf.String())
		buf.Reset()
		if text != "" {
			sections = append(sections, text)
		}
	}
	for _, line := range strings.Split(body, "\n") {
		if buf.Len() > 0 && (strings.HasPrefix(line, "func ") || strings.HasPrefix(line, "type ")) {
			flush()
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	flush()

	var out []Chunk
	for _, text := range sections {
		for _, part := range splitLong(codeHeading(text), text) {
			part = strings.TrimSpace(part)
			if utf8.RuneCountInString(part) < 20 {
				continue
			}
			heading := codeHeading(part)
			out = append(out, Chunk{
				ID:      chunkID(path, heading, part),
				Path:    path,
				Heading: heading,
				Text:    path + "\n" + part,
			})
		}
	}
	return out
}

func codeHeading(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "func ") || strings.HasPrefix(line, "type ") {
			runes := []rune(line)
			if len(runes) > 80 {
				return string(runes[:80])
			}
			return line
		}
	}
	return ""
}

// ChunkMarkdown режет markdown по заголовкам и длинные секции — по абзацам.
func ChunkMarkdown(path, body string) []Chunk {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}

	type section struct {
		heading string
		text    string
	}
	var sections []section
	var heading string
	var buf strings.Builder
	inFence := false
	flush := func() {
		text := strings.TrimSpace(buf.String())
		buf.Reset()
		if text == "" {
			return
		}
		sections = append(sections, section{heading: heading, text: text})
	}
	for _, line := range strings.Split(body, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") {
			inFence = !inFence
		}
		if !inFence && isHeading(trim) {
			flush()
			heading = headingText(trim)
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	flush()

	var out []Chunk
	for _, sec := range sections {
		for _, text := range splitLong(sec.heading, sec.text) {
			text = strings.TrimSpace(text)
			if utf8.RuneCountInString(text) < 20 {
				continue
			}
			out = append(out, Chunk{
				ID:      chunkID(path, sec.heading, text),
				Path:    path,
				Heading: sec.heading,
				Text:    text,
			})
		}
	}
	return out
}

func isHeading(line string) bool {
	if !strings.HasPrefix(line, "#") {
		return false
	}
	level := 0
	for _, r := range line {
		if r != '#' {
			break
		}
		level++
	}
	if level < 1 || level > 3 || level >= len(line) {
		return false
	}
	return line[level] == ' '
}

func headingText(line string) string {
	return strings.TrimSpace(strings.TrimLeft(line, "#"))
}

func splitLong(heading, text string) []string {
	if utf8.RuneCountInString(text) <= maxChunkRunes {
		return []string{text}
	}
	paras := strings.Split(text, "\n\n")
	var parts []string
	var buf strings.Builder
	flush := func() {
		part := strings.TrimSpace(buf.String())
		buf.Reset()
		if part != "" {
			parts = append(parts, part)
		}
	}
	for _, para := range paras {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		if utf8.RuneCountInString(para) > maxChunkRunes {
			flush()
			parts = append(parts, hardSplit(heading, para)...)
			continue
		}
		if buf.Len() > 0 && utf8.RuneCountInString(buf.String())+utf8.RuneCountInString(para)+2 > maxChunkRunes {
			tail := tailRunes(buf.String(), overlapRunes)
			flush()
			if heading != "" {
				buf.WriteString("# ")
				buf.WriteString(heading)
				buf.WriteString("\n\n")
			}
			if tail != "" {
				buf.WriteString(tail)
				buf.WriteString("\n\n")
			}
		}
		if buf.Len() > 0 {
			buf.WriteString("\n\n")
		}
		buf.WriteString(para)
	}
	flush()
	if len(parts) == 0 {
		return []string{text}
	}
	return parts
}

func hardSplit(heading, text string) []string {
	runes := []rune(text)
	var parts []string
	step := maxChunkRunes - overlapRunes
	if step < 400 {
		step = 400
	}
	for start := 0; start < len(runes); start += step {
		end := start + maxChunkRunes
		if end > len(runes) {
			end = len(runes)
		}
		piece := strings.TrimSpace(string(runes[start:end]))
		if heading != "" && start > 0 {
			piece = "# " + heading + "\n\n" + piece
		}
		if piece != "" {
			parts = append(parts, piece)
		}
		if end == len(runes) {
			break
		}
	}
	return parts
}

func tailRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(string(r[len(r)-n:]))
}

func chunkID(path, heading, text string) string {
	sum := sha256.Sum256([]byte(path + "\n" + heading + "\n" + text))
	return hex.EncodeToString(sum[:8])
}

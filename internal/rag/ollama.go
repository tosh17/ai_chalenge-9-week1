package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Ollama ходит в /api/embed локального сервера (nomic-embed-text).
type Ollama struct {
	base  string
	model string
	http  *http.Client
}

func NewOllama(base, model string) *Ollama {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if model == "" {
		model = "nomic-embed-text"
	}
	return &Ollama{
		base:  base,
		model: model,
		http:  &http.Client{Timeout: 45 * time.Second},
	}
}

func (o *Ollama) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if o == nil || o.base == "" {
		return nil, fmt.Errorf("ollama url is empty")
	}
	if len(texts) == 0 {
		return nil, nil
	}
	payload, err := json.Marshal(map[string]any{
		"model": o.model,
		"input": texts,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.base+"/api/embed", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama embed: %s: %s", resp.Status, trimBody(body))
	}
	var parsed struct {
		Embeddings [][]float32 `json:"embeddings"`
		Error      string      `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}
	if parsed.Error != "" {
		return nil, fmt.Errorf("ollama embed: %s", parsed.Error)
	}
	if len(parsed.Embeddings) != len(texts) {
		return nil, fmt.Errorf("ollama embed: got %d vectors for %d texts", len(parsed.Embeddings), len(texts))
	}
	return parsed.Embeddings, nil
}

func trimBody(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 240 {
		return s[:240]
	}
	return s
}

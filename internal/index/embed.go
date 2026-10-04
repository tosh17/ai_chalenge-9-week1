package index

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

// Embedder получает векторы для уже нарезанных чанков.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// Ollama ходит в локальный /api/embed.
type Ollama struct {
	BaseURL string
	Model   string
	HTTP    *http.Client
}

func (o *Ollama) client() *http.Client {
	if o.HTTP != nil {
		return o.HTTP
	}
	return &http.Client{Timeout: 3 * time.Minute}
}

func (o *Ollama) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	prefixed := make([]string, len(texts))
	for i, t := range texts {
		prefixed[i] = "search_document: " + t
	}
	var last error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
		}
		vecs, err := o.embedOnce(ctx, prefixed)
		if err == nil {
			return vecs, nil
		}
		last = err
	}
	return nil, last
}

func (o *Ollama) embedOnce(ctx context.Context, texts []string) ([][]float32, error) {
	payload, err := json.Marshal(map[string]any{
		"model": o.Model,
		"input": texts,
	})
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(o.BaseURL, "/") + "/api/embed"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := o.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama %s: %s", res.Status, truncate(body, 300))
	}
	var parsed struct {
		Embeddings [][]float32 `json:"embeddings"`
		Error      string      `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	if parsed.Error != "" {
		return nil, fmt.Errorf("ollama: %s", parsed.Error)
	}
	if len(parsed.Embeddings) != len(texts) {
		return nil, fmt.Errorf("ollama: векторов %d, текстов %d", len(parsed.Embeddings), len(texts))
	}
	return parsed.Embeddings, nil
}

func truncate(b []byte, n int) string {
	s := string(b)
	if len(s) <= n {
		return s
	}
	return s[:n]
}

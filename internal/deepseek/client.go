package deepseek

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	apiKey        string
	model         string
	baseURL       string
	defaultTokens int
	http          *http.Client
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Stream      bool      `json:"stream"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature *float64  `json:"temperature,omitempty"`
	Stop        []string  `json:"stop,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type DebugInfo struct {
	URL        string          `json:"url"`
	Request    json.RawMessage `json:"request"`
	Response   json.RawMessage `json:"response,omitempty"`
	StatusCode int             `json:"status_code"`
	DurationMs int64           `json:"duration_ms"`
}

type ChatResult struct {
	Reply string
	Debug DebugInfo
}

// StreamHandler вызывается на каждый текстовый фрагмент ответа.
type StreamHandler func(delta string) error

func NewClient(apiKey, model, baseURL string, maxTokens int) *Client {
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	return &Client{
		apiKey:        apiKey,
		model:         model,
		baseURL:       baseURL,
		defaultTokens: maxTokens,
		http: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (c *Client) resolveTokens(opts ChatOptions) int {
	if opts.MaxTokens > 0 {
		return opts.MaxTokens
	}
	return c.defaultTokens
}

func (c *Client) buildPayload(messages []Message, opts ChatOptions, stream bool) chatRequest {
	return chatRequest{
		Model:       c.model,
		Messages:    withSystemPrompt(messages, opts),
		Stream:      stream,
		MaxTokens:   c.resolveTokens(opts),
		Temperature: opts.Temperature,
	}
}

func (c *Client) Chat(ctx context.Context, messages []Message, opts ChatOptions) (ChatResult, error) {
	start := time.Now()
	payload := c.buildPayload(messages, opts, false)

	body, err := json.Marshal(payload)
	if err != nil {
		return ChatResult{}, fmt.Errorf("marshal request: %w", err)
	}

	debug := DebugInfo{
		URL:     c.baseURL,
		Request: json.RawMessage(body),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return ChatResult{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return ChatResult{Debug: debug}, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	debug.StatusCode = resp.StatusCode
	debug.DurationMs = time.Since(start).Milliseconds()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return ChatResult{Debug: debug}, fmt.Errorf("read response: %w", err)
	}
	debug.Response = json.RawMessage(respBody)

	var result chatResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return ChatResult{Debug: debug}, fmt.Errorf("decode response: %w", err)
	}
	if result.Error != nil {
		return ChatResult{Debug: debug}, fmt.Errorf("deepseek api error: %s", result.Error.Message)
	}
	if resp.StatusCode >= 400 {
		return ChatResult{Debug: debug}, fmt.Errorf("deepseek api returned status %d: %s", resp.StatusCode, string(respBody))
	}
	if len(result.Choices) == 0 {
		return ChatResult{Debug: debug}, fmt.Errorf("empty response from deepseek")
	}

	return ChatResult{
		Reply: cleanReply(result.Choices[0].Message.Content),
		Debug: debug,
	}, nil
}

// ChatStream стримит ответ DeepSeek (SSE) и вызывает onDelta на каждый кусок текста.
func (c *Client) ChatStream(ctx context.Context, messages []Message, opts ChatOptions, onDelta StreamHandler) (ChatResult, error) {
	start := time.Now()
	payload := c.buildPayload(messages, opts, true)

	body, err := json.Marshal(payload)
	if err != nil {
		return ChatResult{}, fmt.Errorf("marshal request: %w", err)
	}

	debug := DebugInfo{
		URL:     c.baseURL,
		Request: json.RawMessage(body),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return ChatResult{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(req)
	if err != nil {
		return ChatResult{Debug: debug}, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	debug.StatusCode = resp.StatusCode

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		debug.Response = json.RawMessage(respBody)
		debug.DurationMs = time.Since(start).Milliseconds()
		return ChatResult{Debug: debug}, fmt.Errorf("deepseek api returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var full strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			debug.DurationMs = time.Since(start).Milliseconds()
			return ChatResult{Reply: cleanReply(full.String()), Debug: debug}, err
		}

		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.Error != nil {
			debug.DurationMs = time.Since(start).Milliseconds()
			return ChatResult{Reply: cleanReply(full.String()), Debug: debug}, fmt.Errorf("deepseek api error: %s", chunk.Error.Message)
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		delta := chunk.Choices[0].Delta.Content
		if delta == "" {
			continue
		}
		full.WriteString(delta)
		if onDelta != nil {
			if err := onDelta(delta); err != nil {
				debug.DurationMs = time.Since(start).Milliseconds()
				return ChatResult{Reply: cleanReply(full.String()), Debug: debug}, err
			}
		}
	}

	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		debug.DurationMs = time.Since(start).Milliseconds()
		return ChatResult{Reply: cleanReply(full.String()), Debug: debug}, fmt.Errorf("read stream: %w", err)
	}

	debug.DurationMs = time.Since(start).Milliseconds()
	reply := cleanReply(full.String())
	debug.Response = json.RawMessage([]byte(fmt.Sprintf(`{"streamed_chars":%d}`, len(reply))))
	return ChatResult{Reply: reply, Debug: debug}, nil
}

func withSystemPrompt(messages []Message, opts ChatOptions) []Message {
	out := make([]Message, 0, len(messages)+1)
	out = append(out, Message{Role: "system", Content: BuildSystemPrompt(opts)})
	for _, m := range messages {
		if m.Role == "system" {
			continue
		}
		out = append(out, m)
	}
	return out
}

func cleanReply(reply string) string {
	reply = strings.ReplaceAll(reply, StopMarker, "")
	return strings.TrimSpace(reply)
}

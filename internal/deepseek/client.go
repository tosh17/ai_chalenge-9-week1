package deepseek

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	apiKey  string
	model   string
	baseURL string
	http    *http.Client
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type DebugInfo struct {
	URL        string          `json:"url"`
	Request    json.RawMessage `json:"request"`
	Response   json.RawMessage `json:"response"`
	StatusCode int             `json:"status_code"`
	DurationMs int64           `json:"duration_ms"`
}

type ChatResult struct {
	Reply string
	Debug DebugInfo
}

func NewClient(apiKey, model, baseURL string) *Client {
	return &Client{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		http: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (c *Client) Chat(ctx context.Context, messages []Message) (ChatResult, error) {
	start := time.Now()

	body, err := json.Marshal(chatRequest{
		Model:    c.model,
		Messages: messages,
		Stream:   false,
	})
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
		Reply: result.Choices[0].Message.Content,
		Debug: debug,
	}, nil
}

package deepseek

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
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
	Usage *Usage `json:"usage,omitempty"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Usage — токены из ответа провайдера (OpenAI-compatible).
type Usage struct {
	PromptTokens       int `json:"prompt_tokens"`
	CompletionTokens   int `json:"completion_tokens"`
	TotalTokens        int `json:"total_tokens"`
	PromptCacheHitTok  int `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTok int `json:"prompt_cache_miss_tokens"`
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
	Usage *Usage
	Debug DebugInfo
}

func NewClient(apiKey, model, baseURL string) *Client {
	return &Client{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		http:    newHTTPClient(),
	}
}

// newHTTPClient обходит баг macOS Security.framework (x509 OSStatus -26276).
// RootCAs не задаём: SystemCertPool() на Darwin — это Keychain (systemPool),
// а не Go-корни. Проверку делает crypto/x509 + fallback из
// //go:debug x509usefallbackroots=1 в cmd/server.
func newHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	transport.TLSHandshakeTimeout = 15 * time.Second
	// Короткий TCP-connect: если LAN LLM мёртв, не ждём 30с DefaultTransport.
	transport.DialContext = (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	return &http.Client{
		Timeout:   0,
		Transport: transport,
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
		return ChatResult{Debug: debug}, fmt.Errorf("do request: %w", wrapNet(err))
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
		Usage: result.Usage,
		Debug: debug,
	}, nil
}

func wrapNet(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "OSStatus") {
		return fmt.Errorf("%w (macOS Keychain TLS; нужен перезапуск сервера с Go-корнями сертификатов)", err)
	}
	if strings.Contains(msg, "i/o timeout") || strings.Contains(msg, "connection refused") || strings.Contains(msg, "no route to host") {
		return fmt.Errorf("%w (LLM-хост недоступен; подними локальный сервер или выбери DeepSeek)", err)
	}
	return err
}

// Ping проверяет TCP до API без вызова модели.
func (c *Client) Ping(ctx context.Context) error {
	return pingHTTPURL(ctx, c.baseURL)
}

func pingHTTPURL(ctx context.Context, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	host := u.Host
	if host == "" {
		return fmt.Errorf("empty host in %q", raw)
	}
	if u.Port() == "" {
		port := "80"
		if u.Scheme == "https" {
			port = "443"
		}
		host = net.JoinHostPort(u.Hostname(), port)
	}
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		return err
	}
	return conn.Close()
}

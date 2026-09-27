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
	"strconv"
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
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall — вызов функции, который модель просит выполнить.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Tool — описание функции для запроса к модели.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type chatRequest struct {
	Model    string     `json:"model"`
	Messages []Message  `json:"messages"`
	Stream   bool       `json:"stream"`
	Tools    []wireTool `json:"tools,omitempty"`
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
	Reply     string
	ToolCalls []ToolCall
	Usage     *Usage
	Debug     DebugInfo
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
	return c.ChatTools(ctx, messages, nil)
}

// ChatTools отправляет запрос с описаниями инструментов. Пустой tools равен обычному Chat.
func (c *Client) ChatTools(ctx context.Context, messages []Message, tools []Tool) (ChatResult, error) {
	start := time.Now()

	body, err := json.Marshal(chatRequest{
		Model:    c.model,
		Messages: messages,
		Stream:   false,
		Tools:    wireTools(tools),
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

	msg := result.Choices[0].Message
	return ChatResult{
		Reply:     msg.Content,
		ToolCalls: msg.ToolCalls,
		Usage:     result.Usage,
		Debug:     debug,
	}, nil
}

type wireTool struct {
	Type     string    `json:"type"`
	Function wireFnDef `json:"function"`
}

type wireFnDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type wireFnCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type wireToolCall struct {
	ID       string     `json:"id"`
	Type     string     `json:"type,omitempty"`
	Function wireFnCall `json:"function"`
}

type messageWire struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	Reasoning  string         `json:"reasoning,omitempty"`
	Name       string         `json:"name,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
}

func (m Message) MarshalJSON() ([]byte, error) {
	wire := messageWire{
		Role:       m.Role,
		Name:       m.Name,
		ToolCallID: m.ToolCallID,
	}
	if m.Content != "" || len(m.ToolCalls) == 0 {
		wire.Content = m.Content
	}
	for _, call := range m.ToolCalls {
		args := strings.TrimSpace(call.Arguments)
		if args == "" {
			args = "{}"
		}
		wire.ToolCalls = append(wire.ToolCalls, wireToolCall{
			ID:   call.ID,
			Type: "function",
			Function: wireFnCall{
				Name:      call.Name,
				Arguments: json.RawMessage(strconv.Quote(args)),
			},
		})
	}
	return json.Marshal(wire)
}

func (m *Message) UnmarshalJSON(data []byte) error {
	var wire messageWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	m.Role = wire.Role
	m.Content = wire.Content
	if strings.TrimSpace(m.Content) == "" && len(wire.ToolCalls) == 0 {
		m.Content = strings.TrimSpace(wire.Reasoning)
	}
	m.Name = wire.Name
	m.ToolCallID = wire.ToolCallID
	m.ToolCalls = nil
	for _, call := range wire.ToolCalls {
		m.ToolCalls = append(m.ToolCalls, ToolCall{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: stringifyArgs(call.Function.Arguments),
		})
	}
	return nil
}

func stringifyArgs(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return "{}"
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			if strings.TrimSpace(s) == "" {
				return "{}"
			}
			return s
		}
	}
	return string(raw)
}

func wireTools(tools []Tool) []wireTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]wireTool, 0, len(tools))
	for _, tool := range tools {
		params := tool.Parameters
		if len(bytes.TrimSpace(params)) == 0 {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, wireTool{
			Type: "function",
			Function: wireFnDef{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  params,
			},
		})
	}
	return out
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

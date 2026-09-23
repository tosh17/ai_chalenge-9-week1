package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// Tool — инструмент, который сервер отдал в tools/list.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// CallResult — текст ответа tools/call и сырой обмен по stdin/stdout.
type CallResult struct {
	Text     string
	IsError  bool
	Request  json.RawMessage
	Response json.RawMessage
}

// Client говорит с MCP-сервером по stdin/stdout, по одному JSON на строку.
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	wait   chan error

	mu     sync.Mutex
	nextID int
	tools  []Tool
}

// Start запускает процесс и делает initialize + tools/list.
func Start(ctx context.Context, command string, args ...string) (*Client, error) {
	cmd := exec.Command(command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start mcp: %w", err)
	}

	c := &Client{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
		wait:   make(chan error, 1),
		nextID: 1,
	}
	go func() {
		_, _ = io.Copy(io.Discard, stderr)
	}()
	go func() {
		c.wait <- cmd.Wait()
	}()

	if err := c.initialize(ctx); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

// Tools возвращает список, полученный при старте.
func (c *Client) Tools() []Tool {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Tool, len(c.tools))
	copy(out, c.tools)
	return out
}

// Call вызывает инструмент. arguments — JSON-объект.
func (c *Client) Call(ctx context.Context, name string, arguments json.RawMessage) (CallResult, error) {
	if len(arguments) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	var result callResult
	reqRaw, respRaw, err := c.roundtrip(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": arguments,
	}, &result)
	out := CallResult{Request: reqRaw, Response: respRaw}
	if err != nil {
		return out, err
	}
	out.Text = joinText(result.Content)
	out.IsError = result.IsError
	return out, nil
}

// Close останавливает процесс.
func (c *Client) Close() error {
	if c == nil || c.cmd == nil || c.cmd.Process == nil {
		return nil
	}
	_ = c.stdin.Close()
	_ = c.cmd.Process.Kill()
	<-c.wait
	return nil
}

func (c *Client) initialize(ctx context.Context) error {
	var init initializeResult
	if _, _, err := c.roundtrip(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "week1", "version": "day16"},
	}, &init); err != nil {
		return fmt.Errorf("mcp initialize: %w", err)
	}
	if err := c.notify("notifications/initialized", map[string]any{}); err != nil {
		return err
	}
	var listed toolsList
	if _, _, err := c.roundtrip(ctx, "tools/list", map[string]any{}, &listed); err != nil {
		return fmt.Errorf("mcp tools/list: %w", err)
	}
	c.mu.Lock()
	c.tools = listed.Tools
	c.mu.Unlock()
	return nil
}

func (c *Client) notify(method string, params any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.write(rpcMessage{JSONRPC: "2.0", Method: method, Params: params})
}

func (c *Client) roundtrip(ctx context.Context, method string, params any, dest any) (reqRaw, respRaw []byte, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.nextID
	c.nextID++
	reqRaw, err = json.Marshal(rpcMessage{JSONRPC: "2.0", ID: &id, Method: method, Params: params})
	if err != nil {
		return nil, nil, err
	}
	if err = c.writeRaw(append(append([]byte{}, reqRaw...), '\n')); err != nil {
		return reqRaw, nil, err
	}

	line, err := c.readLine(ctx)
	if err != nil {
		return reqRaw, nil, err
	}
	respRaw = bytes.TrimSpace(line)
	var msg rpcMessage
	if err := json.Unmarshal(respRaw, &msg); err != nil {
		return reqRaw, respRaw, fmt.Errorf("mcp decode: %w", err)
	}
	if msg.Error != nil {
		return reqRaw, respRaw, fmt.Errorf("mcp %s: %s", method, msg.Error.Message)
	}
	if dest == nil || len(msg.Result) == 0 {
		return reqRaw, respRaw, nil
	}
	if err := json.Unmarshal(msg.Result, dest); err != nil {
		return reqRaw, respRaw, fmt.Errorf("mcp result: %w", err)
	}
	return reqRaw, respRaw, nil
}

func (c *Client) writeRaw(body []byte) error {
	_, err := c.stdin.Write(body)
	return err
}

func (c *Client) write(msg rpcMessage) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	body = append(body, '\n')
	_, err = c.stdin.Write(body)
	return err
}

func (c *Client) readLine(ctx context.Context) ([]byte, error) {
	type read struct {
		line []byte
		err  error
	}
	ch := make(chan read, 1)
	go func() {
		line, err := c.stdout.ReadBytes('\n')
		ch <- read{line: line, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case got := <-ch:
		if got.err != nil {
			return nil, fmt.Errorf("mcp stdout: %w", got.err)
		}
		return got.line, nil
	}
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  any             `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type initializeResult struct {
	ProtocolVersion string `json:"protocolVersion"`
	ServerInfo      struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"serverInfo"`
}

type toolsList struct {
	Tools []Tool `json:"tools"`
}

type callResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

func joinText(parts []struct {
	Type string `json:"type"`
	Text string `json:"text"`
}) string {
	out := ""
	for _, part := range parts {
		if part.Text == "" {
			continue
		}
		if out != "" {
			out += "\n"
		}
		out += part.Text
	}
	return out
}

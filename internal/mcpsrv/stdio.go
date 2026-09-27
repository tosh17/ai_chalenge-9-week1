package mcpsrv

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Tool — один инструмент stdio-сервера.
type Tool struct {
	Name        string
	Description string
	Schema      json.RawMessage
	Handle      func(args json.RawMessage) (string, error)
}

// Serve читает JSON-RPC по строкам и пишет ответ в stdout.
func Serve(name string, tools []Tool) {
	in := bufio.NewReader(os.Stdin)
	out := os.Stdout
	for {
		line, err := in.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				fmt.Fprintf(os.Stderr, "mcp %s: %v\n", name, err)
			}
			return
		}
		if len(bytesTrim(line)) == 0 {
			continue
		}
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		if msg.Method == "notifications/initialized" || len(msg.ID) == 0 {
			continue
		}
		result, callErr := dispatch(name, tools, msg.Method, msg.Params)
		var payload []byte
		if callErr != nil {
			payload, _ = json.Marshal(map[string]any{
				"jsonrpc": "2.0",
				"id":      json.RawMessage(msg.ID),
				"error":   map[string]any{"code": -32000, "message": callErr.Error()},
			})
		} else {
			raw, _ := json.Marshal(result)
			payload, _ = json.Marshal(struct {
				JSONRPC string          `json:"jsonrpc"`
				ID      json.RawMessage `json:"id"`
				Result  json.RawMessage `json:"result"`
			}{JSONRPC: "2.0", ID: msg.ID, Result: raw})
		}
		payload = append(payload, '\n')
		_, _ = out.Write(payload)
	}
}

func dispatch(name string, tools []Tool, method string, params json.RawMessage) (any, error) {
	switch method {
	case "initialize":
		return map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]string{"name": name, "version": "day20"},
		}, nil
	case "tools/list":
		listed := make([]map[string]any, 0, len(tools))
		for _, tool := range tools {
			schema := tool.Schema
			if len(schema) == 0 {
				schema = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			listed = append(listed, map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"inputSchema": json.RawMessage(schema),
			})
		}
		return map[string]any{"tools": listed}, nil
	case "tools/call":
		var call struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if len(params) > 0 {
			_ = json.Unmarshal(params, &call)
		}
		for _, tool := range tools {
			if tool.Name != call.Name {
				continue
			}
			text, err := tool.Handle(call.Arguments)
			if err != nil {
				return map[string]any{
					"content": []map[string]string{{"type": "text", "text": err.Error()}},
					"isError": true,
				}, nil
			}
			return map[string]any{
				"content": []map[string]string{{"type": "text", "text": text}},
				"isError": false,
			}, nil
		}
		return nil, fmt.Errorf("unknown tool %s", call.Name)
	default:
		return map[string]any{}, nil
	}
}

func bytesTrim(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && (b[i] == ' ' || b[i] == '\n' || b[i] == '\r' || b[i] == '\t') {
		i++
	}
	for j > i && (b[j-1] == ' ' || b[j-1] == '\n' || b[j-1] == '\r' || b[j-1] == '\t') {
		j--
	}
	return b[i:j]
}

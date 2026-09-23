package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClientRoundtrip(t *testing.T) {
	if _, err := os.Stat("/usr/bin/python3"); err != nil {
		if _, err2 := os.Stat("/usr/local/bin/python3"); err2 != nil {
			t.Skip("python3 not found")
		}
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "fake_mcp.py")
	if err := os.WriteFile(script, []byte(fakeMCP), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := Start(ctx, "python3", script)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	tools := client.Tools()
	if len(tools) != 1 || tools[0].Name != "get_weather" {
		t.Fatalf("tools: %+v", tools)
	}
	got, err := client.Call(ctx, "get_weather", json.RawMessage(`{"city":"Москва","days":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.IsError || got.Text != "Москва 17°C" {
		t.Fatalf("call: %+v", got)
	}
}

func TestOpenMeteoLive(t *testing.T) {
	jar := os.Getenv("MCP_JAR")
	if jar == "" {
		jar = filepath.Join("..", "..", "mcp", "open_meteo", "build", "libs", "open-meteo-0.1.0-all.jar")
	}
	if _, err := os.Stat(jar); err != nil {
		t.Skip("open-meteo jar not built")
	}
	javaBin, err := FindJava(os.Getenv("MCP_JAVA"))
	if err != nil {
		t.Skip(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, err := Start(ctx, javaBin, "-Dkotlin-logging.logStartupMessage=false", "-jar", jar)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	got, err := client.Call(ctx, "get_weather", json.RawMessage(`{"city":"Москва","days":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.IsError || !strings.Contains(got.Text, "Москва") || !strings.Contains(got.Text, "open-meteo.com") {
		t.Fatalf("live weather: %+v", got)
	}
}

const fakeMCP = `import json, sys
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    msg = json.loads(line)
    method = msg.get("method")
    if method == "notifications/initialized":
        continue
    mid = msg.get("id")
    if method == "initialize":
        result = {"protocolVersion": "2024-11-05", "capabilities": {"tools": {}}, "serverInfo": {"name": "fake", "version": "0"}}
    elif method == "tools/list":
        result = {"tools": [{"name": "get_weather", "description": "weather", "inputSchema": {"type": "object"}}]}
    elif method == "tools/call":
        result = {"content": [{"type": "text", "text": "Москва 17°C"}], "isError": False}
    else:
        result = {}
    sys.stdout.write(json.dumps({"jsonrpc": "2.0", "id": mid, "result": result}, ensure_ascii=False) + "\n")
    sys.stdout.flush()
`

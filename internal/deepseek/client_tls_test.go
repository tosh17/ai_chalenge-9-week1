package deepseek

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestPingLocalListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	c := NewClient("k", "m", "http://"+ln.Addr().String()+"/v1/chat/completions")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Ping(ctx); err != nil {
		t.Fatalf("ping live listener: %v", err)
	}
}

func TestPingUnreachableFailsFast(t *testing.T) {
	c := NewClient("k", "m", "http://127.0.0.1:1/v1/chat/completions")
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := c.Ping(ctx)
	if err == nil {
		t.Fatal("expected ping error")
	}
	if time.Since(start) > 1500*time.Millisecond {
		t.Fatalf("ping too slow: %s / %v", time.Since(start), err)
	}
}

func TestDeepSeekTLSHandshake(t *testing.T) {
	c := newHTTPClient()
	req, err := http.NewRequest(http.MethodGet, "https://api.deepseek.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Skipf("tls/http to api.deepseek.com: %v", err)
	}
	defer resp.Body.Close()
}

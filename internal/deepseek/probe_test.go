package deepseek

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbeStableNoRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "no auth", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]string{{"id": "qwen-local"}},
			})
		case "/api/show":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"parameters": "temperature                    0.4\nnum_ctx                        32768\n",
				"details": map[string]string{
					"quantization_level": "Q4_K_M",
					"parameter_size":     "9.7B",
				},
				"model_info": map[string]any{"qwen35.context_length": 262144},
			})
		case "/v1/chat/completions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": "Ок"}}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient("key", "qwen-local", srv.URL+"/v1/chat/completions")
	report, err := c.Probe(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if !report.NetworkOK || !report.Stable || len(report.Shots) != 3 {
		t.Fatalf("report %+v", report)
	}
	if report.ContextLength != 262144 || report.NumCtx != 32768 || report.Quant != "Q4_K_M" {
		t.Fatalf("limits %+v", report)
	}
	if !strings.Contains(report.RateLimit, "отдельного лимита") {
		t.Fatalf("rate %q", report.RateLimit)
	}
	if !strings.Contains(report.Summary, "262144") || !strings.Contains(report.Summary, "3 из 3") {
		t.Fatalf("summary %q", report.Summary)
	}
}

func TestProbeRateLimited(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]string{{"id": "qwen-local"}},
			})
		case "/api/show":
			w.WriteHeader(http.StatusNotFound)
		case "/v1/chat/completions":
			n++
			if n > 1 {
				w.Header().Set("Retry-After", "2")
				http.Error(w, "slow down", http.StatusTooManyRequests)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"message": map[string]string{"content": "Ок"}}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient("key", "qwen-local", srv.URL+"/v1/chat/completions")
	report, err := c.Probe(context.Background(), 2)
	if err == nil {
		t.Fatal("expected unstable probe")
	}
	if report.Stable || !strings.Contains(report.RateLimit, "429") {
		t.Fatalf("rate %q stable %v", report.RateLimit, report.Stable)
	}
	if report.RateHeaders["retry-after"] != "2" {
		t.Fatalf("headers %+v", report.RateHeaders)
	}
}

func TestProbeNetworkDown(t *testing.T) {
	c := NewClient("key", "qwen-local", "http://127.0.0.1:1/v1/chat/completions")
	report, err := c.Probe(context.Background(), 1)
	if err == nil || report.NetworkOK {
		t.Fatalf("err %v report %+v", err, report)
	}
}

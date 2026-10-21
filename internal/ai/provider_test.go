package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	a, _ := Mock{}.Analyze(context.Background(), Context{Type: "high_latency", Severity: "high"})
	b, _ := json.Marshal(a)
	if _, err := Parse(b); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{`{}`, `{"summary":"x"}`, string(b) + `{}`, strings.Replace(string(b), `"summary":`, `"unknown":`, 1)} {
		if _, err := Parse([]byte(v)); err == nil {
			t.Fatal("invalid analysis accepted")
		}
	}
}
func TestProvider(t *testing.T) {
	a, _ := Mock{}.Analyze(context.Background(), Context{Type: "high_cpu"})
	content, _ := json.Marshal(a)
	for _, code := range []int{200, 429, 500} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-key" {
					t.Error("missing authorization")
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body["response_format"] == nil {
					t.Error("missing structured output schema")
				}
				w.WriteHeader(code)
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(content)}, "finish_reason": "stop"}}})
			}))
			defer server.Close()
			client := New(server.URL, "test-key", "test-model", time.Second)
			_, err := client.Analyze(context.Background(), Context{})
			if (err == nil) != (code == 200) {
				t.Fatalf("status %d: %v", code, err)
			}
		})
	}
}
func TestTimeoutAndMalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(100 * time.Millisecond) }))
	defer server.Close()
	_, err := New(server.URL, "key", "model", 10*time.Millisecond).Analyze(context.Background(), Context{})
	if err == nil {
		t.Fatal("timeout missing")
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"choices":[]}`)) }))
	defer bad.Close()
	if _, err = New(bad.URL, "key", "model", time.Second).Analyze(context.Background(), Context{}); err == nil {
		t.Fatal("bad response accepted")
	}
}

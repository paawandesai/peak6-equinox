package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAnthropicClientSuccess verifies the client can parse a successful response
// (M3-06: never call live API, use httptest mock).
func TestAnthropicClientSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method: got %s, want POST", r.Method)
		}
		if r.Header.Get("x-api-key") == "" {
			t.Error("missing x-api-key header")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":"msg_123","type":"message","role":"assistant","content":[{"type":"text","text":"equivalent"}],"model":"claude-haiku-4-5-20251001","usage":{"input_tokens":100,"output_tokens":10}}`))
	}))
	defer server.Close()

	client, err := NewAnthropicClient("sk-ant-test", "claude-haiku-4-5-20251001", 400)
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL // override for test

	resp, err := client.Complete(context.Background(), Prompt{
		System: "You are a test",
		User:   "Respond with: equivalent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp != "equivalent" {
		t.Errorf("response: got %q, want %q", resp, "equivalent")
	}
}

// TestAnthropicClientHTTPError verifies error handling for non-2xx responses.
func TestAnthropicClientHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"type":"rate_limit_error","message":"rate limit exceeded"}}`))
	}))
	defer server.Close()

	client, err := NewAnthropicClient("sk-ant-test", "claude-haiku-4-5-20251001", 400)
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL

	_, err = client.Complete(context.Background(), Prompt{System: "test", User: "test"})
	if err == nil {
		t.Fatal("expected error for 429 response")
	}
}

// TestAnthropicClientEmptyKey verifies empty key is rejected.
func TestAnthropicClientEmptyKey(t *testing.T) {
	_, err := NewAnthropicClient("", "model", 400)
	if err == nil {
		t.Fatal("expected error for empty API key")
	}
}

// TestAnthropicClientEmptyModel verifies empty model is rejected.
func TestAnthropicClientEmptyModel(t *testing.T) {
	_, err := NewAnthropicClient("sk-ant-test", "", 400)
	if err == nil {
		t.Fatal("expected error for empty model")
	}
}

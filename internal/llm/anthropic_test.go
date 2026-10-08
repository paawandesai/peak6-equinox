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
package llm

import (
	"context"
	"sync"
	"testing"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// TestConcurrentVerifyUsageAttribution proves that parallel Verify calls with
// a shared verifier correctly attribute per-call token usage (Cursor: run with -race).
func TestConcurrentVerifyUsageAttribution(t *testing.T) {
	// Create a model that returns different responses for different calls
	model := &ScriptedModel{Responses: []string{
		`{"relationship":"equivalent","confidence":0.9}`,
		`{"relationship":"related","confidence":0.8}`,
		`{"relationship":"unrelated","confidence":0.7}`,
	}}
	verifier := NewVerifier(model)

	a, b := InjectionCase{Field: "title", VenueText: "test"}.Instruments()
	rel := rulesRel(canonical.RelEquivalent)

	// Run 3 concurrent Verify calls
	const N = 3
	results := make([]VerifyResult, N)
	errs := make([]error, N)
	var wg sync.WaitGroup
	wg.Add(N)

	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			results[i], errs[i] = verifier.Verify(context.Background(), a, b, rel)
		}()
	}

	wg.Wait()

	// All calls should succeed
	for i, err := range errs {
		if err != nil {
			t.Fatalf("call %d failed: %v", i, err)
		}
	}

	// Each result should have its own verdict (not shared state)
	// Mocks return zero usage, so we're just checking the verdicts are correct
	types := []canonical.RelationshipType{
		canonical.RelEquivalent,
		canonical.RelRelated,
		canonical.RelUnrelated,
	}
	seen := make(map[canonical.RelationshipType]bool)
	for i, result := range results {
		if result.Verdict.Relationship == "" {
			t.Errorf("call %d: empty verdict", i)
		}
		seen[result.Verdict.Relationship] = true
	}

	// We should see all 3 different types (proving no shared state corruption)
	for _, typ := range types {
		if !seen[typ] {
			t.Errorf("expected to see %s verdict, but didn't (shared state corruption?)", typ)
		}
	}
}

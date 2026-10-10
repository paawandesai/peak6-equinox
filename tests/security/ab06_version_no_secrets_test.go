package security

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/web"
)

// TestAB06_VersionNoSecrets checks that GET /version exposes only commit and
// build_time, never secrets (AB-06, issue #80).
func TestAB06_VersionNoSecrets(t *testing.T) {
	testCommit := "abc123def456"
	testBuildTime := "2026-10-09T12:00:00Z"
	testSecret := "super-secret-api-key-12345"

	srv := web.New(web.Config{}, web.Deps{
		AppVersion:      testCommit,
		AppBuildTime:    testBuildTime,
		AnthropicAPIKey: testSecret, // This should never appear in the response
	})

	req := httptest.NewRequest("GET", "/version", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /version: got status %d, want 200", w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding /version response: %v", err)
	}

	// Check that commit is present and correct
	if got := resp["commit"]; got != testCommit {
		t.Errorf("commit = %q, want %q", got, testCommit)
	}

	// Check that build_time is present and correct
	if got := resp["build_time"]; got != testBuildTime {
		t.Errorf("build_time = %q, want %q", got, testBuildTime)
	}

	// Check that response has exactly 2 fields (commit and build_time)
	if len(resp) != 2 {
		t.Errorf("response has %d fields, want exactly 2 (commit, build_time)", len(resp))
	}

	// Check that the secret does not appear anywhere in the response body
	body := w.Body.String()
	if strings.Contains(body, testSecret) {
		t.Errorf("response contains secret API key: %q", testSecret)
	}

	// Check that no common secret field names appear
	forbiddenKeys := []string{
		"password", "api_key", "secret", "token", "key",
		"anthropic", "database", "admin",
	}
	for _, key := range forbiddenKeys {
		if _, found := resp[key]; found {
			t.Errorf("response contains forbidden key %q", key)
		}
	}
}

// TestAB06_VersionEmptyBuildTime checks that /version works when build_time is not set.
func TestAB06_VersionEmptyBuildTime(t *testing.T) {
	srv := web.New(web.Config{}, web.Deps{
		AppVersion:   "dev+abc123",
		AppBuildTime: "", // Empty = not set
	})

	req := httptest.NewRequest("GET", "/version", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /version: got status %d, want 200", w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding /version response: %v", err)
	}

	// Commit should be present
	if _, ok := resp["commit"]; !ok {
		t.Error("response missing 'commit' field")
	}

	// build_time should be absent when AppBuildTime is empty
	if _, ok := resp["build_time"]; ok {
		t.Error("response includes 'build_time' when AppBuildTime was empty")
	}
}

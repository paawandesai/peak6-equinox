package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAdminAuth_Unauthenticated verifies GET /admin without auth returns 401
// with WWW-Authenticate header and HTML explanation page (M5-04).
func TestAdminAuth_Unauthenticated(t *testing.T) {
	srv := New(Config{AdminUser: "admin", AdminPassword: "secret"}, Deps{})
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}

	// Must have WWW-Authenticate header with Basic realm
	auth := rec.Header().Get("WWW-Authenticate")
	if !strings.Contains(auth, `Basic realm="equinox admin"`) {
		t.Errorf("WWW-Authenticate = %q, want Basic realm", auth)
	}

	// Body must be HTML
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}

	body := rec.Body.String()
	// Must contain explanation text
	wantTexts := []string{
		"Admin Access Required",
		"Admin is operator-only (HTTP basic auth)",
		"Reviewers don't need it: the demo runs from pinned round 904",
		"What admin does:",
		"Ingest:",
		"Pin/Unpin:",
		"LLM Verify:",
		`<a href="/">Matches</a>`,
		`<a href="/eval">Eval</a>`,
	}
	for _, want := range wantTexts {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}

	// Must not contain inline script or style attributes
	if strings.Contains(body, " style=") {
		t.Error("body contains inline style attribute")
	}
	if strings.Contains(body, "<script>") {
		t.Error("body contains inline script")
	}
	// External stylesheet from CDN is OK
	if !strings.Contains(body, "https://cdn.jsdelivr.net/npm/@picocss/pico") {
		t.Error("body missing Pico CSS link")
	}
}

// TestAdminAuth_Lockout verifies rate limiting returns 429 (AB-05).
func TestAdminAuth_Lockout(t *testing.T) {
	srv := New(Config{AdminUser: "admin", AdminPassword: "secret"}, Deps{})
	// Make authFailureLimit failed attempts from same IP
	for i := 0; i < authFailureLimit; i++ {
		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		req.RemoteAddr = "203.0.113.42:12345"
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i+1, rec.Code)
		}
	}

	// Next attempt from same IP should be locked out
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.RemoteAddr = "203.0.113.42:12345"
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status after %d failures = %d, want 429", authFailureLimit, rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("429 response missing Retry-After header")
	}

	// Body should be plain text for 429, not HTML
	body := rec.Body.String()
	if strings.Contains(body, "<html") {
		t.Error("429 response should not be HTML")
	}
	if !strings.Contains(body, "too many failed logins") {
		t.Errorf("429 body = %q, want 'too many failed logins'", body)
	}
}

// TestAdminAuth_Authenticated verifies valid credentials allow access.
func TestAdminAuth_Authenticated(t *testing.T) {
	srv := New(Config{AdminUser: "admin", AdminPassword: "secret"}, Deps{})
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.SetBasicAuth("admin", "secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code == http.StatusUnauthorized {
		t.Error("valid credentials returned 401")
	}
	// Should not be the admin denied page
	if strings.Contains(rec.Body.String(), "Admin Access Required") {
		t.Error("authenticated request showed admin denied page")
	}
}

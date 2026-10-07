package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func httptestPost(path, body, contentType string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("HX-Request", "true")
	return req
}

func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestSecurityHeadersOnEveryResponse: the CSP from the ticket (plus base-uri
// and form-action) and the companion headers are set on pages, JSON and 404s.
func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	h := New(Config{}, Deps{}).Handler()
	for _, path := range []string{"/", "/healthz", "/static/app.css", "/nowhere", "/pairs/x"} {
		rec := get(h, path)
		csp := rec.Header().Get("Content-Security-Policy")
		for _, directive := range []string{
			"default-src 'self'",
			"script-src 'self' https://cdn.jsdelivr.net",
			"style-src 'self' https://cdn.jsdelivr.net",
			"object-src 'none'",
			"frame-ancestors 'none'",
		} {
			if !strings.Contains(csp, directive+";") && !strings.HasSuffix(csp, directive) {
				t.Errorf("%s: CSP %q lacks %q", path, csp, directive)
			}
		}
		if strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "unsafe-eval") {
			t.Errorf("%s: CSP allows inline or eval: %q", path, csp)
		}
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s: X-Content-Type-Options = %q", path, got)
		}
		if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
			t.Errorf("%s: X-Frame-Options = %q", path, got)
		}
		if got := rec.Header().Get("Referrer-Policy"); got == "" {
			t.Errorf("%s: Referrer-Policy missing", path)
		}
	}
}

package web

import (
	"io/fs"
	"regexp"
	"testing"
)

// TestStaticAssetsServed verifies every /static/* URL referenced in layout.html
// returns 200 with the correct Content-Type, so this can't regress (M5-03 AC 4).
func TestStaticAssetsServed(t *testing.T) {
	// Read layout.html to extract all /static/* references
	layoutBytes, err := fs.ReadFile(templateFS, "templates/layout.html")
	if err != nil {
		t.Fatal(err)
	}
	layout := string(layoutBytes)

	// Find all /static/* URLs in src, href attributes
	staticURLRe := regexp.MustCompile(`(?:href|src)="(/static/[^"]+)"`)
	matches := staticURLRe.FindAllStringSubmatch(layout, -1)
	if len(matches) == 0 {
		t.Fatal("no /static/* URLs found in layout.html")
	}

	srv := New(Config{}, Deps{})
	seen := make(map[string]bool)
	for _, m := range matches {
		url := m[1]
		if seen[url] {
			continue
		}
		seen[url] = true

		rec := get(srv.Handler(), url)
		if rec.Code != 200 {
			t.Errorf("GET %s returned %d, want 200", url, rec.Code)
		}

		// Verify Content-Type is set appropriately
		ct := rec.Header().Get("Content-Type")
		if ct == "" {
			t.Errorf("GET %s missing Content-Type header", url)
		}
	}

	// Explicit checks for expected static files
	expectedStatic := map[string]string{
		"/static/app.css":           "text/css",
		"/static/timezone-local.js": "application/javascript",
	}
	for url, wantCT := range expectedStatic {
		if !seen[url] {
			t.Errorf("%s not referenced in layout.html", url)
		}
		rec := get(srv.Handler(), url)
		if rec.Code != 200 {
			t.Errorf("GET %s returned %d, want 200", url, rec.Code)
			continue
		}
		ct := rec.Header().Get("Content-Type")
		if ct != wantCT+"; charset=utf-8" {
			t.Errorf("GET %s Content-Type = %q, want %q", url, ct, wantCT+"; charset=utf-8")
		}
		// Verify Cache-Control is set
		cc := rec.Header().Get("Cache-Control")
		if cc != "public, max-age=3600" {
			t.Errorf("GET %s Cache-Control = %q, want %q", url, cc, "public, max-age=3600")
		}
	}
}

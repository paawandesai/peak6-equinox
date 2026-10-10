package web

import (
	"bytes"
	"io/fs"
	"net/http"
	"strings"
	"testing"
)

// TestIdentityAssetsServed: the favicons, the home-screen icon and the
// link-preview card return 200 with their content type and a one-year
// immutable cache, without a database (PAGE 6).
func TestIdentityAssetsServed(t *testing.T) {
	srv := New(Config{}, Deps{})
	for path, want := range map[string]struct {
		contentType string
		magic       []byte
	}{
		"/favicon.ico":          {"image/x-icon", []byte{0, 0, 1, 0}},
		"/favicon.svg":          {"image/svg+xml", []byte("<svg")},
		"/apple-touch-icon.png": {"image/png", []byte("\x89PNG")},
		"/static/og-card.png":   {"image/png", []byte("\x89PNG")},
	} {
		rec := get(srv.Handler(), path)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, rec.Code)
			continue
		}
		if ct := rec.Header().Get("Content-Type"); ct != want.contentType {
			t.Errorf("GET %s Content-Type = %q, want %q", path, ct, want.contentType)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != identityCacheControl {
			t.Errorf("GET %s Cache-Control = %q, want %q", path, cc, identityCacheControl)
		}
		if !bytes.HasPrefix(rec.Body.Bytes(), want.magic) {
			t.Errorf("GET %s body does not start with %q", path, want.magic)
		}
		if csp := rec.Header().Get("Content-Security-Policy"); csp != contentSecurityPolicy {
			t.Errorf("GET %s CSP = %q, want the unchanged site CSP", path, csp)
		}
	}
}

// TestFaviconSVGHasNoScriptOrStyle: the SVG mark uses presentation
// attributes only, so the site CSP (no inline style) never blocks it.
func TestFaviconSVGHasNoScriptOrStyle(t *testing.T) {
	svg := strings.ToLower(string(faviconSVG))
	for _, bad := range []string{"<script", "<style", "style=", " on"} {
		if strings.Contains(svg, bad) {
			t.Errorf("favicon.svg contains %q", bad)
		}
	}
}

// TestLayoutLinksIdentity: every page links the favicons, the touch icon,
// theme colours and Open Graph tags (PAGE 6).
func TestLayoutLinksIdentity(t *testing.T) {
	layout, err := fs.ReadFile(templateFS, "templates/layout.html")
	if err != nil {
		t.Fatal(err)
	}
	wantContains(t, string(layout),
		`<link rel="icon" href="/favicon.svg" type="image/svg+xml">`,
		`<link rel="icon" href="/favicon.ico" sizes="32x32">`,
		`<link rel="apple-touch-icon" href="/apple-touch-icon.png">`,
		`<meta name="theme-color" content="#ffffff" media="(prefers-color-scheme: light)">`,
		`<meta name="theme-color" content="#13171f" media="(prefers-color-scheme: dark)">`,
		`<meta property="og:title" content="{{.Title}} · Equinox">`,
		`<meta property="og:image" content="/static/og-card.png">`,
		`<meta name="twitter:card" content="summary_large_image">`,
	)

	body := get(New(Config{}, Deps{}).Handler(), "/glossary").Body.String()
	wantContains(t, body,
		`<meta property="og:title" content="Glossary · Equinox">`,
		`<meta property="og:description" content="Equinox finds contracts on Kalshi and Polymarket that pay out on the same thing`,
		`<details class="nav-menu">`, `<summary>Menu</summary>`,
	)
}

// TestLayoutDescriptionOverride: a page's Description replaces the site
// default in the description and og:description tags.
func TestLayoutDescriptionOverride(t *testing.T) {
	var buf bytes.Buffer
	data := glossaryPage{page: page{Title: "Decision #96", Description: "Routed to Polymarket: $0.80 for 100 contracts.", Assets: pinnedAssets}}
	if err := pages["glossary"].ExecuteTemplate(&buf, "layout", data); err != nil {
		t.Fatal(err)
	}
	wantContains(t, buf.String(),
		`<meta name="description" content="Routed to Polymarket: $0.80 for 100 contracts.">`,
		`<meta property="og:description" content="Routed to Polymarket: $0.80 for 100 contracts.">`,
	)
}

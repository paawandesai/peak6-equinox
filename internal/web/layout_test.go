package web

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// TestLayoutAsOfLine verifies the .asof line is present with a timezone abbreviation (UI-02).
func TestLayoutAsOfLine(t *testing.T) {
	srv, st := newTestServer(t)
	seedPublic(t, st)
	body := get(srv.Handler(), "/").Body.String()

	// Extract the .asof line content
	asofPattern := regexp.MustCompile(`<p class="asof">([^<]+)</p>`)
	matches := asofPattern.FindStringSubmatch(body)
	if len(matches) < 2 {
		t.Fatal("layout missing <p class=\"asof\"> element or content")
	}
	asofContent := matches[1]

	// The .asof line should contain "Data as of" and a timezone abbreviation (ET/CT/UTC)
	if !strings.Contains(asofContent, "Data as of") {
		t.Errorf(".asof line missing 'Data as of' text, got: %q", asofContent)
	}
	// Check for a timezone abbreviation (ET, CT, UTC, or EDT/EST/CDT/CST) within the .asof content
	tzPattern := regexp.MustCompile(`(ET|CT|UTC|EDT|EST|CDT|CST)`)
	if !tzPattern.MatchString(asofContent) {
		t.Errorf(".asof line missing timezone abbreviation, got: %q", asofContent)
	}
}

// TestLayoutStaleWarningBanner verifies a stale warning renders as .banner with its text (UI-02).
func TestLayoutStaleWarningBanner(t *testing.T) {
	// Test by rendering a page with StaleWarning set directly (per Cursor review: POST /route or set page.StaleWarning directly)
	// Use the error page template which has a simple structure
	tmpl := pages["error"]
	type errorPage struct {
		page
		Status  int
		Message string
	}
	p := errorPage{
		page: page{
			Title:        "Test",
			Question:     "",
			Assets:       pinnedAssets,
			Round:        &roundInfo{ID: 1, Status: "ok", AsOf: "2026-10-08 01:35:52 ET"},
			StaleWarning: "Data is 45 minutes old.",
			Timezone:     "ET",
		},
		Status:  200,
		Message: "Test",
	}
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "layout", p); err != nil {
		t.Fatal(err)
	}
	body := buf.String()

	// Stale warning should render as .banner
	if !strings.Contains(body, `<div class="banner`) {
		t.Error("stale warning not rendered as .banner")
	}
	// Should contain the stale text
	if !strings.Contains(body, "Data is 45 minutes old") {
		t.Error("stale warning banner missing expected text")
	}
}

// TestLayoutNoInlineScriptOrStyle verifies no style attribute or inline script exists (UI-02, AB-07).
func TestLayoutNoInlineScriptOrStyle(t *testing.T) {
	srv, st := newTestServer(t)
	seedPublic(t, st)
	body := get(srv.Handler(), "/").Body.String()

	// No style attribute (CSP forbids inline styles)
	styleAttrRe := regexp.MustCompile(`(?i)<[a-z][^>]*\sstyle\s*=`)
	if m := styleAttrRe.FindString(body); m != "" {
		t.Errorf("found inline style attribute: %s", m)
	}

	// No inline event handlers (on*=)
	inlineHandlerRe := regexp.MustCompile(`(?i)<[a-z][^>]*\son[a-z]+\s*=`)
	if m := inlineHandlerRe.FindString(body); m != "" {
		t.Errorf("found inline event handler: %s", m)
	}
}

// TestLayoutCDNAssetsSRI verifies both CDN assets keep their SRI hashes (UI-02, reuses templates_test.go assertion).
func TestLayoutCDNAssetsSRI(t *testing.T) {
	srv, st := newTestServer(t)
	seedPublic(t, st)
	body := get(srv.Handler(), "/").Body.String()

	// Pico CSS with SRI
	wantContains(t, body,
		`<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/@picocss/pico@2.1.1/css/pico.classless.min.css" integrity="sha384-`,
	)
	// htmx with SRI
	wantContains(t, body,
		`<script src="https://cdn.jsdelivr.net/npm/htmx.org@2.0.11/dist/htmx.min.js" integrity="sha384-`,
		`crossorigin="anonymous"`,
	)
}

// TestLayoutDemoBanner verifies the demo mode banner renders with role="status" (UI-02, M3-07).
func TestLayoutDemoBanner(t *testing.T) {
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)
	sd := seedPublic(t, st)
	ctx := context.Background()
	if err := st.PinRound(ctx, sd.round.ID); err != nil {
		t.Fatal(err)
	}
	srv := New(Config{MinRouteConfidence: dec("0.9")}, Deps{Store: st, Now: func() time.Time { return seedNow.Add(3 * time.Minute) }})
	body := get(srv.Handler(), "/").Body.String()

	// Demo banner should have role="status"
	if !strings.Contains(body, `role="status"`) {
		t.Error("demo mode banner missing role=status")
	}
	if !strings.Contains(body, "Demo mode") {
		t.Error("demo mode banner missing 'Demo mode' text")
	}
}

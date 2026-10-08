package web

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// TestLayoutStamp verifies the stamp template is used with freshness badge, time, and age (UI-10).
func TestLayoutStamp(t *testing.T) {
	srv, st := newTestServer(t)
	seedPublic(t, st)
	body := get(srv.Handler(), "/").Body.String()

	// Stamp should contain a freshness badge (Fresh/Stale/Unknown/Pinned)
	badgePattern := regexp.MustCompile(`<span class="badge [fb]-[a-z]+">`)
	if !badgePattern.MatchString(body) {
		t.Error("stamp missing freshness badge")
	}

	// Stamp should have "as of" with <time datetime>
	if !strings.Contains(body, "as of") {
		t.Error("stamp missing 'as of' text")
	}
	timePattern := regexp.MustCompile(`<time datetime="[^"]+">`)
	if !timePattern.MatchString(body) {
		t.Error("stamp missing <time datetime> element")
	}

	// Stamp should have age ("s ago", "min ago", etc.)
	agePattern := regexp.MustCompile(`\d+\s+(s|min|h|days)\s+ago`)
	if !agePattern.MatchString(body) {
		t.Error("stamp missing age text")
	}

	// Check for a timezone abbreviation (ET, CT, UTC, EDT/EST/CDT/CST) in the <time> element
	timePattern := regexp.MustCompile(`<time[^>]*>[^<]*(ET|CT|UTC|EDT|EST|CDT|CST|MST|MDT|PST|PDT)[^<]*</time>`)
	if !timePattern.MatchString(body) {
		t.Error("stamp missing timezone abbreviation in <time> element")
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

// UI-10: Banner glyphs

// TestLayoutBannerGlyphs verifies banners gain glyphs (UI-10 AC): demo ◆ info, no-data ? warn, stale ✕ danger.
func TestLayoutBannerGlyphs(t *testing.T) {
	// Demo mode banner (pinned round)
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)
	sd := seedPublic(t, st)
	ctx := context.Background()
	if err := st.PinRound(ctx, sd.round.ID); err != nil {
		t.Fatal(err)
	}
	srv := New(Config{MinRouteConfidence: dec("0.9")}, Deps{Store: st, Now: func() time.Time { return seedNow.Add(3 * time.Minute) }})
	body := get(srv.Handler(), "/").Body.String()

	// Demo banner should have ◆ glyph with aria-hidden
	if !strings.Contains(body, `<span class="g" aria-hidden="true">◆</span>`) {
		t.Error("demo banner missing ◆ glyph with aria-hidden")
	}
	if !strings.Contains(body, `banner-info`) {
		t.Error("demo banner should use banner-info class")
	}

	// Stale banner (set StaleWarning directly)
	tmpl := pages["error"]
	type errorPage struct {
		page
		Status  int
		Message string
	}
	p := errorPage{
		page: page{
			Title:        "Test",
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
	staleBody := buf.String()

	// Stale banner should have ✕ glyph with aria-hidden and banner-danger class
	if !strings.Contains(staleBody, `<span class="g" aria-hidden="true">✕</span>`) {
		t.Error("stale banner missing ✕ glyph with aria-hidden")
	}
	if !strings.Contains(staleBody, `banner-danger`) {
		t.Error("stale banner should use banner-danger class")
	}

	// No-data banner (no round available - empty database)
	st2 := store.New(tdb.Pool(t))
	tdb.Reset(t) // Reset to empty database
	srv2 := New(Config{MinRouteConfidence: dec("0.9")}, Deps{Store: st2, Now: func() time.Time { return seedNow }})
	noDataBody := get(srv2.Handler(), "/").Body.String()

	// No-data banner should have ? glyph with aria-hidden and banner-warn class
	if !strings.Contains(noDataBody, `<span class="g" aria-hidden="true">?</span>`) {
		t.Error("no-data banner missing ? glyph with aria-hidden")
	}
	if !strings.Contains(noDataBody, `banner-warn`) {
		t.Error("no-data banner should use banner-warn class")
	}
	if !strings.Contains(noDataBody, "No data available yet") {
		t.Error("no-data banner missing expected text")
	}
}

// TestLayoutContextBar verifies the context bar (UI-10 AC): stamp + round + timezone form.
func TestLayoutContextBar(t *testing.T) {
	srv, st := newTestServer(t)
	seedPublic(t, st)
	body := get(srv.Handler(), "/").Body.String()

	// Context bar should exist
	if !strings.Contains(body, `<div class="context-bar">`) {
		t.Error("layout missing context-bar")
	}
	// Should contain the stamp (.stamp span with freshness badge)
	if !strings.Contains(body, `<span class="stamp">`) {
		t.Error("context bar missing .stamp element")
	}
	if !strings.Contains(body, "as of") {
		t.Error("stamp missing 'as of' text")
	}
	// Should contain "round N"
	if !strings.Contains(body, "round ") {
		t.Error("context bar missing round text")
	}
	// Should contain timezone selector
	if !strings.Contains(body, `<form method="post" action="/timezone" class="timezone-selector">`) {
		t.Error("context bar missing timezone selector")
	}
}

// TestLayoutHeaderNavAriaCurrent verifies nav keeps aria-current (UI-10 AC).
func TestLayoutHeaderNavAriaCurrent(t *testing.T) {
	srv, st := newTestServer(t)
	seedPublic(t, st)
	body := get(srv.Handler(), "/").Body.String()

	// The matches page should have aria-current on its nav link
	if !strings.Contains(body, `<a href="/" aria-current="page">Matches</a>`) {
		t.Error("matches nav link missing aria-current=page")
	}
}

package web

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

var ctx = context.Background()

// TestLayoutAsOfLine verifies the .asof line is present with a timezone abbreviation (UI-02).
func TestLayoutAsOfLine(t *testing.T) {
	srv, st := newTestServer(t)
	seedPublic(t, st)
	body := get(srv.Handler(), "/").Body.String()

	// The .asof line should contain "Data as of" and a timezone abbreviation (ET/CT/UTC)
	if !strings.Contains(body, `class="asof"`) {
		t.Error("layout missing .asof element")
	}
	if !strings.Contains(body, "Data as of") {
		t.Error(".asof line missing 'Data as of' text")
	}
	// Check for a timezone abbreviation (ET, CT, UTC, or EDT/EST/CDT/CST)
	tzPattern := regexp.MustCompile(`(ET|CT|UTC|EDT|EST|CDT|CST)`)
	if !tzPattern.MatchString(body) {
		t.Error(".asof line missing timezone abbreviation")
	}
}

// TestLayoutStaleWarningBanner verifies a stale warning renders as .banner with its text (UI-02).
func TestLayoutStaleWarningBanner(t *testing.T) {
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)
	// Seed a round finished at seedNow+3m, then advance clock to seedNow+48m (45 min after finish) → stale with 30 min threshold
	_ = seedPublic(t, st)
	deps := Deps{
		Store: st,
		Now:   func() time.Time { return seedNow.Add(48 * time.Minute) },
	}
	srv := New(Config{StaleRoundWarnMinutes: 30, MinRouteConfidence: dec("0.9")}, deps)
	body := get(srv.Handler(), "/").Body.String()

	// Debug: print what we got
	if testing.Verbose() {
		t.Logf("Body snippet: %s", body[0:min(1000, len(body))])
	}

	// Stale warning should render as .banner with role="alert"
	if !strings.Contains(body, `<div class="banner" role="alert">`) {
		t.Errorf("stale warning not rendered as .banner with role=alert (has .banner: %v)", strings.Contains(body, "banner"))
	}
	// Should contain the stale text (produced by stale_warning.go)
	if !strings.Contains(body, "minutes old") {
		t.Errorf("stale warning banner missing expected text (has 'old': %v)", strings.Contains(body, "old"))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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

	// Script tags must only be the pinned htmx with SRI
	scriptTagRe := regexp.MustCompile(`(?i)<script\b[^>]*>`)
	for _, tag := range scriptTagRe.FindAllString(body, -1) {
		if !strings.Contains(tag, `src="{{.Assets.HtmxURL}}"`) &&
			!strings.Contains(tag, htmxURL) {
			t.Errorf("unexpected script tag (not pinned htmx): %s", tag)
		}
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
	srv, st := newTestServer(t)
	sd := seedPublic(t, st)
	if err := st.PinRound(ctx, sd.round.ID); err != nil {
		t.Fatal(err)
	}
	body := get(srv.Handler(), "/").Body.String()

	// Demo banner should have role="status"
	if !strings.Contains(body, `<div class="banner" role="status">`) {
		t.Error("demo mode banner missing or not using role=status")
	}
	if !strings.Contains(body, "Demo mode") {
		t.Error("demo mode banner missing 'Demo mode' text")
	}
}

package security

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/web"
)

// Hostile external text (threat-model AB-07): a venue title, rules text,
// market ID, term values, a veto detail and an explanation that would run if
// any page rendered them unescaped.
const (
	xssTitle   = `<script>alert('title')</script>Bitcoin above $120k?`
	xssRules   = `Resolves YES <img src=x onerror=alert('rules')> if "BTC" > 120k`
	xssMarket  = `"><svg/onload=alert(1)>`
	xssSource  = `javascript:alert('source')`
	xssVeto    = `<script>alert('veto')</script> deadline differs`
	xssExplain = `</td></tr></table><script>alert('explain')</script>`
	xssPolyID  = "polymarket:7070707070"
	xssKalID   = "kalshi:AB07-XSS"
)

var (
	// rawTagRe finds every real (unescaped) HTML tag in a response body.
	rawTagRe = regexp.MustCompile(`<[a-zA-Z][^>]*>`)
	// handlerAttrRe matches an inline event handler inside a tag.
	handlerAttrRe = regexp.MustCompile(`(?i)\son[a-z]+\s*=`)
)

func seedAB07(t *testing.T, s *store.Store) int64 {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 10, 6, 17, 0, 0, 0, time.UTC)
	r, err := s.CreateRound(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRound(ctx, r.ID, store.RoundOK, nil, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	threshold := decimal.NewFromInt(120000)
	mk := func(id string, v canonical.Venue, fee canonical.FeeSchedule) canonical.Instrument {
		return canonical.Instrument{
			ID: id, Venue: v, VenueMarketID: xssMarket, VenueEventID: xssMarket, Category: canonical.CategoryCrypto,
			Title: xssTitle, RulesText: xssRules, Status: canonical.StatusOpen, FeeSchedule: fee, CloseTime: now.Add(time.Hour),
			Terms: canonical.Terms{Subject: "BTC", Metric: "price", Comparator: canonical.ComparatorGTE, Threshold: &threshold,
				Unit: "USD", DeadlineTZ: "UTC", ResolutionSource: xssSource, EdgeRules: xssRules, Polarity: canonical.PolarityPositive},
		}
	}
	a := mk(xssKalID, canonical.VenueKalshi, canonical.FeeSchedule{ID: canonical.FeeScheduleKalshiTakerV1, Params: map[string]decimal.Decimal{"multiplier": decimal.RequireFromString("0.07")}})
	b := mk(xssPolyID, canonical.VenuePolymarket, canonical.FeeSchedule{ID: canonical.FeeSchedulePolymarketV1, Params: map[string]decimal.Decimal{"rate": decimal.RequireFromString("0.07")}})
	for _, in := range []canonical.Instrument{a, b} {
		if err := s.UpsertInstrument(ctx, r.ID, in); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{a.ID, b.ID} {
		q := canonical.Quote{InstrumentID: id, FetchedAt: now,
			YesBids: []canonical.Level{{Price: decimal.RequireFromString("0.48"), Size: decimal.NewFromInt(100)}},
			YesAsks: []canonical.Level{{Price: decimal.RequireFromString("0.52"), Size: decimal.NewFromInt(100)}}}
		if err := s.InsertQuote(ctx, r.ID, q); err != nil {
			t.Fatal(err)
		}
	}
	id, err := s.UpsertRelationship(ctx, canonical.Relationship{
		RoundID: r.ID, AID: a.ID, BID: b.ID, Type: canonical.RelEquivalent, Confidence: decimal.RequireFromString("0.95"),
		Method:      canonical.MethodRules,
		Diff:        []canonical.FieldDiff{{Field: "resolution_source", A: xssSource, B: xssRules, Status: canonical.DiffDiffer}},
		Vetoes:      []canonical.Veto{{Code: "deadline_gap", Field: "deadline", Detail: xssVeto}},
		FactsUsed:   []canonical.FactUse{{Key: xssMarket, Value: xssTitle, Source: xssSource, AsOf: now}},
		BasisRisk:   &canonical.BasisRisk{Score: decimal.RequireFromString("0.2"), Components: []canonical.BasisComponent{{Name: xssMarket, Score: decimal.RequireFromString("0.2"), Detail: xssRules}}},
		Explanation: xssExplain,
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// checkEscaped asserts that no real tag in body is a script (other than the
// pinned htmx) or carries an event handler, and that the hostile strings
// appear only in escaped form.
func checkEscaped(t *testing.T, page, body string, escaped ...string) {
	t.Helper()
	for _, tag := range rawTagRe.FindAllString(body, -1) {
		lower := strings.ToLower(tag)
		if strings.HasPrefix(lower, "<script") && !strings.Contains(tag, "https://cdn.jsdelivr.net/npm/htmx.org@") {
			t.Errorf("%s: unexpected script tag: %s", page, tag)
		}
		if handlerAttrRe.MatchString(tag) {
			t.Errorf("%s: tag with inline handler: %s", page, tag)
		}
		if strings.HasPrefix(lower, "<svg") || strings.HasPrefix(lower, "<img") {
			t.Errorf("%s: injected tag rendered raw: %s", page, tag)
		}
		if strings.Contains(lower, "javascript:") {
			t.Errorf("%s: javascript: URL in a tag: %s", page, tag)
		}
	}
	for _, raw := range []string{"<script>alert", "<img src=x onerror=", "<svg/onload", "</td></tr></table>"} {
		if strings.Contains(body, raw) {
			t.Errorf("%s: raw hostile markup %q reached the page", page, raw)
		}
	}
	for _, e := range escaped {
		if !strings.Contains(body, e) {
			t.Errorf("%s: expected escaped text %q in the page", page, e)
		}
	}
}

func checkCSP(t *testing.T, page string, h http.Header) {
	t.Helper()
	csp := h.Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'self'", "script-src 'self' https://cdn.jsdelivr.net", "style-src 'self' https://cdn.jsdelivr.net",
		"object-src 'none'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("%s: CSP %q lacks %q", page, csp, want)
		}
	}
	if strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "unsafe-eval") {
		t.Errorf("%s: CSP %q permits inline script or eval", page, csp)
	}
	if h.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("%s: X-Content-Type-Options missing", page)
	}
}

// TestAB07_HostileTextRendersEscaped: a title, rules text, market ID, term
// values, veto, fact, basis component and explanation containing <script>,
// onerror= and javascript: render escaped on the matches page, the pair page
// and the routing trace, every response carries the CSP, and no inline script
// exists anywhere.
func TestAB07_HostileTextRendersEscaped(t *testing.T) {
	pool := tdb.Pool(t)
	tdb.Reset(t)
	s := store.New(pool)
	relID := seedAB07(t, s)
	h := web.New(web.Config{MinRouteConfidence: decimal.RequireFromString("0.9")}, web.Deps{Store: s}).Handler()

	pairPath := "/pairs/" + strconv.FormatInt(relID, 10)
	// The matches page shows titles; the pair page also shows rules, market
	// IDs, term values, vetoes, facts, basis components and the explanation.
	expect := map[string][]string{
		"/": {"&lt;script&gt;alert(&#39;title&#39;)&lt;/script&gt;"},
		pairPath: {"&lt;script&gt;alert(&#39;title&#39;)&lt;/script&gt;", "&lt;img src=x onerror=alert(&#39;rules&#39;)&gt;", "&lt;svg/onload=alert(1)&gt;",
			"&lt;script&gt;alert(&#39;veto&#39;)&lt;/script&gt;", "&lt;/td&gt;&lt;/tr&gt;&lt;/table&gt;&lt;script&gt;alert(&#39;explain&#39;)"},
	}
	for _, path := range []string{"/", pairPath} {
		rec := webGet(h, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status %d\n%s", path, rec.Code, rec.Body.String())
		}
		checkCSP(t, path, rec.Header())
		checkEscaped(t, path, rec.Body.String(), expect[path]...)
	}

	form := url.Values{"pair": {strconv.FormatInt(relID, 10)}, "target": {xssKalID}, "side": {"buy"}, "outcome": {"yes"},
		"quantity": {"10"}, "type": {"market"}, "tif": {"IOC"}}
	for _, htmx := range []bool{true, false} {
		rec := webPost(h, "/route", form, htmx)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST /route (htmx=%v): status %d\n%s", htmx, rec.Code, rec.Body.String())
		}
		checkCSP(t, "/route", rec.Header())
		body := rec.Body.String()
		for _, tag := range rawTagRe.FindAllString(body, -1) {
			if handlerAttrRe.MatchString(tag) || (strings.HasPrefix(strings.ToLower(tag), "<script") && !strings.Contains(tag, "htmx.org@")) {
				t.Errorf("/route: hostile tag: %s", tag)
			}
		}
	}

	// Hostile form values are never echoed: the validation messages are fixed text.
	hostile := url.Values{"pair": {`<script>alert(1)</script>`}, "target": {`"><img src=x onerror=alert(1)>`}, "side": {`<b>buy</b>`},
		"outcome": {"yes"}, "quantity": {"<script>"}, "type": {"market"}, "tif": {"IOC"}}
	rec := webPost(h, "/route", hostile, true)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("hostile form: status %d, want 400", rec.Code)
	}
	checkCSP(t, "/route (invalid)", rec.Header())
	for _, raw := range []string{"<script>", "onerror=", "<b>buy</b>", "&lt;script&gt;", "&lt;b&gt;"} {
		if strings.Contains(rec.Body.String(), raw) {
			t.Errorf("validation response echoes input %q", raw)
		}
	}
}

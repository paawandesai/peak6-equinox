package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/match"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

const (
	seedKalshiA   = "kalshi:KXBTCD-26OCT0617-T120000"
	seedPolyB     = "polymarket:12345678901234567890"
	seedKalshiC   = "kalshi:KXETHD-26OCT0617-T4500"
	seedTitleA    = "Bitcoin above $120,000 on Oct 6?"
	seedTitleB    = "Will Bitcoin be above $120,000 on October 6?"
	seedTitleC    = "Ethereum above $4,500 on Oct 6?"
	seedExplainEq = "Same subject, threshold, comparator and deadline; settlement sources differ in name only."
)

var seedNow = time.Date(2026, 10, 6, 17, 0, 0, 0, time.UTC)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// seeded is what seedPublic stored, for assertions.
type seeded struct {
	round   store.Round
	eqID    int64 // equivalent pair A/B
	relID   int64 // related pair A/C with vetoes
	noQuote string
}

// seedInstrument returns an open crypto instrument with parsed terms.
func seedInstrument(id string, v canonical.Venue, title, rules, subject, source string, fee canonical.FeeSchedule) canonical.Instrument {
	threshold := dec("120000")
	deadline := seedNow.Add(24 * time.Hour)
	return canonical.Instrument{
		ID: id, Venue: v, VenueMarketID: strings.SplitN(id, ":", 2)[1], VenueEventID: "evt-" + subject,
		Category: canonical.CategoryCrypto, Title: title, RulesText: rules,
		Terms: canonical.Terms{Subject: subject, Metric: "price", Comparator: canonical.ComparatorGTE, Threshold: &threshold,
			Unit: "USD", Deadline: &deadline, DeadlineTZ: "UTC", ResolutionSource: source, EdgeRules: canonical.Unknown,
			Polarity: canonical.PolarityPositive},
		CloseTime: seedNow.Add(24 * time.Hour), Status: canonical.StatusOpen, FeeSchedule: fee,
	}
}

var (
	kalshiFee = canonical.FeeSchedule{ID: canonical.FeeScheduleKalshiTakerV1, Params: map[string]decimal.Decimal{"multiplier": dec("0.07")}}
	polyFee   = canonical.FeeSchedule{ID: canonical.FeeSchedulePolymarketV1, Params: map[string]decimal.Decimal{"rate": dec("0.07")},
		Verified: true, Source: "https://docs.polymarket.com/trading/fees"}
)

// seedPublic stores one finished round with three instruments, books on A and
// B (none on C), an equivalent pair A/B and a related pair A/C with vetoes.
func seedPublic(t *testing.T, st *store.Store) seeded {
	t.Helper()
	ctx := context.Background()
	round, err := st.CreateRound(ctx, seedNow)
	if err != nil {
		t.Fatal(err)
	}
	// FinishedAt at seedNow+3m: routing snapshot after relationship logs (seedNow+2m).
	if err := st.FinishRound(ctx, round.ID, store.RoundOK, nil, seedNow.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	round, err = st.GetRound(ctx, round.ID)
	if err != nil {
		t.Fatal(err)
	}
	a := seedInstrument(seedKalshiA, canonical.VenueKalshi, seedTitleA, "Resolves YES if BTC/USD on CF Benchmarks is above $120,000 at 5 PM ET.", "BTC", "CF Benchmarks", kalshiFee)
	b := seedInstrument(seedPolyB, canonical.VenuePolymarket, seedTitleB, "Resolves to Yes if Binance BTCUSDT is above 120,000 at noon ET.", "BTC", "Binance", polyFee)
	c := seedInstrument(seedKalshiC, canonical.VenueKalshi, seedTitleC, "Resolves YES if ETH/USD is above $4,500.", "ETH", "CF Benchmarks", kalshiFee)
	for _, in := range []canonical.Instrument{a, b, c} {
		if err := st.UpsertInstrument(ctx, round.ID, in); err != nil {
			t.Fatal(err)
		}
	}
	quotes := []canonical.Quote{
		{InstrumentID: seedKalshiA, YesBids: []canonical.Level{{Price: dec("0.48"), Size: dec("200")}}, YesAsks: []canonical.Level{{Price: dec("0.52"), Size: dec("300")}}, FetchedAt: seedNow.Add(3 * time.Minute)},
		{InstrumentID: seedPolyB, YesBids: []canonical.Level{{Price: dec("0.49"), Size: dec("80")}}, YesAsks: []canonical.Level{{Price: dec("0.50"), Size: dec("50")}, {Price: dec("0.53"), Size: dec("100")}}, FetchedAt: seedNow.Add(3 * time.Minute)},
	}
	for _, q := range quotes {
		if err := st.InsertQuote(ctx, round.ID, q); err != nil {
			t.Fatal(err)
		}
	}
	eqID, err := st.UpsertRelationship(ctx, canonical.Relationship{
		RoundID: round.ID, AID: seedKalshiA, BID: seedPolyB, Type: canonical.RelEquivalent, Confidence: dec("0.97"),
		Method: canonical.MethodRules, Diff: match.Diff(a.Terms, b.Terms), Explanation: seedExplainEq,
		BasisRisk: &canonical.BasisRisk{Score: dec("0.1"), Components: []canonical.BasisComponent{{Name: "source", Score: dec("0.1"), Detail: "Binance vs CF Benchmarks"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	relID, err := st.UpsertRelationship(ctx, canonical.Relationship{
		RoundID: round.ID, AID: seedKalshiA, BID: seedKalshiC, Type: canonical.RelRelated, Confidence: dec("0.40"),
		Method: canonical.MethodRules, Diff: match.Diff(a.Terms, c.Terms),
		Vetoes:      []canonical.Veto{{Code: "subject_mismatch", Field: "subject", Detail: "BTC is not ETH"}},
		Explanation: "Different subject: BTC vs ETH.",
	})
	if err != nil {
		t.Fatal(err)
	}
	return seeded{round: round, eqID: eqID, relID: relID, noQuote: seedKalshiC}
}

// newTestServer builds the server over the package schema with a fixed clock.
func newTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)
	// Now at seedNow+3m: quotes fresh (FetchedAt=seedNow+3m), routing newer than relationships (seedNow+2m).
	s := New(Config{MinRouteConfidence: dec("0.9")}, Deps{Store: st, Now: func() time.Time { return seedNow.Add(3 * time.Minute) }})
	return s, st
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func postForm(h http.Handler, path string, form url.Values, htmx bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func wantContains(t *testing.T, body string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(body, w) {
			t.Errorf("body lacks %q", w)
		}
	}
}

func wantHTML(t *testing.T, rec *httptest.ResponseRecorder, status int) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body:\n%s", rec.Code, status, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q", ct)
	}
	if csp := rec.Header().Get("Content-Security-Policy"); csp != contentSecurityPolicy {
		t.Errorf("CSP = %q", csp)
	}
}

func TestPublicMatchesPage(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	rec := get(s.Handler(), "/")
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()
	wantContains(t, body,
		"Which Kalshi and Polymarket contracts are the same bet",
		`<h2 id="routable">Routable pairs (1)</h2>`, // UI-03: routable section with count
		seedTitleA, seedTitleB, seedTitleC,
		"0.97", "0.40",
		`href="/pairs/`+strconv.FormatInt(sd.eqID, 10)+`"`,
		`href="/pairs/`+strconv.FormatInt(sd.relID, 10)+`"`,
		// UI-03: strip replaces the old paragraph with round info.
		`<dt>Round</dt><dd>`+strconv.FormatInt(sd.round.ID, 10)+`</dd>`,
		`<dt>Pairs kept</dt><dd>2</dd>`,
		`<dt>Routable</dt><dd><a href="#routable">1</a></dd>`,
		`<dt>Related</dt><dd>1</dd>`,
		// UI-03: type chips and confidence words.
		`<span class="chip chip-ok">equivalent</span>`,
		`0.97 high`, // confidence with bucket word
		// UI-03: related is inside <details>.
		`<summary>related (1): same event, different payout; never routed</summary>`,
		`<span class="chip chip-warn">related</span>`,
		`0.40 low`,
		// UI-11: checks column and key.
		`<ul class="checks"`,
		`<div class="checks-key">`,
	)
	// UI-11: assert Pair column with round id.
	wantContains(t, body, `data-label="Pair">`)
	wantContains(t, body, `<small>round `+strconv.FormatInt(sd.round.ID, 10)+`</small>`)
	// UI-03: routable section comes before related details.
	if strings.Index(body, `id="routable"`) > strings.Index(body, `<summary>related`) {
		t.Error("routable section should come before related")
	}
	if strings.Contains(body, `id="type-unrelated"`) {
		t.Error("empty groups must not render")
	}
}

func TestPublicMatchesPrefersPinnedRound(t *testing.T) {
	s, st := newTestServer(t)
	first := seedPublic(t, st)
	ctx := context.Background()
	later, err := st.CreateRound(ctx, seedNow.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRound(ctx, later.ID, store.RoundPartial, nil, seedNow.Add(2*time.Hour+time.Minute)); err != nil {
		t.Fatal(err)
	}
	// The latest finished round is empty: the page says so.
	body := get(s.Handler(), "/").Body.String()
	// UI-03: round is in the strip now.
	wantContains(t, body, `<dt>Round</dt><dd>`+strconv.FormatInt(later.ID, 10)+`</dd>`, "has no stored relationships")
	if strings.Contains(body, seedTitleA) {
		t.Error("latest round shows the earlier round's pairs")
	}
	// Pinning the first round switches the page to it.
	if err := st.PinRound(ctx, first.round.ID); err != nil {
		t.Fatal(err)
	}
	body = get(s.Handler(), "/").Body.String()
	// UI-03: "pinned for the demo" is in the demo banner (UI-02), not in the page content.
	// The strip shows the pinned round's data.
	wantContains(t, body, seedTitleA, `<dt>Round</dt><dd>`+strconv.FormatInt(first.round.ID, 10)+`</dd>`)
}

func TestPublicMatchesNoRound(t *testing.T) {
	s, _ := newTestServer(t)
	rec := get(s.Handler(), "/")
	wantHTML(t, rec, http.StatusOK)
	// UI-03: empty state copy unchanged.
	wantContains(t, rec.Body.String(), "No completed fetch round yet")
}

func TestPublicPagesWithoutStoreAre503(t *testing.T) {
	h := New(Config{}, Deps{}).Handler()
	for _, path := range []string{"/", "/pairs/1"} {
		rec := get(h, path)
		wantHTML(t, rec, http.StatusServiceUnavailable)
		wantContains(t, rec.Body.String(), "not configured")
	}
	rec := postForm(h, "/route", validRouteForm(1, seedKalshiA), true)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("POST /route without a store: status %d, want 503", rec.Code)
	}
}

func TestPublicPairPage(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	rec := get(s.Handler(), "/pairs/"+strconv.FormatInt(sd.eqID, 10))
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()
	wantContains(t, body,
		"Why does Equinox think these two contracts are, or are not, the same bet",
		seedTitleA, seedTitleB, seedExplainEq, "No vetoes.",
		// Diff rows: term, A, B, status.
		`<tr class="diff-match"><td>subject</td><td>BTC</td><td>BTC</td><td>match</td></tr>`,
		`<tr class="diff-differ"><td>resolution_source</td><td>CF Benchmarks</td><td>Binance</td><td>differ</td></tr>`,
		`<tr class="diff-unknown"><td>edge_rules</td>`,
		// Touch on each venue.
		"0.4800", "0.5200", "0.5000", "0.4900", "1/2", "2026-10-06 13:03:00 EDT", // seedNow+3min in ET (M3-08)
		// Route form.
		`action="/route"`, `hx-post="/route"`, `name="pair" value="`+strconv.FormatInt(sd.eqID, 10)+`"`,
		`<option value="`+seedKalshiA+`" selected>`, `<option value="`+seedPolyB+`">`,
		`id="route-result"`,
		"Resolves YES if BTC/USD",
	)

	rec = get(s.Handler(), "/pairs/"+strconv.FormatInt(sd.relID, 10))
	wantHTML(t, rec, http.StatusOK)
	body = rec.Body.String()
	wantContains(t, body, "Vetoes (1): each blocks equivalent", "subject_mismatch", "BTC is not ETH", "Different subject: BTC vs ETH.",
		"No quote for this instrument in the round.", `class="chip chip-warn">related`)
}

func TestPublicPairNotFound(t *testing.T) {
	s, st := newTestServer(t)
	seedPublic(t, st)
	for _, raw := range []string{"999999", "abc", "0", "-1", "01", "1e3", "1;DROP", "1234567890123456789", "9223372036854775808", " 1", "1 "} {
		rec := get(s.Handler(), "/pairs/"+url.PathEscape(raw))
		if rec.Code != http.StatusNotFound {
			t.Errorf("/pairs/%q: status %d, want 404", raw, rec.Code)
		}
		wantContains(t, rec.Body.String(), "No such pair")
	}
}

func TestParseID(t *testing.T) {
	good := map[string]int64{"1": 1, "42": 42, "999999999999999999": 999999999999999999}
	for raw, want := range good {
		if got, ok := parseID(raw); !ok || got != want {
			t.Errorf("parseID(%q) = %d, %v", raw, got, ok)
		}
	}
	for _, raw := range []string{"", "0", "00", "01", "-1", "+1", "1.0", "1e3", "abc", "1a", " 1", "1 ",
		"1234567890123456789", "99999999999999999999", "\x001", "١"} {
		if _, ok := parseID(raw); ok {
			t.Errorf("parseID(%q) accepted", raw)
		}
	}
}

func TestGroupPairsOrdering(t *testing.T) {
	rels := []canonical.Relationship{
		{ID: 3, AID: "kalshi:A", BID: "polymarket:B", Type: canonical.RelRelated, Confidence: dec("0.5")},
		{ID: 1, AID: "kalshi:A", BID: "polymarket:B", Type: canonical.RelEquivalent, Confidence: dec("0.9")},
		{ID: 2, AID: "kalshi:A", BID: "polymarket:C", Type: canonical.RelEquivalent, Confidence: dec("0.95")},
		{ID: 4, AID: "kalshi:A", BID: "polymarket:D", Type: canonical.RelEquivalent, Confidence: dec("0.95")},
	}
	ids := relationshipInstrumentIDs(rels)
	if strings.Join(ids, ",") != "kalshi:A,polymarket:B,polymarket:C,polymarket:D" {
		t.Errorf("ids = %v", ids)
	}
	views := map[string]instrumentView{}
	for _, id := range ids {
		views[id] = missingInstrumentView(id)
	}
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	// UI-11: groupPairs now takes sortOrder, qParam, and roundID.
	groups := groupPairs(rels, views, now, "confidence", "", 1)
	if len(groups) != 2 || groups[0].Type != "equivalent" || groups[1].Type != "related" {
		t.Fatalf("groups = %+v", groups)
	}
	var order []int64
	for _, p := range groups[0].Pairs {
		order = append(order, p.ID)
	}
	if len(order) != 3 || order[0] != 2 || order[1] != 4 || order[2] != 1 {
		t.Errorf("equivalent order = %v, want [2 4 1] (confidence desc, then id)", order)
	}
	// UI-03: check new fields (ConfidenceWord, ChipClass).
	row := groups[0].Pairs[0]
	if !row.A.Missing || row.AsOf != now || row.Confidence != "0.95" || row.ConfidenceWord != "high" || row.ChipClass != "chip-ok" {
		t.Errorf("row = %+v", row)
	}
}

func TestLoadInstrumentViewsMarksMissing(t *testing.T) {
	s, st := newTestServer(t)
	seedPublic(t, st)
	_ = s
	views, err := loadInstrumentViews(context.Background(), st, []string{seedKalshiA, "kalshi:NOPE"})
	if err != nil {
		t.Fatal(err)
	}
	wantClose := time.Date(2026, 10, 7, 17, 0, 0, 0, time.UTC)
	if views[seedKalshiA].Missing || views[seedKalshiA].Title != seedTitleA || views[seedKalshiA].Close != wantClose {
		t.Errorf("A = %+v", views[seedKalshiA])
	}
	if !views["kalshi:NOPE"].Missing || views["kalshi:NOPE"].Venue != "kalshi" {
		t.Errorf("missing = %+v", views["kalshi:NOPE"])
	}
}

func TestStaticCSS(t *testing.T) {
	h := New(Config{}, Deps{}).Handler()
	rec := get(h, "/static/app.css")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/css") || !strings.Contains(rec.Body.String(), ".diff-match") {
		t.Errorf("GET /static/app.css = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	for _, path := range []string{"/static/", "/static/other.css", "/static/app.css/x"} {
		if rec := get(h, path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", path, rec.Code)
		}
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/static/app.css", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /static/app.css: status %d, want 405", rec.Code)
	}
}

func TestPublicRoutesRejectMutations(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	h := s.Handler()
	for _, path := range []string{"/", "/pairs/" + strconv.FormatInt(sd.eqID, 10)} {
		for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(m, path, nil))
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s: status %d, want 405", m, path, rec.Code)
			}
		}
	}
	rec := get(h, "/route")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /route: status %d, want 405", rec.Code)
	}
}

func TestFormatHelpers(t *testing.T) {
	if got := formatUTC(time.Time{}); got != "unknown" {
		t.Errorf("formatUTC(zero) = %q", got)
	}
	if got := formatUTC(time.Date(2026, 10, 6, 1, 2, 3, 0, time.FixedZone("x", 3600))); got != "2026-10-06 00:02:03 UTC" {
		t.Errorf("formatUTC = %q", got)
	}
	if got := nullPrice(decimal.NullDecimal{}); got != "n/a" {
		t.Errorf("nullPrice(null) = %q", got)
	}
	if got := nullPrice(decimal.NullDecimal{Decimal: dec("0.5"), Valid: true}); got != "0.5000" {
		t.Errorf("nullPrice = %q", got)
	}
	info := newRoundInfo(store.Round{ID: 3, StartedAt: seedNow, Status: store.RoundRunning}, TzET)
	wantAsOf := "2026-10-06 13:00:00 EDT" // seedNow (17:00 UTC) in ET (EDT in October)
	if info.AsOf != wantAsOf || info.Status != "running" {
		t.Errorf("roundInfo = %+v", info)
	}
}

// TestTimezoneSelectorSelected verifies that the timezone selector's 'selected' attribute
// follows the cookie value (M3-08 Cursor round 2 finding 4).
func TestTimezoneSelectorSelected(t *testing.T) {
	s, st := newTestServer(t)
	seedPublic(t, st)

	tests := []struct {
		name       string
		cookieVal  string
		wantSelect string
	}{
		{
			name:       "ET cookie shows ET selected",
			cookieVal:  "ET",
			wantSelect: `<option value="ET" selected>ET</option>`,
		},
		{
			name:       "CT cookie shows CT selected",
			cookieVal:  "CT",
			wantSelect: `<option value="CT" selected>CT</option>`,
		},
		{
			name:       "UTC cookie shows UTC selected",
			cookieVal:  "UTC",
			wantSelect: `<option value="UTC" selected>UTC</option>`,
		},
		{
			name:       "no cookie defaults to ET selected",
			cookieVal:  "",
			wantSelect: `<option value="ET" selected>ET</option>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tt.cookieVal != "" {
				//nolint:gosec // G124: Test cookie doesn't need security attributes
				req.AddCookie(&http.Cookie{Name: timezoneCookie, Value: tt.cookieVal})
			}
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.Code)
			}

			body := w.Body.String()
			if !strings.Contains(body, tt.wantSelect) {
				t.Errorf("response does not contain %q", tt.wantSelect)
			}
		})
	}
}

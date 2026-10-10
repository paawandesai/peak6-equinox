package web

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/decisionlog"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// seedMore adds, on top of seedPublic: a second round with quotes for A and
// B (so the pair has history), decision-log entries for both pairs, one
// stored routing decision via POST /route, and a related pair with facts but
// no veto (the December-style near miss).
func seedMore(t *testing.T, s *Server, st *store.Store, sd seeded) (decisionID, factMissID int64) {
	t.Helper()
	ctx := context.Background()
	dlog := decisionlog.New(st).WithClock(func() time.Time { return seedNow.Add(2 * time.Minute) })
	for _, id := range []int64{sd.eqID, sd.relID} {
		rel, err := st.GetRelationship(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := dlog.Relationship(ctx, rel); err != nil {
			t.Fatal(err)
		}
	}
	// A later finished round with another touch on both instruments; the
	// current round stays the pinned/first one because it is pinned.
	if err := st.PinRound(ctx, sd.round.ID); err != nil {
		t.Fatal(err)
	}
	later, err := st.CreateRound(ctx, seedNow.Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []canonical.Quote{
		{InstrumentID: seedKalshiA, YesBids: []canonical.Level{{Price: dec("0.50"), Size: dec("200")}}, YesAsks: []canonical.Level{{Price: dec("0.54"), Size: dec("300")}}, FetchedAt: seedNow.Add(5 * time.Minute)},
		{InstrumentID: seedPolyB, YesAsks: []canonical.Level{{Price: dec("0.51"), Size: dec("50")}}, FetchedAt: seedNow.Add(5 * time.Minute)},
	} {
		if err := st.InsertQuote(ctx, later.ID, q); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.FinishRound(ctx, later.ID, store.RoundOK, nil, seedNow.Add(6*time.Minute)); err != nil {
		t.Fatal(err)
	}
	factMissID, err = st.UpsertRelationship(ctx, canonical.Relationship{
		RoundID: sd.round.ID, AID: seedKalshiC, BID: seedPolyB, Type: canonical.RelRelated, Confidence: dec("0.30"),
		Method: canonical.MethodRules, Explanation: "The December bucket depends on the October decision; fed_next_meeting did not place it.",
		FactsUsed: []canonical.FactUse{{Key: "fed_next_meeting", Value: "2026-10-28", Source: "https://www.federalreserve.gov/monetarypolicy/fomccalendars.htm", AsOf: seedNow}},
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := postForm(s.Handler(), "/route", validRouteForm(sd.eqID, seedKalshiA), true)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /route: %d\n%s", rec.Code, rec.Body.String())
	}
	decs, err := st.ListRoutingDecisions(ctx, 10)
	if err != nil || len(decs) != 1 {
		t.Fatalf("decisions: %v, %d", err, len(decs))
	}
	return decs[0].ID, factMissID
}

func TestNearMissesPage(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	_, factMissID := seedMore(t, s, st, sd)
	rec := get(s.Handler(), "/near-misses")
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()
	wantContains(t, body,
		"Which pairs looked like the same bet but were rejected, and by which rule?",
		"2 near misses", "Stopped by a veto:", "1", "Fact did not close:", "1",
		`href="/pairs/`+strconv.FormatInt(sd.relID, 10)+`"`, `href="/pairs/`+strconv.FormatInt(factMissID, 10)+`"`,
		`<code>subject_mismatch</code>`, "BTC is not ETH", "Different subject: BTC vs ETH.",
		`<code>fact_did_not_close</code>`, "a fact was consulted but did not apply", seedTitleC,
		"Not-a-match record →",
	)
	if strings.Contains(body, seedExplainEq) {
		t.Error("the equivalent pair must not be listed as a near miss")
	}
	// Groups should exist
	if !strings.Contains(body, `id="group-subject-mismatch"`) || !strings.Contains(body, `id="group-fact-did-not-close"`) {
		t.Error("groups should be present")
	}
}

func TestNearMissesEmptyStates(t *testing.T) {
	s, st := newTestServer(t)
	rec := get(s.Handler(), "/near-misses")
	wantHTML(t, rec, http.StatusOK)
	wantContains(t, rec.Body.String(), "No completed fetch round yet")
	ctx := context.Background()
	r, err := st.CreateRound(ctx, seedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRound(ctx, r.ID, store.RoundOK, nil, seedNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	wantContains(t, get(s.Handler(), "/near-misses").Body.String(), "has no stored near misses")
	if rec := get(New(Config{}, Deps{}).Handler(), "/near-misses"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("without a store: %d", rec.Code)
	}
}

func TestNearMissRowsOrdering(t *testing.T) {
	rels := []canonical.Relationship{
		{ID: 1, AID: "kalshi:A", BID: "polymarket:B", Confidence: dec("0.9")},
		{ID: 2, AID: "kalshi:A", BID: "polymarket:C", Confidence: dec("0.5"), Vetoes: []canonical.Veto{{Code: "x", Field: "field", Detail: "detail"}}},
		{ID: 3, AID: "kalshi:A", BID: "polymarket:D", Confidence: dec("0.7"), Vetoes: []canonical.Veto{{Code: "y", Field: "field", Detail: "detail"}}},
		{ID: 4, AID: "kalshi:A", BID: "polymarket:E", Confidence: dec("0.7"), Vetoes: []canonical.Veto{{Code: "z", Field: "field", Detail: "detail"}}},
	}
	views := map[string]instrumentView{}
	rows := nearMissRows(rels, views)
	groups := groupNearMisses(rows, "", false)
	// Should have 4 groups (x, y, z, fact_did_not_close)
	if len(groups) != 4 {
		t.Fatalf("groups = %d, want 4", len(groups))
	}
	// fact_did_not_close should be last
	if groups[3].Code != "fact_did_not_close" {
		t.Errorf("last group = %s, want fact_did_not_close", groups[3].Code)
	}
	// fact_did_not_close should have row 1
	if len(groups[3].Rows) != 1 || groups[3].Rows[0].ID != 1 {
		t.Errorf("fact_did_not_close group should have row 1")
	}
}

func TestDecisionLogPage(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	_, _ = seedMore(t, s, st, sd)
	rec := get(s.Handler(), "/log")
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()
	wantContains(t, body,
		"What did Equinox decide recently, and why?",
		// UI-15: intro in details element (collapses at small viewports)
		`<details class="intro-details" open>`,
		`<summary>About the log</summary>`,
		`href="/pairs/`+strconv.FormatInt(sd.eqID, 10)+`">pair #`+strconv.FormatInt(sd.eqID, 10)+`: kalshi / polymarket</a>`,
		// UI-15: chip-filter links instead of select (M5-11: default is verdicts & routing)
		`<a href="/log" class="chip-filter" aria-current="page">verdicts &amp; routing</a>`,
		`<a href="/log?kind=all" class="chip-filter">all</a>`,
		`<a href="/log?kind=relationship" class="chip-filter">relationship verdicts</a>`,
		`<a href="/log?kind=routing" class="chip-filter">routing decisions</a>`,
		`<span class="chip chip-info">relationship</span>`, `<span class="chip chip-ok">routing</span>`,
		"route buy 100 contracts yes on "+seedKalshiA,
		// M6 P2: a long summary shows once, split into a head and the rest on expand.
		"Same subject, threshold", "differ in name only.", `<details class="log-summary">`, "Latest 3 entries",
	)
	// Newest first: the routing decision (now = seedNow+1h) precedes the verdicts.
	if strings.Index(body, `chip-ok">routing`) > strings.Index(body, `chip-info">relationship`) {
		t.Error("entries are not newest first")
	}

	// UI-15: test routing-specific view with enriched columns
	body = get(s.Handler(), "/log?kind=routing").Body.String()
	wantContains(t, body, "Latest 1 entry",
		// aria-current on the selected kind chip
		`<a href="/log?kind=routing" class="chip-filter" aria-current="page">routing decisions</a>`,
		// Routing table columns (UI-15)
		`<th>When</th>`,
		`<th>About</th>`,
		`<th>Order</th>`,
		`<th>Outcome</th>`,
		`<th class="num">All-in</th>`,
		`<th class="num">Saving vs best ask</th>`,
		// Group-by chips (UI-15)
		`Group by`,
		`href="/log?kind=routing&group=venue"`,
		`href="/log?kind=routing&group=label"`,
		// Sign rule (UI-15)
		`<p class="sign-rule">`,
		`Positive is favourable`,
	)
	if strings.Contains(body, `chip-info">relationship`) {
		t.Error("kind filter did not apply")
	}

	wantContains(t, get(s.Handler(), "/log?kind=llm").Body.String(), "No decision-log entries of kind <code>llm</code> yet")
	if rec := get(s.Handler(), "/log?kind=<script>"); rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "<script>") {
		t.Errorf("unknown kind: %d; body echoes input: %v", rec.Code, strings.Contains(rec.Body.String(), "<script>"))
	}
	if rec := get(New(Config{}, Deps{}).Handler(), "/log"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("without a store: %d", rec.Code)
	}
}

func TestLogEntryLink(t *testing.T) {
	cases := []struct {
		kind       store.LogKind
		ref        string
		href, text string
	}{
		{store.LogRelationship, "12", "/pairs/12", "pair #12"},
		{store.LogLLM, "12", "/pairs/12", "pair #12"},
		{store.LogRouting, "7", "/decisions/7", "decision #7"},
		{store.LogIngest, "3", "", "round 3"},
		{store.LogRelationship, "12; DROP", "", ""},
		{store.LogRouting, "", "", ""},
		{store.LogKind("other"), "1", "", ""},
	}
	for _, c := range cases {
		href, text := logEntryLink(c.kind, c.ref)
		if href != c.href || text != c.text {
			t.Errorf("logEntryLink(%s, %q) = %q, %q; want %q, %q", c.kind, c.ref, href, text, c.href, c.text)
		}
	}
}

func TestDecisionPage(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	decisionID, _ := seedMore(t, s, st, sd)
	rec := get(s.Handler(), "/decisions/"+strconv.FormatInt(decisionID, 10))
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()
	wantContains(t, body,
		"Routing decision #"+strconv.FormatInt(decisionID, 10),
		"What did this simulated order do", "Decision #"+strconv.FormatInt(decisionID, 10),
		"buy 100 contracts YES, market IOC", "Partners considered:</strong> 2", "round "+strconv.FormatInt(sd.round.ID, 10), // UI-13: v2 HTML (v1: "Partners considered: 2")
		`href="/log?kind=routing"`, "Chosen legs", "Explanation",
		// UI-13 v2 elements
		"permalink", "Order and input snapshot", "Eligibility", "Fill simulation and fee math",
	)
	for _, raw := range []string{"999999", "abc", "0"} {
		rec := get(s.Handler(), "/decisions/"+raw)
		if rec.Code != http.StatusNotFound {
			t.Errorf("/decisions/%s: %d", raw, rec.Code)
		}
		wantContains(t, rec.Body.String(), "No such routing decision")
	}
	if rec := get(New(Config{}, Deps{}).Handler(), "/decisions/1"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("without a store: %d", rec.Code)
	}
}

func TestCountPartners(t *testing.T) {
	d := canonical.Decision{TargetID: "kalshi:T",
		Alternatives: []canonical.Alternative{{Legs: []canonical.Leg{{InstrumentID: "kalshi:T"}}}, {Legs: []canonical.Leg{{InstrumentID: "polymarket:P"}}}},
		Exclusions:   []canonical.Exclusion{{InstrumentID: "polymarket:P"}, {InstrumentID: "polymarket:Q"}}}
	if got := countPartners(d); got != 2 {
		t.Errorf("countPartners = %d, want 2 (P and Q; the target is not a partner)", got)
	}
	if got := countPartners(canonical.Decision{TargetID: "kalshi:T"}); got != 0 {
		t.Errorf("countPartners(empty) = %d", got)
	}
}

func TestPairPageShowsHistoryFactsBasisAndLog(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	_, factMissID := seedMore(t, s, st, sd)
	// Give the equivalent pair facts and basis risk to render.
	ctx := context.Background()
	rel, err := st.GetRelationship(ctx, sd.eqID)
	if err != nil {
		t.Fatal(err)
	}
	rel.FactsUsed = []canonical.FactUse{{Key: "fed_upper_bound", Value: "4.00", Source: "https://www.federalreserve.gov/newsevents/pressreleases/monetary20260916a.htm", AsOf: seedNow},
		{Key: "fed_rate_step", Value: "0.25", Source: "FOMC history, not a URL", AsOf: seedNow}}
	rel.BasisRisk = &canonical.BasisRisk{Score: dec("0.55"), Components: []canonical.BasisComponent{
		{Name: "source_difference", Score: dec("0.50"), Detail: "CF Benchmarks vs Binance"},
		{Name: "fact_dependency", Score: dec("0.05"), Detail: "one fact applied"}}}
	if _, err := st.UpsertRelationship(ctx, rel); err != nil {
		t.Fatal(err)
	}
	rec := get(s.Handler(), "/pairs/"+strconv.FormatInt(sd.eqID, 10))
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()
	wantContains(t, body,
		// Facts: the URL source is a link, the free-text source is not.
		"External facts consulted (2)", "<code>fed_upper_bound</code>", `<a href="https://www.federalreserve.gov/newsevents/pressreleases/monetary20260916a.htm" rel="noopener noreferrer">`,
		"<td>FOMC history, not a URL</td>",
		// Basis risk components and verdict panel (UI-12: structured display with <strong>score</strong> <span>word</span>).
		"<strong>0.55</strong>", ">high<", // basis risk in verdict panel
		"Basis risk: 0.55", "<code>source_difference</code></td><td>0.50</td><td>CF Benchmarks vs Binance</td><td>", // 4-column basis table (added "Diff row" column)
		// Full type set.
		"The full set of relationship types", `<li class="type-complement">complement (routable):`, `<li class="type-implies">implies:`,
		// History: two rounds, four touches; polylines for both series.
		`<svg class="hist"`, "4 touches across 2 rounds", `<polyline class="hist-mid hist-a"`, `<polyline class="hist-mid hist-b"`, `<circle class="hist-dot hist-a"`,
		"last mid 0.5200", "last mid 0.4950", "<td>0.5400</td>",
		// Decision-log entries for this pair.
		"Decision-log entries for this pair", "equivalent (0.97, rules): "+seedKalshiA,
	)
	if strings.Contains(body, "near miss") {
		t.Error("an equivalent pair is not a near miss")
	}
	// The related pair with facts and no veto says so, and shows "Not scored".
	body = get(s.Handler(), "/pairs/"+strconv.FormatInt(factMissID, 10)).Body.String()
	wantContains(t, body, `This pair is a <a href="/near-misses">near miss</a>: no veto fired, but the external fact consulted did not close the gap.`,
		"Not scored for this pair", "<code>fed_next_meeting</code>")
	// The vetoed related pair.
	body = get(s.Handler(), "/pairs/"+strconv.FormatInt(sd.relID, 10)).Body.String()
	wantContains(t, body, "near miss</a>: a hard veto fired.", "None: this verdict rests on the contract terms alone.",
		// C has no quotes at all; A has two touches.
		"2 touches across 2 rounds")
}

func TestBuildHistoryChart(t *testing.T) {
	const a, b = "kalshi:A", "polymarket:B"
	if c := buildHistoryChart(a, b, canonical.RelEquivalent, nil, TzET); !c.Empty || c.Width != chartWidth {
		t.Errorf("empty chart = %+v", c)
	}
	np := func(s string) decimal.NullDecimal { return decimal.NullDecimal{Decimal: dec(s), Valid: true} }
	pts := []store.QuotePoint{
		{RoundID: 1, InstrumentID: a, BestBid: np("0.40"), BestAsk: np("0.60"), Mid: np("0.50"), FetchedAt: seedNow},
		{RoundID: 1, InstrumentID: b, BestBid: np("0"), BestAsk: np("1"), Mid: np("0.50"), FetchedAt: seedNow},
		{RoundID: 2, InstrumentID: a, BestAsk: np("0.70"), FetchedAt: seedNow.Add(time.Hour)}, // one-sided: no mid, no bid
	}
	c := buildHistoryChart(a, b, canonical.RelEquivalent, pts, TzET)
	wantFrom := time.Date(2026, 10, 6, 17, 0, 0, 0, time.UTC)
	wantTo := time.Date(2026, 10, 6, 18, 0, 0, 0, time.UTC)
	if c.Empty || c.Points != 3 || c.Rounds != 2 || c.From != wantFrom || c.To != wantTo {
		t.Fatalf("chart = %+v", c)
	}
	if len(c.Series) != 2 || c.Series[0].Key != "a" || c.Series[1].Key != "b" || c.Series[0].Venue != "kalshi" || c.Series[1].Venue != "polymarket" {
		t.Fatalf("series = %+v", c.Series)
	}
	sa, sb := c.Series[0], c.Series[1]
	// A: mid only at the first time (x = left edge), ask at both ends.
	wantMid := "44.0,120.0"
	if sa.Mid != wantMid || sa.N != 2 || sa.Last != "0.5000" {
		t.Errorf("A mid = %q (want %q), N = %d, last = %q", sa.Mid, wantMid, sa.N, sa.Last)
	}
	if !strings.HasPrefix(sa.Ask, "44.0,98.4 708.0,") || sa.Bid != "44.0,141.6" {
		t.Errorf("A ask = %q, bid = %q", sa.Ask, sa.Bid)
	}
	// B: price 0 sits on the bottom edge and 1 on the top edge of the plot.
	if sb.Bid != "44.0,228.0" || sb.Ask != "44.0,12.0" || sb.N != 1 {
		t.Errorf("B bid = %q, ask = %q, N = %d", sb.Bid, sb.Ask, sb.N)
	}
	if len(c.YTicks) != 5 || c.YTicks[0].Label != "0.00" || c.YTicks[4].Label != "1.00" || len(c.XTicks) != 2 ||
		c.XTicks[0].Anchor != "start" || c.XTicks[1].Anchor != "end" || c.XTicks[1].Label != "10-06 18:00 UTC" {
		t.Errorf("ticks = %+v / %+v", c.YTicks, c.XTicks)
	}
	if len(sa.Dots) != 1 || sa.Dots[0] != (chartPt{X: "44.0", Y: "120.0"}) || len(sb.Dots) != 1 {
		t.Errorf("dots = %+v / %+v", sa.Dots, sb.Dots)
	}
	// M6 P5: the table is newest first with both venues interleaved.
	if len(c.Rows) != 3 || c.Rows[0].Mid != "n/a" || c.Rows[0].Ask != "0.7000" || c.Rows[2].Venue != "polymarket" {
		t.Errorf("rows = %+v", c.Rows)
	}
	// A single round is centred and gets one x tick.
	c = buildHistoryChart(a, b, canonical.RelEquivalent, pts[:2], TzET)
	if c.Rounds != 1 || len(c.XTicks) != 1 || c.XTicks[0].Anchor != "middle" || !strings.HasPrefix(c.Series[0].Mid, "376.0,") {
		t.Errorf("single-round chart = %+v", c)
	}
	// Out-of-range values are clamped, never placed off the plot.
	if y := yOf(dec("1.5")); y != chartTop {
		t.Errorf("yOf(1.5) = %v", y)
	}
	if y := yOf(dec("-1")); y != chartHeight-chartBottom {
		t.Errorf("yOf(-1) = %v", y)
	}
}

func writeEval(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "eval_summary.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const sampleEval = `{
  "generated_at": "2026-10-07T12:00:00Z", "command": "equinox eval --round 3", "round_id": 3,
  "labels_file": "testdata/golden/labels.yaml", "labels": 40, "method": "rules",
  "types": [
    {"type": "equivalent", "support": 12, "tp": 12, "fp": 0, "fn": 1, "precision": "1", "recall": 0.9231},
    {"type": "complement", "support": 4, "tp": 3, "fp": 1, "fn": 1, "precision": "0.75", "recall": "0.75"},
    {"type": "implies", "support": 0, "tp": 0, "fp": 0, "fn": 0, "precision": null, "recall": null}
  ],
  "notes": ["3 labels skipped: instrument not in round <b>x</b>"]
}`

func TestEvalPage(t *testing.T) {
	// No file configured, and a configured file that does not exist.
	for _, file := range []string{"", filepath.Join(t.TempDir(), "missing.json")} {
		rec := get(New(Config{EvalSummaryFile: file}, Deps{}).Handler(), "/eval")
		wantHTML(t, rec, http.StatusOK)
		// UI-16: empty state in panel, references Q-7
		wantContains(t, rec.Body.String(), "How accurate is the matcher on human-labelled pairs", "No labels confirmed yet (Q-7)")
	}
	// A valid artifact.
	rec := get(New(Config{EvalSummaryFile: writeEval(t, sampleEval)}, Deps{}).Handler(), "/eval")
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()
	// M3-08: Produced timestamp now formatted in user's timezone, so we check without the exact time
	// UI-16: headline panel with verdict badge, badges for types, F1 column, macro and weighted averages, meter bars
	wantContains(t, body,
		"by <code>equinox eval --round 3</code>: <strong>40</strong> labelled pairs, <strong>16</strong> scored", // P11: no "on round N"
		`<div class="panel panel-info">`, "Precision at equivalent", "100.00%", "meets the 0.95 target", "✓",
		`<span class="badge b-equivalent">`, `≡</span>equivalent`,
		"92.31%", "75.00%", `>n/a<`, // UI-16: percentages are now in meter-cell spans, not bare td.num
		"Macro average", "Weighted by support", "<th>F1</th>",
		`<meter min="0" max="1"`,
		"There is no calibration plot because confidence is a rules score",
		"Notes from the run", "&lt;b&gt;x&lt;/b&gt;",
	)
	if strings.Contains(body, "<b>x</b>") {
		t.Error("note rendered unescaped")
	}
	// Below target.
	low := strings.Replace(sampleEval, `"precision": "1"`, `"precision": "0.80"`, 1)
	wantContains(t, get(New(Config{EvalSummaryFile: writeEval(t, low)}, Deps{}).Handler(), "/eval").Body.String(),
		"below the 0.95 target", "80.00%", "✕")
	// Undefined precision at equivalent.
	undef := strings.Replace(sampleEval, `"precision": "1"`, `"precision": null`, 1)
	wantContains(t, get(New(Config{EvalSummaryFile: writeEval(t, undef)}, Deps{}).Handler(), "/eval").Body.String(),
		"undefined: no equivalent predictions", "?")
	// Broken artifacts: the page says so without echoing file content.
	for name, body := range map[string]string{
		"not json":       `{"generated_at": `,
		"unknown field":  `{"generated_at": "2026-10-07T12:00:00Z", "bogus": 1}`,
		"missing stamp":  `{"labels": 3}`,
		"bad type":       `{"generated_at": "2026-10-07T12:00:00Z", "types": [{"type": "same"}]}`,
		"bad precision":  `{"generated_at": "2026-10-07T12:00:00Z", "types": [{"type": "equivalent", "precision": "1.5"}]}`,
		"negative count": `{"generated_at": "2026-10-07T12:00:00Z", "types": [{"type": "equivalent", "tp": -1}]}`,
		"too large":      `{"generated_at": "2026-10-07T12:00:00Z", "notes": ["` + strings.Repeat("x", maxEvalArtifactBytes) + `"]}`,
	} {
		rec := get(New(Config{EvalSummaryFile: writeEval(t, body)}, Deps{}).Handler(), "/eval")
		wantHTML(t, rec, http.StatusOK)
		if !strings.Contains(rec.Body.String(), "The eval artifact could not be read") {
			t.Errorf("%s: page does not report the broken artifact", name)
		}
		if strings.Contains(rec.Body.String(), "bogus") || strings.Contains(rec.Body.String(), `"same"`) || strings.Contains(rec.Body.String(), "types[") {
			t.Errorf("%s: page echoes artifact content", name)
		}
	}
}

func TestFormatPercentAndHTTPURL(t *testing.T) {
	if got := formatPercent(nil); got != "n/a" {
		t.Errorf("formatPercent(nil) = %q", got)
	}
	d := dec("0.9231")
	if got := formatPercent(&d); got != "92.31%" {
		t.Errorf("formatPercent = %q", got)
	}
	for raw, want := range map[string]string{
		"https://www.federalreserve.gov/x": "https://www.federalreserve.gov/x",
		" http://example.com/a?b=c ":       "http://example.com/a?b=c",
		"javascript:alert(1)":              "",
		"ftp://example.com":                "",
		"federalreserve.gov":               "",
		"https://":                         "",
		"":                                 "",
		"not a url at all":                 "",
	} {
		if got := httpURL(raw); got != want {
			t.Errorf("httpURL(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestLayoutNavLinksNewPages(t *testing.T) {
	body := get(New(Config{}, Deps{}).Handler(), "/eval").Body.String()
	// UI-10: nav links now include aria-current on the active page
	// M5-09: nav includes Glossary after Eval
	wantContains(t, body,
		`<a href="/"`,
		`>Matches</a>`,
		`<a href="/near-misses"`,
		`>Near misses</a>`,
		`<a href="/log"`,
		`>Decision log</a>`,
		`<a href="/eval"`,
		`aria-current="page"`,
		`>Eval</a>`,
		`<a href="/glossary"`,
		`>Glossary</a>`)
}

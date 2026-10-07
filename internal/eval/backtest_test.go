package eval

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"pgregory.net/rapid"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/facts"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues/fixtureserver"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues/kalshi"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues/polymarket"
)

var (
	btDeadline = time.Date(2026, 10, 6, 21, 0, 0, 0, time.UTC)
	// fixturesRecordedAt is when testdata/fixtures/** was recorded (D-056/D-057).
	fixturesRecordedAt = time.Date(2026, 10, 6, 22, 0, 0, 0, time.UTC)
)

func btDec(s string) *decimal.Decimal {
	d := decimal.RequireFromString(s)
	return &d
}

// btSettled is a fully parsed BTC threshold contract on one venue with its outcome.
func btSettled(id, threshold string, comp canonical.Comparator, pol canonical.Polarity, outcome canonical.Outcome) canonical.Settled {
	dl := btDeadline
	return canonical.Settled{
		Instrument: canonical.Instrument{
			ID: id, Venue: canonical.VenueOf(id), VenueMarketID: "X", Category: canonical.CategoryCrypto,
			Title: "Bitcoin above " + threshold + " on Oct 6?", RulesText: "r", CloseTime: btDeadline, Status: canonical.StatusSettled,
			Terms: canonical.Terms{
				Subject: "BTC", Metric: "price", Comparator: comp, Threshold: btDec(threshold), Unit: "USD",
				Deadline: &dl, DeadlineTZ: "America/New_York", ResolutionSource: "cf_benchmarks_brti",
				EdgeRules: canonical.Unknown, Polarity: pol,
			},
			FeeSchedule: canonical.FeeSchedule{ID: canonical.FeeScheduleKalshiTakerV1},
		},
		Outcome:   outcome,
		SettledAt: btDeadline.Add(time.Minute),
	}
}

func kalshiYes(threshold string, outcome canonical.Outcome) canonical.Settled {
	return btSettled("kalshi:A-"+threshold, threshold, canonical.ComparatorGT, canonical.PolarityPositive, outcome)
}

func polyYes(threshold string, outcome canonical.Outcome) canonical.Settled {
	return btSettled("polymarket:B-"+threshold, threshold, canonical.ComparatorGT, canonical.PolarityPositive, outcome)
}

func TestBacktestEquivalentAgreement(t *testing.T) {
	r := Backtest([]canonical.Settled{kalshiYes("76599.99", canonical.OutcomeYes), polyYes("76599.99", canonical.OutcomeYes)}, facts.Set{}, nil)
	if r.Pairs != 1 || r.ByType[canonical.RelEquivalent] != 1 || len(r.Checked) != 1 {
		t.Fatalf("report = %+v", r)
	}
	p := r.Checked[0]
	if !p.Agree || p.Expect != ExpectSame || p.Rel.Type != canonical.RelEquivalent || p.Exception != nil {
		t.Errorf("checked pair = %+v", p)
	}
	if !r.Passed() || len(r.Mismatches(false)) != 0 || len(r.Exceptions()) != 0 {
		t.Errorf("passed=%v mismatches=%d exceptions=%d", r.Passed(), len(r.Mismatches(false)), len(r.Exceptions()))
	}
	out := r.String()
	for _, want := range []string{"PASS", "| equivalent | 1 | 1 | 1 | 0 | 0 | same outcome |", "Agreed pairs:", "kalshi:A-76599.99 settled yes", "facts available: none"} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
}

func TestBacktestEquivalentMismatchAndAllowlist(t *testing.T) {
	settled := []canonical.Settled{kalshiYes("76599.99", canonical.OutcomeYes), polyYes("76599.99", canonical.OutcomeNo)}
	r := Backtest(settled, facts.Set{}, nil)
	if r.Passed() || len(r.Mismatches(true)) != 1 || len(r.Mismatches(false)) != 1 {
		t.Fatalf("mismatch not reported: %+v", r.Checked)
	}
	out := r.String()
	for _, want := range []string{"FAIL", "1 equivalent-tier pairs settled differently", "Disagreements", "DISAGREED", "| equivalent | 1 | 1 | 0 | 1 | 0 |"} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}

	// The same pair listed the other way round is allowlisted with a reason and a decision.
	al, err := NewAllowlist([]Exception{{AID: "polymarket:B-76599.99", BID: "kalshi:A-76599.99", Reason: "venue B voided and paid NO by its own rule", Decision: "D-070"}})
	if err != nil {
		t.Fatal(err)
	}
	r = Backtest(settled, facts.Set{}, al)
	if !r.Passed() || len(r.Mismatches(false)) != 0 || len(r.Exceptions()) != 1 || r.Exceptions()[0].Exception.Decision != "D-070" {
		t.Fatalf("allowlist not applied: passed=%v exceptions=%+v", r.Passed(), r.Exceptions())
	}
	out = r.String()
	for _, want := range []string{"PASS", "(1 allowlisted exceptions)", "Allowlisted exceptions:", "allowed by D-070: venue B voided", "| equivalent | 1 | 1 | 0 | 0 | 1 |"} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
}

func TestBacktestComplementAndImplies(t *testing.T) {
	neg := btSettled("polymarket:B-neg", "76599.99", canonical.ComparatorGT, canonical.PolarityNegative, canonical.OutcomeNo)
	// Complement: YES on A pays exactly when NO on B pays, so the outcomes must differ.
	r := Backtest([]canonical.Settled{kalshiYes("76599.99", canonical.OutcomeYes), neg}, facts.Set{}, nil)
	if r.ByType[canonical.RelComplement] != 1 || len(r.Checked) != 1 || !r.Checked[0].Agree || r.Checked[0].Expect != ExpectOpposite {
		t.Fatalf("complement: %+v (types %v)", r.Checked, r.ByType)
	}
	neg.Outcome = canonical.OutcomeYes
	r = Backtest([]canonical.Settled{kalshiYes("76599.99", canonical.OutcomeYes), neg}, facts.Set{}, nil)
	if r.Checked[0].Agree || !r.Passed() || len(r.Mismatches(false)) != 1 || len(r.Mismatches(true)) != 0 {
		t.Errorf("a complement that settled the same way must be a non-gate disagreement: %+v", r.Checked[0])
	}

	// Implies: "above 76600" forces "above 76599.99"; YES on the forcing side with NO on the forced side is the only violation.
	cases := []struct {
		forcing, forced canonical.Outcome
		agree           bool
	}{
		{canonical.OutcomeYes, canonical.OutcomeYes, true},
		{canonical.OutcomeNo, canonical.OutcomeYes, true},
		{canonical.OutcomeNo, canonical.OutcomeNo, true},
		{canonical.OutcomeYes, canonical.OutcomeNo, false},
	}
	for _, tc := range cases {
		r := Backtest([]canonical.Settled{kalshiYes("76599.99", tc.forced), polyYes("76600", tc.forcing)}, facts.Set{}, nil)
		if r.ByType[canonical.RelImplies] != 1 || len(r.Checked) != 1 {
			t.Fatalf("implies: %+v (types %v)", r.Checked, r.ByType)
		}
		p := r.Checked[0]
		if p.Forcing != "polymarket:B-76600" || p.Forced != "kalshi:A-76599.99" {
			t.Fatalf("direction = %s forces %s; explanation %q", p.Forcing, p.Forced, p.Rel.Explanation)
		}
		if p.Agree != tc.agree {
			t.Errorf("forcing %s, forced %s: agree=%v want %v", tc.forcing, tc.forced, p.Agree, tc.agree)
		}
		if !strings.Contains(r.String(), "implies (polymarket:B-76600 forces kalshi:A-76599.99)") {
			t.Errorf("report does not name the direction:\n%s", r.String())
		}
	}
}

func TestBacktestImpliesWithoutDirectionDisagrees(t *testing.T) {
	a, b := kalshiYes("76599.99", canonical.OutcomeYes), polyYes("76600", canonical.OutcomeYes)
	rel := canonical.Relationship{AID: a.Instrument.ID, BID: b.Instrument.ID, Type: canonical.RelImplies, Explanation: "implies: direction lost"}
	p, ok := check(a, b, rel, nil)
	if !ok || p.Agree || p.Forcing != "" {
		t.Errorf("an implies verdict without a readable direction must not pass: %+v", p)
	}
}

func TestBacktestRejectsInvalidAndDuplicates(t *testing.T) {
	bad := kalshiYes("1", canonical.OutcomeYes)
	bad.Outcome = "maybe"
	dup := polyYes("76599.99", canonical.OutcomeYes)
	r := Backtest([]canonical.Settled{kalshiYes("76599.99", canonical.OutcomeYes), dup, dup, bad}, facts.Set{}, nil)
	if len(r.Rejected) != 2 || r.Instruments[canonical.VenueKalshi] != 1 || r.Instruments[canonical.VenuePolymarket] != 1 || r.Pairs != 1 {
		t.Fatalf("report = %+v", r)
	}
	if r.Rejected[0].ID != dup.Instrument.ID || r.Rejected[0].Reason != "duplicate id" || !strings.Contains(r.Rejected[1].Reason, "outcome") {
		t.Errorf("rejected = %+v", r.Rejected)
	}
	if !strings.Contains(r.String(), "Rejected records (2):") {
		t.Errorf("report lacks the rejected list:\n%s", r.String())
	}
}

func TestBacktestSkipsOtherCategoriesAndSameVenue(t *testing.T) {
	fed := kalshiYes("2", canonical.OutcomeYes)
	fed.Instrument.ID, fed.Instrument.Category = "polymarket:fed", canonical.CategoryFed
	r := Backtest([]canonical.Settled{kalshiYes("76599.99", canonical.OutcomeYes), kalshiYes("76600", canonical.OutcomeYes), fed}, facts.Set{}, nil)
	if r.Pairs != 0 || len(r.Checked) != 0 || !r.Passed() {
		t.Errorf("same-venue and cross-category pairs must not be compared: %+v", r)
	}
	if !strings.Contains(r.String(), "0 cross-venue pairs compared") {
		t.Errorf("report:\n%s", r.String())
	}
}

func TestAllowlistParse(t *testing.T) {
	good := `exceptions:
  - a_id: kalshi:KXFED-26SEP-T4.00
    b_id: polymarket:123
    reason: investigated; venue rule differs on an off-grid move
    decision: D-070
`
	al, err := ParseAllowlist([]byte(good))
	if err != nil || len(al) != 1 {
		t.Fatalf("ParseAllowlist: %v, %d entries", err, len(al))
	}
	if _, ok := al[pairKey("polymarket:123", "kalshi:KXFED-26SEP-T4.00")]; !ok {
		t.Error("allowlist is not keyed by the unordered pair")
	}
	if al, err := ParseAllowlist([]byte("exceptions: []\n")); err != nil || len(al) != 0 {
		t.Errorf("empty list: %v, %d", err, len(al))
	}
	bad := map[string]string{
		"missing list":      "other: 1\n",
		"empty file":        "",
		"unknown key":       "exceptions:\n  - a_id: kalshi:A\n    b_id: polymarket:B\n    reason: r\n    decision: D-070\n    why: x\n",
		"missing reason":    "exceptions:\n  - a_id: kalshi:A\n    b_id: polymarket:B\n    reason: '  '\n    decision: D-070\n",
		"bad decision":      "exceptions:\n  - a_id: kalshi:A\n    b_id: polymarket:B\n    reason: r\n    decision: ticket-7\n",
		"same id":           "exceptions:\n  - a_id: kalshi:A\n    b_id: kalshi:A\n    reason: r\n    decision: D-070\n",
		"not instrument id": "exceptions:\n  - a_id: A\n    b_id: polymarket:B\n    reason: r\n    decision: D-070\n",
		"duplicate pair":    "exceptions:\n  - {a_id: kalshi:A, b_id: polymarket:B, reason: r, decision: D-070}\n  - {a_id: polymarket:B, b_id: kalshi:A, reason: r, decision: D-071}\n",
	}
	for name, src := range bad {
		if _, err := ParseAllowlist([]byte(src)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestLoadAllowlist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "allow.yaml")
	if err := os.WriteFile(path, []byte("exceptions: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if al, err := LoadAllowlist(path); err != nil || len(al) != 0 {
		t.Errorf("LoadAllowlist: %v, %d", err, len(al))
	}
	if _, err := LoadAllowlist(filepath.Join(dir, "missing.yaml")); err == nil {
		t.Error("missing file: no error")
	}
	big := make([]byte, maxAllowlistBytes+1)
	if err := os.WriteFile(path, big, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAllowlist(path); err == nil || !strings.Contains(err.Error(), "cap") {
		t.Errorf("oversized file: %v", err)
	}
}

// TestBacktestCheckProperties pins the settlement rules: the equivalent tier
// and complement are symmetric in the pair order, and implies is violated
// exactly when the forcing side paid YES and the forced side paid NO.
func TestBacktestCheckProperties(t *testing.T) {
	outcomes := []canonical.Outcome{canonical.OutcomeYes, canonical.OutcomeNo}
	rapid.Check(t, func(rt *rapid.T) {
		oa := rapid.SampledFrom(outcomes).Draw(rt, "a")
		ob := rapid.SampledFrom(outcomes).Draw(rt, "b")
		typ := rapid.SampledFrom([]canonical.RelationshipType{canonical.RelEquivalent, canonical.RelEquivalentGivenFacts, canonical.RelComplement, canonical.RelImplies}).Draw(rt, "type")
		forward := rapid.Bool().Draw(rt, "forward")
		a, b := kalshiYes("1", oa), polyYes("2", ob)
		explain := "implies: YES on " + a.Instrument.ID + " forces YES on " + b.Instrument.ID + ", not the reverse"
		if !forward {
			explain = "implies: YES on " + b.Instrument.ID + " forces YES on " + a.Instrument.ID + ", not the reverse"
		}
		rel := canonical.Relationship{AID: a.Instrument.ID, BID: b.Instrument.ID, Type: typ, Explanation: explain}
		p, ok := check(a, b, rel, nil)
		if !ok {
			rt.Fatalf("type %s not checked", typ)
		}
		var want bool
		switch typ {
		case canonical.RelComplement:
			want = oa != ob
		case canonical.RelImplies:
			forcing, forced := oa, ob
			if !forward {
				forcing, forced = ob, oa
			}
			want = forcing != canonical.OutcomeYes || forced != canonical.OutcomeNo
		default:
			want = oa == ob
		}
		if p.Agree != want {
			rt.Fatalf("%s a=%s b=%s forward=%v: agree=%v want %v", typ, oa, ob, forward, p.Agree, want)
		}
		// Swapping the pair must not change the verdict on the settlements.
		swapped := rel
		swapped.AID, swapped.BID = rel.BID, rel.AID
		q, _ := check(b, a, swapped, nil)
		if q.Agree != p.Agree {
			rt.Fatalf("%s: agree differs when the pair is swapped", typ)
		}
	})
}

func TestLookbackSince(t *testing.T) {
	lb := DefaultLookback()
	asOf := fixturesRecordedAt
	if got := lb.Since(canonical.CategoryCrypto, asOf); !got.Equal(asOf.AddDate(0, 0, -1)) {
		t.Errorf("crypto since = %v", got)
	}
	if got := lb.Since(canonical.CategoryFed, asOf); !got.Equal(asOf.AddDate(0, 0, -90)) {
		t.Errorf("fed since = %v", got)
	}
	if got := (Lookback{}).Since(canonical.CategoryFed, asOf); !got.IsZero() {
		t.Errorf("missing category since = %v, want zero", got)
	}
}

// fixtureAdapters serves testdata/fixtures/** over httptest and builds both
// venue adapters against it. No network is involved.
func fixtureAdapters(t *testing.T) []venues.Adapter {
	t.Helper()
	root := filepath.Join("..", "..", "testdata", "fixtures")
	fx, err := fixtureserver.NewFS(os.DirFS(root), nil)
	if err != nil {
		t.Fatalf("fixture server: %v", err)
	}
	srv := httptest.NewServer(fx.Handler())
	t.Cleanup(srv.Close)
	kURL, gammaURL, clobURL := fixtureserver.VenueURLs(srv.URL)
	quiet := slog.New(slog.DiscardHandler)
	// Two KXBTCD settled pages are recorded; a third page request is a 404.
	k, err := kalshi.New(kalshi.Options{BaseURL: kURL, Retries: -1, MinInterval: -1, Backoff: time.Millisecond, MaxSettledPages: 2, Logger: quiet})
	if err != nil {
		t.Fatalf("kalshi: %v", err)
	}
	p, err := polymarket.New(polymarket.Options{GammaURL: gammaURL, ClobURL: clobURL, Timeout: 5 * time.Second, MaxBodyBytes: 10 << 20, Retries: -1, Logger: quiet})
	if err != nil {
		t.Fatalf("polymarket: %v", err)
	}
	return []venues.Adapter{k, p}
}

// recordedUniverse is the part of config/universe/ with settled fixtures on
// disk: the ethereum settled pages were not committed (D-057).
func recordedUniverse() venues.Universes {
	return venues.Universes{
		canonical.VenueKalshi: {Venue: canonical.VenueKalshi, Categories: map[canonical.Category]venues.Selector{
			canonical.CategoryCrypto: {Series: []string{"KXBTCD"}},
			canonical.CategoryFed:    {Series: []string{"KXFED"}},
		}},
		canonical.VenuePolymarket: {Venue: canonical.VenuePolymarket, Categories: map[canonical.Category]venues.Selector{
			canonical.CategoryCrypto: {TagSlugs: []string{"bitcoin"}},
			canonical.CategoryFed:    {TagSlugs: []string{"fed-rates"}},
		}},
	}
}

// TestBacktestFixtures is the NFR-07 evidence on the recorded sample: every
// equivalent-tier verdict on settled cross-venue pairs agreed with the
// settlements (no allowlist is needed), and so did every derived verdict.
func TestBacktestFixtures(t *testing.T) {
	settled, errs := FetchSettled(context.Background(), fixtureAdapters(t), recordedUniverse(), DefaultLookback(), fixturesRecordedAt, nil)
	if len(errs) != 0 {
		t.Fatalf("fetch errors: %v", errs)
	}
	f, err := facts.LoadAt(filepath.Join("..", "..", "testdata", "fixtures", "backtest", "facts.yaml"), fixturesRecordedAt)
	if err != nil {
		t.Fatalf("facts: %v", err)
	}
	r := Backtest(NormalizeSettled(settled), f, nil)
	t.Logf("\n%s", r.String())
	if r.Instruments[canonical.VenueKalshi] < 400 || r.Instruments[canonical.VenuePolymarket] < 50 || r.Pairs == 0 {
		t.Fatalf("thin sample: %+v", r.Instruments)
	}
	if len(r.Rejected) != 0 {
		t.Errorf("rejected records: %+v", r.Rejected)
	}
	if !r.Passed() {
		t.Errorf("NFR-07: %d equivalent-tier pairs settled differently", len(r.Mismatches(true)))
	}
	if m := r.Mismatches(false); len(m) != 0 {
		t.Errorf("%d derived verdicts disagreed with the settlements", len(m))
	}
	// The sample must exercise the facts rule, or the gate is vacuous.
	if r.ByType[canonical.RelEquivalentGivenFacts] == 0 || r.ByType[canonical.RelComplement] == 0 || r.ByType[canonical.RelImplies] == 0 {
		t.Errorf("sample exercises no derived types: %v", r.ByType)
	}
	for _, p := range r.Checked {
		if p.Rel.Type == canonical.RelEquivalentGivenFacts && len(p.Rel.FactsUsed) == 0 {
			t.Errorf("%s/%s: equivalent_given_facts without facts", p.A.Instrument.ID, p.B.Instrument.ID)
		}
	}
}

// TestFetchSettledReportsPartialFailure: a venue whose fixture is missing
// (ethereum, D-057) is reported, and the other categories still count.
func TestFetchSettledReportsPartialFailure(t *testing.T) {
	us := recordedUniverse()
	k := us[canonical.VenueKalshi]
	k.Categories[canonical.CategoryCrypto] = venues.Selector{Series: []string{"KXBTCD", "KXETHD"}}
	us[canonical.VenueKalshi] = k
	settled, errs := FetchSettled(context.Background(), fixtureAdapters(t), us, DefaultLookback(), fixturesRecordedAt, nil)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "kalshi crypto") || !strings.Contains(errs[0].Error(), "KXETHD") {
		t.Fatalf("errors = %v", errs)
	}
	var fed int
	for _, s := range settled {
		if s.Instrument.Category == canonical.CategoryFed {
			fed++
		}
	}
	if fed == 0 || len(settled) <= fed {
		t.Errorf("partial results dropped: %d settled, %d fed", len(settled), fed)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, errs := FetchSettled(ctx, fixtureAdapters(t), us, DefaultLookback(), fixturesRecordedAt, nil); len(errs) == 0 {
		t.Error("cancelled context: no error")
	}
	if settled, errs := FetchSettled(context.Background(), nil, us, DefaultLookback(), fixturesRecordedAt, nil); len(settled) != 0 || len(errs) != 0 {
		t.Errorf("no adapters: %d settled, %v", len(settled), errs)
	}
}

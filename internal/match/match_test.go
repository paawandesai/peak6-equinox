package match

import (
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

var (
	t0 = time.Date(2026, 10, 6, 22, 0, 0, 0, time.UTC)
)

func dec(s string) *decimal.Decimal {
	d := decimal.RequireFromString(s)
	return &d
}

// known returns a fully parsed BTC threshold instrument.
func known(id string, deadline time.Time) canonical.Instrument {
	dl := deadline
	return canonical.Instrument{
		ID: id, Venue: canonical.VenueOf(id), VenueMarketID: "X", Category: canonical.CategoryCrypto,
		Title: "BTC", RulesText: "r", CloseTime: deadline, Status: canonical.StatusOpen,
		Terms: canonical.Terms{
			Subject: "BTC", Metric: "price", Comparator: canonical.ComparatorGT, Threshold: dec("76599.99"), Unit: "USD",
			Deadline: &dl, DeadlineTZ: "America/New_York", ResolutionSource: "cf_benchmarks_brti",
			EdgeRules: canonical.Unknown, Polarity: canonical.PolarityPositive,
		},
		FeeSchedule: canonical.FeeSchedule{ID: canonical.FeeScheduleKalshiTakerV1},
	}
}

func pair() (canonical.Instrument, canonical.Instrument) {
	return known("kalshi:A", t0), known("polymarket:B", t0)
}

func TestCryptoCompareEquivalent(t *testing.T) {
	a, b := pair()
	rel := Compare(a, b, nil)
	if rel.Type != canonical.RelEquivalent || len(rel.Vetoes) != 0 {
		t.Fatalf("got %s %v", rel.Type, rel.Vetoes)
	}
	// 0.99 base − 0.02 per side for unknown edge rules = 0.95.
	if !rel.Confidence.Equal(decimal.RequireFromString("0.95")) {
		t.Errorf("confidence = %s, want 0.95", rel.Confidence)
	}
	if rel.Method != canonical.MethodRules || rel.AID != "kalshi:A" || rel.BID != "polymarket:B" || rel.RoundID != 0 {
		t.Errorf("relationship = %+v", rel)
	}
	for _, want := range []string{"equivalent", "BTC", "price", "gt", "76599.99", "cf_benchmarks_brti", "positive", "edge rules not compared"} {
		if !strings.Contains(rel.Explanation, want) {
			t.Errorf("explanation %q lacks %q", rel.Explanation, want)
		}
	}
	rel.RoundID = 7
	if err := rel.Validate(); err != nil {
		t.Error(err)
	}
}

func TestEquivalentConfidenceDeductions(t *testing.T) {
	a, b := pair()
	a.Terms.EdgeRules, b.Terms.EdgeRules = "none", "none"
	if c := equivalentConfidence(a, b); !c.Equal(decimal.RequireFromString("0.99")) {
		t.Errorf("clean pair: %s", c)
	}
	a.Terms.Unit = canonical.Unknown
	if c := equivalentConfidence(a, b); !c.Equal(decimal.RequireFromString("0.97")) {
		t.Errorf("unknown unit: %s", c)
	}
	b.Quality.Add(canonical.QualityDeadlineDefaulted, "deadline", "x")
	if c := equivalentConfidence(a, b); !c.Equal(decimal.RequireFromString("0.92")) {
		t.Errorf("defaulted deadline: %s", c)
	}
	// A non-crypto category tolerates a gap: 5 h costs 0.05.
	a.Category, b.Category = canonical.CategoryFed, canonical.CategoryFed
	later := t0.Add(5*time.Hour + 30*time.Minute)
	b.Terms.Deadline = &later
	if c := equivalentConfidence(a, b); !c.Equal(decimal.RequireFromString("0.87")) {
		t.Errorf("5.5 h gap: %s", c)
	}
	// Every deduction at once: 0.99 − 0.23 (23 h) − 0.02 (unit) − 0.04 (edge rules) − 0.10 (two defaulted deadlines).
	a.Terms.EdgeRules, b.Terms.EdgeRules = canonical.Unknown, canonical.Unknown
	a.Quality.Add(canonical.QualityDeadlineDefaulted, "deadline", "x")
	far := t0.Add(23 * time.Hour)
	b.Terms.Deadline = &far
	if c := equivalentConfidence(a, b); !c.Equal(decimal.RequireFromString("0.6")) {
		t.Errorf("all deductions: %s", c)
	}
	// The floor clamps anything lower.
	if c := clampConfidence(decimal.RequireFromString("0.2")); !c.Equal(confFloor) {
		t.Errorf("floor: %s", c)
	}
}

func TestVetoCodesPerField(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(b *canonical.Instrument)
		code   string
		field  string
	}{
		{"subject differs", func(b *canonical.Instrument) { b.Terms.Subject = "ETH" }, VetoSubjectMismatch, "subject"},
		{"subject unknown", func(b *canonical.Instrument) { b.Terms.Subject = canonical.Unknown }, VetoSubjectUnknown, "subject"},
		{"metric differs", func(b *canonical.Instrument) { b.Terms.Metric = "price_high" }, VetoMetricMismatch, "metric"},
		{"metric unknown", func(b *canonical.Instrument) { b.Terms.Metric = canonical.Unknown }, VetoMetricUnknown, "metric"},
		{"comparator differs", func(b *canonical.Instrument) { b.Terms.Comparator = canonical.ComparatorGTE }, VetoComparatorMismatch, "comparator"},
		{"comparator unknown", func(b *canonical.Instrument) { b.Terms.Comparator = canonical.ComparatorUnknown }, VetoComparatorUnknown, "comparator"},
		{"threshold differs", func(b *canonical.Instrument) { b.Terms.Threshold = dec("76600") }, VetoThresholdMismatch, "threshold"},
		{"threshold unknown", func(b *canonical.Instrument) { b.Terms.Threshold = nil }, VetoThresholdUnknown, "threshold"},
		{"deadline unknown", func(b *canonical.Instrument) { b.Terms.Deadline = nil }, VetoDeadlineUnknown, "deadline"},
		{"deadline 1s off in crypto", func(b *canonical.Instrument) { d := t0.Add(time.Second); b.Terms.Deadline = &d }, VetoDeadlineMismatch, "deadline"},
		{"deadline 24h off", func(b *canonical.Instrument) { d := t0.Add(24 * time.Hour); b.Terms.Deadline = &d }, VetoDeadlineGap, "deadline"},
		{"source differs", func(b *canonical.Instrument) { b.Terms.ResolutionSource = "binance_btc_usdt" }, VetoSourceMismatch, "resolution_source"},
		{"source unknown", func(b *canonical.Instrument) { b.Terms.ResolutionSource = canonical.Unknown }, VetoSourceUnknown, "resolution_source"},
		{"polarity differs", func(b *canonical.Instrument) { b.Terms.Polarity = canonical.PolarityNegative }, VetoPolarityMismatch, "polarity"},
		{"polarity unknown", func(b *canonical.Instrument) { b.Terms.Polarity = canonical.PolarityUnknown }, VetoPolarityUnresolved, "polarity"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, b := pair()
			tc.mutate(&b)
			vs := vetoes(a, b)
			if len(vs) != 1 || vs[0].Code != tc.code || vs[0].Field != tc.field || vs[0].Detail == "" {
				t.Fatalf("vetoes = %+v, want one %s on %s", vs, tc.code, tc.field)
			}
			if rel := Compare(a, b, nil); rel.Type == canonical.RelEquivalent {
				t.Fatalf("still equivalent: %s", rel.Explanation)
			}
		})
	}
}

func TestVetoDeadlineToleranceOutsideExactCategories(t *testing.T) {
	a, b := pair()
	a.Category, b.Category = canonical.CategoryFed, canonical.CategoryFed
	within := t0.Add(23*time.Hour + 59*time.Minute)
	b.Terms.Deadline = &within
	if vs := vetoes(a, b); len(vs) != 0 {
		t.Errorf("23h59m in fed vetoed: %v", vs)
	}
	exact := t0.Add(DeadlineTolerance)
	b.Terms.Deadline = &exact
	if vs := vetoes(a, b); len(vs) != 1 || vs[0].Code != VetoDeadlineGap {
		t.Errorf("exactly 24h in fed: %v", vs)
	}
}

func TestVetoBetweenBounds(t *testing.T) {
	a, b := pair()
	a.Terms.Comparator, b.Terms.Comparator = canonical.ComparatorBetween, canonical.ComparatorBetween
	a.Terms.ThresholdHigh, b.Terms.ThresholdHigh = dec("80000"), dec("80000")
	if vs := vetoes(a, b); len(vs) != 0 {
		t.Errorf("equal ranges vetoed: %v", vs)
	}
	b.Terms.ThresholdHigh = dec("81000")
	if vs := vetoes(a, b); len(vs) != 1 || vs[0].Field != "threshold_high" || vs[0].Code != VetoThresholdMismatch {
		t.Errorf("different upper bound: %v", vs)
	}
	b.Terms.ThresholdHigh = nil
	if vs := vetoes(a, b); len(vs) != 1 || vs[0].Code != VetoThresholdUnknown {
		t.Errorf("missing upper bound: %v", vs)
	}
	// Range vs threshold is only a comparator mismatch, not an unknown.
	b.Terms.Comparator = canonical.ComparatorGT
	if vs := vetoes(a, b); len(vs) != 1 || vs[0].Code != VetoComparatorMismatch {
		t.Errorf("range vs threshold: %v", vs)
	}
}

func TestBlockReasons(t *testing.T) {
	a, b := pair()
	if r := block(a, b); r != nil {
		t.Fatalf("clean pair blocked: %+v", r)
	}
	b.Category = canonical.CategoryFed
	if r := block(a, b); r == nil || r.field != "category" {
		t.Errorf("category: %+v", r)
	}
	a, b = pair()
	b.Terms.Subject = "ETH"
	if r := block(a, b); r == nil || r.field != "subject" || strings.Contains(r.detail, "unknown") {
		t.Errorf("subject: %+v", r)
	}
	b.Terms.Subject = canonical.Unknown
	if r := block(a, b); r == nil || r.field != "subject" || !strings.Contains(r.detail, "unknown") {
		t.Errorf("unknown subject: %+v", r)
	}
	a, b = pair()
	b.Terms.Deadline = nil
	if r := block(a, b); r == nil || r.field != "deadline" {
		t.Errorf("nil deadline: %+v", r)
	}
	edge := t0.Add(BlockWindow)
	b.Terms.Deadline = &edge
	if r := block(a, b); r != nil {
		t.Errorf("exactly 7 days blocked: %+v", r)
	}
	beyond := t0.Add(BlockWindow + time.Second)
	b.Terms.Deadline = &beyond
	if r := block(a, b); r == nil || r.field != "deadline" {
		t.Errorf("beyond 7 days: %+v", r)
	}
}

func TestClassifyUnrelatedConfidence(t *testing.T) {
	a, b := pair()
	b.Terms.Subject = "ETH"
	rel := Compare(a, b, nil)
	if rel.Type != canonical.RelUnrelated || !rel.Confidence.Equal(confUnrelatedKnown) || !strings.Contains(rel.Explanation, "blocked on subject (BTC vs ETH)") {
		t.Errorf("known mismatch: %s %s %q", rel.Type, rel.Confidence, rel.Explanation)
	}
	b.Terms.Subject = canonical.Unknown
	rel = Compare(a, b, nil)
	if rel.Type != canonical.RelUnrelated || !rel.Confidence.Equal(confUnrelatedUnsure) {
		t.Errorf("unknown subject: %s %s", rel.Type, rel.Confidence)
	}
	// Vetoes are still recorded for a blocked pair (FR-08: each veto is recorded).
	if len(rel.Vetoes) != 1 || rel.Vetoes[0].Code != VetoSubjectUnknown {
		t.Errorf("vetoes = %v", rel.Vetoes)
	}
}

func TestClassifyRelatedConfidenceAndUncertainty(t *testing.T) {
	a, b := pair()
	b.Terms.Threshold = dec("76600")
	rel := Compare(a, b, nil)
	if rel.Type != canonical.RelRelated || !rel.Confidence.Equal(confRelatedKnown) || strings.Contains(rel.Explanation, "Uncertain") {
		t.Errorf("known difference: %s %s %q", rel.Type, rel.Confidence, rel.Explanation)
	}
	if !strings.Contains(rel.Explanation, "threshold (76599.99 vs 76600)") {
		t.Errorf("explanation %q does not name the threshold values", rel.Explanation)
	}
	b.Terms.ResolutionSource = canonical.Unknown
	rel = Compare(a, b, nil)
	if rel.Type != canonical.RelRelated || !rel.Confidence.Equal(confUncertain) || !strings.Contains(rel.Explanation, "Uncertain") {
		t.Errorf("unknown source: %s %s %q", rel.Type, rel.Confidence, rel.Explanation)
	}
}

func TestDiffStatuses(t *testing.T) {
	a, b := pair()
	later := t0.Add(time.Hour)
	b.Terms.Deadline = &later
	b.Terms.Threshold = nil
	b.Terms.Unit = "USDC"
	diff := Diff(a.Terms, b.Terms)
	got := map[string]canonical.FieldDiff{}
	for _, d := range diff {
		got[d.Field] = d
	}
	if len(diff) != len(TermFields) {
		t.Fatalf("%d fields", len(diff))
	}
	checks := map[string]canonical.DiffStatus{
		"subject": canonical.DiffMatch, "metric": canonical.DiffMatch, "comparator": canonical.DiffMatch,
		"threshold": canonical.DiffUnknown, "threshold_high": canonical.DiffMatch, "unit": canonical.DiffDiffer,
		"deadline": canonical.DiffDiffer, "deadline_tz": canonical.DiffMatch, "resolution_source": canonical.DiffMatch,
		"edge_rules": canonical.DiffUnknown, "polarity": canonical.DiffMatch,
	}
	for f, want := range checks {
		if got[f].Status != want {
			t.Errorf("%s: %s (%q vs %q), want %s", f, got[f].Status, got[f].A, got[f].B, want)
		}
	}
	if got["deadline"].A != "2026-10-06T22:00:00Z" || got["threshold"].B != canonical.Unknown || got["threshold_high"].A != "n/a" {
		t.Errorf("rendered values: %+v %+v %+v", got["deadline"], got["threshold"], got["threshold_high"])
	}
	// Order is stable.
	for i, f := range TermFields {
		if diff[i].Field != f {
			t.Errorf("diff[%d] = %s, want %s", i, diff[i].Field, f)
		}
	}
}

func TestDiffBetweenShowsUpperBound(t *testing.T) {
	a, _ := pair()
	a.Terms.Comparator, a.Terms.ThresholdHigh = canonical.ComparatorBetween, dec("80000")
	b := a
	b.Terms.ThresholdHigh = dec("81000")
	for _, d := range Diff(a.Terms, b.Terms) {
		if d.Field == "threshold_high" && (d.Status != canonical.DiffDiffer || d.A != "80000" || d.B != "81000") {
			t.Errorf("threshold_high diff = %+v", d)
		}
	}
	unknownCmp := a
	unknownCmp.Terms.Comparator, unknownCmp.Terms.ThresholdHigh = canonical.ComparatorUnknown, nil
	for _, d := range Diff(unknownCmp.Terms, unknownCmp.Terms) {
		if d.Field == "threshold_high" && d.Status != canonical.DiffUnknown {
			t.Errorf("unknown comparator threshold_high = %+v", d)
		}
	}
}

func TestCompareIgnoresVenue(t *testing.T) {
	// The matcher must not care which venue an instrument is from: a Kalshi
	// pair and a cross-venue pair with the same terms give the same verdict.
	a, b := pair()
	c := known("kalshi:C", t0)
	if x, y := Compare(a, b, nil), Compare(a, c, nil); x.Type != y.Type || !x.Confidence.Equal(y.Confidence) {
		t.Errorf("venue changed the verdict: %s vs %s", x.Type, y.Type)
	}
}

type fakeFacts struct{}

func (fakeFacts) Lookup(string) (canonical.FactUse, bool) { return canonical.FactUse{}, false }

func TestCompareAcceptsFacts(t *testing.T) {
	a, b := pair()
	rel := Compare(a, b, fakeFacts{})
	if rel.Type != canonical.RelEquivalent || len(rel.FactsUsed) != 0 || rel.FactsUsed == nil {
		t.Errorf("with facts: %s %v", rel.Type, rel.FactsUsed)
	}
}

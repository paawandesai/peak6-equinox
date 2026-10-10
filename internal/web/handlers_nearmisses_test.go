package web

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// TestNearMissGrouping verifies rows are grouped by veto code.
func TestNearMissGrouping(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	seedMore(t, s, st, sd)
	rec := get(s.Handler(), "/near-misses")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// Groups should have sections with anchors
	wantContains(t, body, `id="group-subject-mismatch"`)
	wantContains(t, body, `id="group-fact-did-not-close"`)

	// M5-10: Headings now show description first
	wantContains(t, body, "Different underlying subject", `(<code>subject_mismatch</code>`)
	wantContains(t, body, "A fact was consulted but did not close the difference", `(<code>fact_did_not_close</code>`)

	// Group chips should appear
	wantContains(t, body, `href="#group-subject-mismatch"`, `href="#group-fact-did-not-close"`)

	// M5-10: Legend should appear
	wantContains(t, body, "<strong>Legend:</strong>", "How close: the gap between the two thresholds")
}

// TestNearMissCountCells verifies strip shows count, veto count and fact-did-not-close.
func TestNearMissCountCells(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	seedMore(t, s, st, sd)
	rec := get(s.Handler(), "/near-misses")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	wantContains(t, body, `<dl class="strip">`)
	wantContains(t, body, "<dt>Count:</dt>", "<dd>2 near misses</dd>")
	wantContains(t, body, `<div class="strip-danger">`, "<dt>Stopped by a veto:</dt>", "<dd>1</dd>")
	wantContains(t, body, "<dt>Fact did not close:</dt>", "<dd>1</dd>")
}

// TestNearMissVetoBadge verifies the veto badge renders with glyph.
func TestNearMissVetoBadge(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	seedMore(t, s, st, sd)
	rec := get(s.Handler(), "/near-misses")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// Badge with glyph
	wantContains(t, body, `<span class="badge b-danger mono">`, `<span class="g" aria-hidden="true">✕</span>subject_mismatch`)
	wantContains(t, body, "<strong>subject</strong>: BTC is not ETH")

	// No-veto case
	wantContains(t, body, `<span class="badge b-info">`, `<span class="g" aria-hidden="true">◇</span>no veto`)
}

// TestNearMissTermGapThreshold verifies TermGap for a numeric threshold.
func TestNearMissTermGapThreshold(t *testing.T) {
	views := map[string]instrumentView{"kalshi:A": {}, "kalshi:B": {}}
	rels := []canonical.Relationship{{
		ID: 1, AID: "kalshi:A", BID: "kalshi:B", Type: canonical.RelRelated,
		Confidence: dec("0.5"),
		Diff: []canonical.FieldDiff{
			{Field: "threshold", A: "4.25", B: "0.5", Status: canonical.DiffDiffer},
		},
		Vetoes: []canonical.Veto{{Code: "threshold_mismatch", Field: "threshold", Detail: "4.25 vs 0.5"}},
	}}
	rows := nearMissRows(rels, views)
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if rows[0].TermGap != "3.75" {
		t.Errorf("TermGap = %q, want 3.75", rows[0].TermGap)
	}
}

// TestNearMissTermGapDeadline verifies TermGap for a deadline.
func TestNearMissTermGapDeadline(t *testing.T) {
	views := map[string]instrumentView{"kalshi:A": {}, "kalshi:B": {}}
	t1 := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	t2 := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	rels := []canonical.Relationship{{
		ID: 1, AID: "kalshi:A", BID: "kalshi:B", Type: canonical.RelRelated,
		Confidence: dec("0.5"),
		Diff: []canonical.FieldDiff{
			{Field: "deadline", A: t1, B: t2, Status: canonical.DiffDiffer},
		},
		Vetoes: []canonical.Veto{{Code: "deadline_mismatch", Field: "deadline", Detail: "2 days"}},
	}}
	rows := nearMissRows(rels, views)
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if rows[0].TermGap != "2 days" {
		t.Errorf("TermGap = %q, want \"2 days\"", rows[0].TermGap)
	}
}

// TestNearMissNoVetoGroup verifies the "fact did not close" group.
func TestNearMissNoVetoGroup(t *testing.T) {
	views := map[string]instrumentView{"kalshi:A": {}, "kalshi:B": {}, "kalshi:C": {}}
	rels := []canonical.Relationship{
		{ID: 1, AID: "kalshi:A", BID: "kalshi:B", Type: canonical.RelRelated, Confidence: dec("0.5"),
			Vetoes: []canonical.Veto{{Code: "x", Field: "field", Detail: "detail"}}},
		{ID: 2, AID: "kalshi:A", BID: "kalshi:C", Type: canonical.RelRelated, Confidence: dec("0.6")},
	}
	rows := nearMissRows(rels, views)
	groups := groupNearMisses(rows, "", false)
	var codes []string
	for _, g := range groups {
		codes = append(codes, g.Code)
	}
	// fact_did_not_close should be last
	if len(codes) != 2 || codes[1] != "fact_did_not_close" {
		t.Errorf("group codes = %v, want [x fact_did_not_close]", codes)
	}
	if groups[1].Count != 1 {
		t.Errorf("fact_did_not_close count = %d, want 1", groups[1].Count)
	}
}

// TestNearMissLogLink verifies the log link is present.
func TestNearMissLogLink(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	_, factMissID := seedMore(t, s, st, sd)
	rec := get(s.Handler(), "/near-misses")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// Log links should be present
	wantContains(t, body,
		`href="/pairs/`+strconv.FormatInt(sd.relID, 10)+`">Not-a-match record →</a>`,
		`href="/pairs/`+strconv.FormatInt(factMissID, 10)+`">Not-a-match record →</a>`)
}

// TestFormatDurationCoverage tests all branches of formatDurationDistance.
func TestFormatDurationCoverage(t *testing.T) {
	tests := []struct {
		dur  time.Duration
		want string
	}{
		{48 * time.Hour, "2 days"},
		{24 * time.Hour, "1 day"},
		{2 * time.Hour, "2 hours"},
		{1 * time.Hour, "1 hour"},
		{30 * time.Minute, "30 minutes"},
		{1 * time.Minute, "1 minute"},
	}
	for _, tt := range tests {
		got := formatDurationDistance(tt.dur)
		if got != tt.want {
			t.Errorf("formatDurationDistance(%v) = %q, want %q", tt.dur, got, tt.want)
		}
	}
}

// TestPluralS tests the pluralS helper.
func TestPluralS(t *testing.T) {
	if got := pluralS(0); got != "s" {
		t.Errorf("pluralS(0) = %q, want \"s\"", got)
	}
	if got := pluralS(1); got != "" {
		t.Errorf("pluralS(1) = %q, want \"\"", got)
	}
	if got := pluralS(2); got != "s" {
		t.Errorf("pluralS(2) = %q, want \"s\"", got)
	}
}

// TestVetoDescriptionCoverage: every veto code declared in
// internal/match/veto.go, plus "fact_did_not_close", has a one-sentence plain
// description rather than the code-with-spaces fallback (M5-06). The codes are
// read from veto.go's source, so a new code fails here until it is described.
func TestVetoDescriptionCoverage(t *testing.T) {
	codes := append(matchVetoCodes(t), "fact_did_not_close")
	for _, code := range codes {
		got := vetoDescription(code)
		if got == strings.ReplaceAll(code, "_", " ") {
			t.Errorf("vetoDescription(%q) falls back to the code", code)
		}
		if !strings.HasSuffix(got, ".") || strings.Count(got, ". ") > 0 {
			t.Errorf("vetoDescription(%q) = %q, want one sentence", code, got)
		}
	}
	// The dead direction_mismatch code (never emitted) is gone; unknown codes fall back.
	for _, code := range []string{"direction_mismatch", "unknown_code"} {
		if got, want := vetoDescription(code), strings.ReplaceAll(code, "_", " "); got != want {
			t.Errorf("vetoDescription(%q) = %q, want fallback %q", code, got, want)
		}
	}
}

// matchVetoCodes returns the string values of the Veto* constants declared
// in internal/match/veto.go, in source order. The web package must not import
// match's internals for this, so it reads the source.
func matchVetoCodes(t *testing.T) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), filepath.Join("..", "match", "veto.go"), nil, 0)
	if err != nil {
		t.Fatalf("parsing veto.go: %v", err)
	}
	var codes []string
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, name := range vs.Names {
				if !strings.HasPrefix(name.Name, "Veto") || i >= len(vs.Values) {
					continue
				}
				if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					v, err := strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatal(err)
					}
					codes = append(codes, v)
				}
			}
		}
	}
	if len(codes) < 15 {
		t.Fatalf("found %d veto codes in veto.go, want at least 15: %v", len(codes), codes)
	}
	return codes
}

// TestNearMissVetoFilter verifies ?veto= filters to a single group (M5-10).
func TestNearMissVetoFilter(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	seedMore(t, s, st, sd)

	// Default view has multiple groups
	rec := get(s.Handler(), "/near-misses")
	wantHTML(t, rec, 200)
	body := rec.Body.String()
	wantContains(t, body, `id="group-subject-mismatch"`, `id="group-fact-did-not-close"`)

	// Filtered view shows only subject_mismatch group
	rec = get(s.Handler(), "/near-misses?veto=subject_mismatch")
	wantHTML(t, rec, 200)
	body = rec.Body.String()
	wantContains(t, body, `id="group-subject-mismatch"`)
	if strings.Contains(body, `id="group-fact-did-not-close"`) {
		t.Error("filtered view should not show fact_did_not_close group")
	}
}

// TestNearMissInvalidVeto verifies unknown veto code returns 400 (M5-10).
func TestNearMissInvalidVeto(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	seedMore(t, s, st, sd)

	rec := get(s.Handler(), "/near-misses?veto=nonexistent_code")
	if rec.Code != 400 {
		t.Errorf("GET /near-misses?veto=nonexistent_code = %d, want 400", rec.Code)
	}
	wantContains(t, rec.Body.String(), "Unknown veto code")
}

// TestNearMissGroupCapLogic verifies the 25-row cap logic directly (M5-10).
func TestNearMissGroupCapLogic(t *testing.T) {
	// Create 30 rows with the same veto
	views := map[string]instrumentView{"a": {}, "b": {}}
	var rels []canonical.Relationship
	for i := 0; i < 30; i++ {
		rels = append(rels, canonical.Relationship{
			ID: int64(i + 1), AID: "a", BID: "b", Type: canonical.RelRelated, Confidence: dec("0.5"),
			Vetoes: []canonical.Veto{{Code: "threshold_mismatch", Field: "threshold", Detail: "0.5 vs 0.6"}},
		})
	}
	rows := nearMissRows(rels, views)

	// Default view (showAll=false) should cap at 25
	groups := groupNearMisses(rows, "", false)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	g := groups[0]
	if g.Code != "threshold_mismatch" {
		t.Errorf("group code = %q, want threshold_mismatch", g.Code)
	}
	if g.Count != 30 {
		t.Errorf("group count = %d, want 30", g.Count)
	}
	if len(g.Rows) != 25 {
		t.Errorf("group rows = %d, want 25 (capped)", len(g.Rows))
	}
	if g.Truncated != 5 {
		t.Errorf("group truncated = %d, want 5", g.Truncated)
	}
	if g.ShowAllLink == "" {
		t.Error("expected ShowAllLink to be set")
	}

	// showAll=true should show all 30
	groupsAll := groupNearMisses(rows, "", true)
	if len(groupsAll) != 1 {
		t.Fatalf("expected 1 group with all=true, got %d", len(groupsAll))
	}
	gAll := groupsAll[0]
	if len(gAll.Rows) != 30 {
		t.Errorf("group rows with all=true = %d, want 30", len(gAll.Rows))
	}
	if gAll.Truncated != 0 {
		t.Errorf("group truncated with all=true = %d, want 0", gAll.Truncated)
	}
}

// TestNearMissGroupCap verifies 25-row cap per group with "Show all" link (M5-10).
func TestNearMissGroupCap(t *testing.T) {
	// Simplified integration test - the unit test TestNearMissGroupCapLogic verifies capping logic
	s, st := newTestServer(t)
	seedPublic(t, st)

	rec := get(s.Handler(), "/near-misses")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// Should have the legend
	wantContains(t, body, "<strong>Legend:</strong>", "How close: the gap between the two thresholds")

	// Should have groups with plain headings
	wantContains(t, body, `id="group-subject-mismatch"`)
	wantContains(t, body, "Different underlying subject")

	// Test query parameter filtering
	rec = get(s.Handler(), "/near-misses?veto=subject_mismatch")
	wantHTML(t, rec, 200)
	body = rec.Body.String()
	wantContains(t, body, `id="group-subject-mismatch"`)

	// Test invalid veto parameter returns 400
	rec = get(s.Handler(), "/near-misses?veto=invalid_code")
	if rec.Code != 400 {
		t.Errorf("GET /near-misses?veto=invalid_code = %d, want 400", rec.Code)
	}
	wantContains(t, rec.Body.String(), "Unknown veto code")
}

// TestComparatorToWords verifies comparator codes are rendered as words (M5-10).
func TestComparatorToWords(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"gt vs lt", "above vs below"},
		{"gte vs lte", "at or above vs at or below"},
		{"eq", "equal to"},
		{"between", "between"},
		{"some other text", "some other text"},
	}
	for _, tt := range tests {
		got := comparatorToWords(tt.input)
		if got != tt.want {
			t.Errorf("comparatorToWords(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestNearMissComparatorRendering verifies comparators render as words in the page (M5-10).
func TestNearMissComparatorRendering(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)

	// Create a pair with comparator_mismatch veto
	ctx := t.Context()
	aID := "kalshi:COMPTEST"
	bID := "polymarket:COMPTEST"
	threshold := dec("1.0")
	// Create instruments first (foreign key requirement)
	if err := st.UpsertInstrument(ctx, sd.round.ID, canonical.Instrument{
		ID:            aID,
		Venue:         canonical.VenueKalshi,
		VenueMarketID: "mktcomp",
		Category:      canonical.CategoryCrypto,
		Title:         "Test comparator",
		Status:        canonical.StatusOpen,
		Terms:         canonical.Terms{Subject: "BTC", Metric: "price", Comparator: canonical.ComparatorGTE, Threshold: &threshold, Unit: "USD", DeadlineTZ: "UTC", ResolutionSource: "test", EdgeRules: canonical.Unknown, Polarity: canonical.PolarityPositive},
		FeeSchedule:   canonical.FeeSchedule{ID: canonical.FeeScheduleKalshiTakerV1},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertInstrument(ctx, sd.round.ID, canonical.Instrument{
		ID:            bID,
		Venue:         canonical.VenuePolymarket,
		VenueMarketID: "mktcomp",
		Category:      canonical.CategoryCrypto,
		Title:         "Test comparator",
		Status:        canonical.StatusOpen,
		Terms:         canonical.Terms{Subject: "BTC", Metric: "price", Comparator: canonical.ComparatorLT, Threshold: &threshold, Unit: "USD", DeadlineTZ: "UTC", ResolutionSource: "test", EdgeRules: canonical.Unknown, Polarity: canonical.PolarityPositive},
		FeeSchedule:   canonical.FeeSchedule{ID: canonical.FeeSchedulePolymarketV1},
	}); err != nil {
		t.Fatal(err)
	}
	_, err := st.UpsertRelationship(ctx, canonical.Relationship{
		AID:         aID,
		BID:         bID,
		Type:        canonical.RelRelated,
		Method:      canonical.MethodRules,
		Confidence:  dec("0.5"),
		Explanation: "test",
		Vetoes:      []canonical.Veto{{Code: "comparator_mismatch", Field: "comparator", Detail: "gt vs lt"}},
		RoundID:     sd.round.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/near-misses")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// Should show "above vs below" instead of "gt vs lt"
	wantContains(t, body, "above vs below")
	if strings.Contains(body, "gt vs lt") {
		t.Error("should not show raw 'gt vs lt' in comparator detail")
	}
}

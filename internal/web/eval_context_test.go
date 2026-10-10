package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/eval"
)

// committedEvalServer serves /eval from the committed artifacts prod uses.
func committedEvalServer(t *testing.T, withStore bool) (*Server, seeded) {
	t.Helper()
	cfg := Config{EvalSummaryFile: "../../config/eval_summary.json", BacktestSummaryFile: "../../config/backtest_summary.json"}
	if !withStore {
		return New(cfg, Deps{}), seeded{}
	}
	_, st := newTestServer(t)
	sd := seedPublic(t, st)
	return New(cfg, Deps{Store: st, Now: func() time.Time { return seedNow.Add(3 * time.Minute) }}), sd
}

// TestEvalStatesLabelledSetNotPinnedRound: /eval says the scores come from
// the labelled set, gives the labels file's provenance round as such, and
// separates it from the pinned demo round; it no longer says the scores
// are "on round N" (P11).
func TestEvalStatesLabelledSetNotPinnedRound(t *testing.T) {
	s, sd := committedEvalServer(t, true)
	if err := s.deps.Store.PinRound(context.Background(), sd.round.ID); err != nil {
		t.Fatal(err)
	}
	body := get(s.Handler(), "/eval").Body.String()
	wantContains(t, body,
		"computed by <code>equinox eval</code> over the human-confirmed labels in <code>testdata/golden/labels.yaml</code>",
		"the run classified the two contracts' stored terms with the rules matcher and compared the verdict with the human label",
		"<strong>95</strong> labelled pairs, <strong>90</strong> scored (<a href=\"#unscored\">5 not scored</a>), method <code>rules</code>",
		"The labels file records round 2 as the round its first pairs were drafted from.",
		"These scores are separate from the pinned demo round "+strconv.FormatInt(sd.round.ID, 10),
	)
	if strings.Contains(body, "on round <strong>") {
		t.Error("/eval still says the scores are on a round")
	}
}

// TestEvalExamplesAreLabelled: every example pair is in labels.yaml with
// the type it illustrates, each type has one or two, and none repeats (P11).
func TestEvalExamplesAreLabelled(t *testing.T) {
	labels, err := eval.LoadLabels("../../testdata/golden/labels.yaml")
	if err != nil {
		t.Fatal(err)
	}
	byPair := map[string]canonical.RelationshipType{}
	for _, l := range labels.Labels {
		byPair[eval.PairKey(l.AID, l.BID)] = l.Label
	}
	seen := map[string]bool{}
	for _, typ := range canonical.RelationshipTypes() {
		exs := evalExamples[typ]
		if len(exs) < 1 || len(exs) > 2 {
			t.Errorf("%s has %d examples, want 1 or 2", typ, len(exs))
		}
		for _, ex := range exs {
			key := eval.PairKey(ex.AID, ex.BID)
			if got, ok := byPair[key]; !ok {
				t.Errorf("%s example %s / %s is not in labels.yaml", typ, ex.AID, ex.BID)
			} else if got != typ {
				t.Errorf("%s example %s / %s is labelled %s", typ, ex.AID, ex.BID, got)
			}
			if seen[key] {
				t.Errorf("example %s listed twice", key)
			}
			seen[key] = true
			if ex.Desc == "" {
				t.Errorf("%s example %s has no description", typ, ex.AID)
			}
		}
	}
}

// TestEvalExamplesLinkPairsInRound: each type row links to its examples,
// an example stored in the current round links its pair page (resolved by
// instrument ids), and one that is not says so (P11).
func TestEvalExamplesLinkPairsInRound(t *testing.T) {
	saved := evalExamples
	t.Cleanup(func() { evalExamples = saved })
	evalExamples = map[canonical.RelationshipType][]evalExample{
		canonical.RelEquivalent: {
			{seedPolyB, seedKalshiA, "the seeded equivalent pair, ids given in reverse"},
			{"kalshi:KXNOPE-1", "polymarket:1", "a pair the round does not hold"},
		},
	}
	s, sd := committedEvalServer(t, true)
	body := get(s.Handler(), "/eval").Body.String()
	wantContains(t, body,
		`<a href="#examples-equivalent" class="small">examples</a>`,
		`<h2 id="examples">Example labelled pairs</h2>`,
		`<dt id="examples-equivalent">`,
		`<a href="/pairs/`+strconv.FormatInt(sd.eqID, 10)+`">the seeded equivalent pair, ids given in reverse</a>`,
		`a pair the round does not hold <small class="note">(not stored in this round; Kalshi <code>kalshi:KXNOPE-1</code>)</small>`,
	)
	if strings.Contains(body, `href="#examples-implies"`) {
		t.Error("a type with no examples links to an examples anchor")
	}
}

// TestEvalExamplesWithoutStore: with no database the examples still render,
// unlinked, so /eval never fails on them (P11).
func TestEvalExamplesWithoutStore(t *testing.T) {
	s, _ := committedEvalServer(t, false)
	rec := get(s.Handler(), "/eval")
	wantHTML(t, rec, http.StatusOK)
	wantContains(t, rec.Body.String(), "Newsom wins the 2028 Democratic nomination, on both venues <small class=\"note\">(not stored in this round")
}

// TestEvalUnrelatedPrecisionNote: under unrelated, a line explains that a
// false positive there is an under-claim (the safe direction); it appears
// only when unrelated has false positives (P11).
func TestEvalUnrelatedPrecisionNote(t *testing.T) {
	s, _ := committedEvalServer(t, false)
	body := get(s.Handler(), "/eval").Body.String()
	wantContains(t, body,
		`<tr class="row-note"><td colspan="8"><small>Reading this row: 13 pairs the matcher called <code>unrelated</code> carry a stronger human label.`,
		"none of them can be routed: the safe direction for a precision-first matcher",
		"their subjects differ and blocking calls it <code>unrelated</code>",
	)
	// The note follows the unrelated row, not another type's.
	if strings.Index(body, `<tr class="row-note">`) < strings.Index(body, `<span class="badge b-unrelated">`) {
		t.Error("the unrelated note renders before the unrelated row")
	}
	noFP := strings.Replace(sampleEval, `{"type": "implies"`, `{"type": "unrelated", "support": 2, "tp": 2, "fp": 0, "fn": 0, "precision": "1", "recall": "1"}, {"type": "implies"`, 1)
	if b := get(New(Config{EvalSummaryFile: writeEval(t, noFP)}, Deps{}).Handler(), "/eval").Body.String(); strings.Contains(b, "row-note") {
		t.Error("unrelated note shown with no unrelated false positives")
	}
}

// TestEvalUnscoredLabelsListed: each label the run could not score is listed
// with its reason, and the old collapsed note is gone (P11).
func TestEvalUnscoredLabelsListed(t *testing.T) {
	s, _ := committedEvalServer(t, false)
	body := get(s.Handler(), "/eval").Body.String()
	wantContains(t, body, `<h2 id="unscored">Labels not scored</h2>`, "5 of the 95 labels could not be scored, so the table covers 90.")
	if got := strings.Count(body, "The Kalshi contract&#39;s ticker date is 8 Oct 2026, before this run"); got != 5 {
		t.Errorf("unscored reasons = %d, want 5", got)
	}
	wantContains(t, body, `<code title="kalshi:KXBTCD-26OCT0812-T77999.99">kalshi:KXBTCD-26OCT0812-T77999.99</code>`,
		`<code title="polymarket:77443519844770925744859368185756294247896766669594679461676958360391934252270">polymarket:77443519844…252270</code>`)
	if strings.Contains(body, "recorded fixtures don&#39;t include one side") {
		t.Error("the old collapsed not-found note is still rendered")
	}
}

func TestUnscoredLabels(t *testing.T) {
	run := time.Date(2026, 10, 9, 22, 0, 0, 0, time.UTC)
	got := unscoredLabels([]string{
		"equivalent precision 1.0000 on 9 predictions meets the 0.95 target (NFR-06)",
		"not evaluated: kalshi:KXBTCD-26OCT0812-T1 / polymarket:9 (instrument not found)",
		"not evaluated: kalshi:KXFED-26DEC-T3.50 / polymarket:8 (instrument not found)",
		"not evaluated: malformed (instrument not found)",
	}, run)
	if len(got) != 2 {
		t.Fatalf("got %d unscored, want 2: %+v", len(got), got)
	}
	if got[0].AID != "kalshi:KXBTCD-26OCT0812-T1" || got[0].BID != "polymarket:9" || !strings.Contains(got[0].Reason, "ticker date is 8 Oct 2026") {
		t.Errorf("first = %+v", got[0])
	}
	if !strings.HasPrefix(got[1].Reason, "At least one of the two contracts is not in the database") {
		t.Errorf("a ticker with no date code got %q", got[1].Reason)
	}
	// A ticker date after the run is not called resolved.
	if r := unscoredReason("kalshi:KXRATECUTCOUNT-26DEC31-T0", "polymarket:1", run); strings.Contains(r, "ticker date") {
		t.Errorf("future ticker date called resolved: %q", r)
	}
}

func TestKalshiTickerDate(t *testing.T) {
	for id, want := range map[string]string{
		"kalshi:KXBTCD-26OCT0812-T77999.99": "2026-10-08",
		"kalshi:KXRATECUTCOUNT-26DEC31-T0":  "2026-12-31",
		"kalshi:KXFED-26OCT-T4.25":          "",
		"kalshi:KXPRESNOMD-28-GN":           "",
		"polymarket:26OCT08":                "",
		"kalshi:KXBTCD-26XYZ08-T1":          "",
	} {
		d, ok := kalshiTickerDate(id)
		got := ""
		if ok {
			got = d.Format("2006-01-02")
		}
		if got != want {
			t.Errorf("kalshiTickerDate(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestShortInstrumentID(t *testing.T) {
	if got := shortInstrumentID("kalshi:KXFED-26OCT-T4.25"); got != "kalshi:KXFED-26OCT-T4.25" {
		t.Errorf("short id changed: %q", got)
	}
	long := "polymarket:18246296022868501258060085368606280205967114341141276439803424448463050819498"
	if got := shortInstrumentID(long); got != "polymarket:18246296022…819498" {
		t.Errorf("long id = %q", got)
	}
}

// TestEvalMeterCellsStayTableCells: the precision and recall cells keep
// table-cell layout, so each value sits under its own header (P11: a
// display:flex td had pushed F1 into the Recall column).
func TestEvalMeterCellsStayTableCells(t *testing.T) {
	if !strings.Contains(string(appCSS), "td.meter-cell { display: table-cell;") {
		t.Error("app.css lacks the td.meter-cell table-cell override")
	}
}

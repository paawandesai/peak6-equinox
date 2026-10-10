package web

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// storeNotRouted stores n identical not-routed decisions on target, one
// second apart starting at start, and returns their ids oldest first.
func storeNotRouted(t *testing.T, st *store.Store, roundID int64, target string, n int, start time.Time) []int64 {
	t.Helper()
	ctx := context.Background()
	qty := dec("100")
	var ids []int64
	for i := 0; i < n; i++ {
		at := start.Add(time.Duration(i) * time.Second)
		d := canonical.Decision{RoundID: roundID, TargetID: target, TargetType: canonical.RelEquivalent, DecidedAt: at,
			Order:       canonical.Order{Side: canonical.SideBuy, Outcome: canonical.OutcomeYes, Quantity: &qty, Type: canonical.OrderMarket, TIF: canonical.TIFIOC},
			Exclusions:  []canonical.Exclusion{{InstrumentID: target, Venue: canonical.VenueOf(target), Reason: "insufficient_depth", Detail: "0 contracts available on consumed side"}},
			Explanation: "No venue can fill.",
		}
		id, err := st.InsertRoutingDecision(ctx, d)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.AppendLog(ctx, store.LogRouting, strconv.FormatInt(id, 10), routeLogSummary(d), d, at); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

// TestLogFoldsIdenticalRouting (M6 P7): 15 identical consecutive "Not
// routed" decisions show as one row "×15" linking to the newest; a different
// decision stays its own row; the default view is verdicts & routing, newest
// first.
func TestLogFoldsIdenticalRouting(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	base := seedNow.Add(10 * time.Minute)
	ids := storeNotRouted(t, st, sd.round.ID, seedKalshiA, 15, base)
	other := storeNotRouted(t, st, sd.round.ID, seedPolyB, 1, base.Add(time.Minute))
	newest := strconv.FormatInt(ids[len(ids)-1], 10)

	for _, path := range []string{"/log", "/log?kind=routing", "/log?kind=all"} {
		body := get(s.Handler(), path).Body.String()
		wantContains(t, body,
			`<a href="/decisions/`+newest+`" class="chip chip-muted" title="15 identical decisions in a row; this links to the newest">×15</a>`,
			"14 repeated routing decisions are folded into the newest identical row")
		if n := strings.Count(body, "no venue can fill (1 excluded)"); n != 2 {
			t.Errorf("%s: %d not-routed summary rows, want 2 (the fold and the other target)", path, n)
		}
		// Newest first: the other target (a minute later) comes before the fold.
		if strings.Index(body, "/decisions/"+strconv.FormatInt(other[0], 10)) > strings.Index(body, "/decisions/"+newest) {
			t.Errorf("%s: not newest first", path)
		}
		// The older folded decisions are not listed as rows.
		if strings.Contains(body, `href="/decisions/`+strconv.FormatInt(ids[0], 10)+`"`) {
			t.Errorf("%s: oldest folded decision still has its own row", path)
		}
	}
	body := get(s.Handler(), "/log").Body.String()
	// The panel heading ("Latest decisions", formerly "Outliers") is checked in TestLogOutliers.
	wantContains(t, body, `<a href="/log" class="chip-filter" aria-current="page">verdicts &amp; routing</a>`)
	// Grouped routing view still works and keeps the fold.
	wantContains(t, get(s.Handler(), "/log?kind=routing&group=venue").Body.String(), "×15")
}

// TestFoldRepeatedRouting: only consecutive identical routing rows fold.
func TestFoldRepeatedRouting(t *testing.T) {
	r := func(sum string, pair int64) logEntryView {
		return logEntryView{Kind: string(store.LogRouting), Summary: sum, Routing: &routingLogView{PairID: pair, Outcome: Badge{Text: "Not routed"}}}
	}
	rel := logEntryView{Kind: string(store.LogRelationship), Summary: "a"}
	in := []logEntryView{r("a", 1), r("a", 1), r("a", 2), rel, rel, r("a", 2), r("b", 2),
		{Kind: string(store.LogRouting), Summary: "c"}, {Kind: string(store.LogRouting), Summary: "c"}, r("c", 0)}
	out := foldRepeatedRouting(in)
	var got []int
	for _, e := range out {
		got = append(got, e.Repeat)
	}
	want := []int{2, 1, 1, 1, 1, 1, 2, 1}
	if len(got) != len(want) {
		t.Fatalf("repeats = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("repeats = %v, want %v", got, want)
		}
	}
}

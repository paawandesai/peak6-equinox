package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// historyPoints makes n points per instrument, a minute apart, with A and B
// fetched at alternating times so a per-venue table would not interleave.
func historyPoints(n int) []store.QuotePoint {
	var pts []store.QuotePoint
	mid := decimal.NullDecimal{Decimal: dec("0.5"), Valid: true}
	for i := range n {
		at := seedNow.Add(time.Duration(i) * time.Minute)
		pts = append(pts,
			store.QuotePoint{RoundID: int64(i + 1), InstrumentID: "kalshi:A", FetchedAt: at, Mid: mid, BestBid: mid, BestAsk: mid},
			store.QuotePoint{RoundID: int64(i + 1), InstrumentID: "polymarket:B", FetchedAt: at.Add(10 * time.Second), Mid: mid, BestBid: mid, BestAsk: mid})
	}
	return pts
}

// TestHistoryRowsInterleaved: the data table lists both venues newest first,
// so the 50-row cap (D-116) shows the most recent points of both (M6 P5).
func TestHistoryRowsInterleaved(t *testing.T) {
	c := buildHistoryChart("kalshi:A", "polymarket:B", canonical.RelEquivalent, historyPoints(60), TzUTC)
	if len(c.Rows) != 120 {
		t.Fatalf("rows = %d, want 120", len(c.Rows))
	}
	for i := 1; i < len(c.Rows); i++ {
		if c.Rows[i].At.After(c.Rows[i-1].At) {
			t.Fatalf("row %d (%v) is newer than row %d (%v)", i, c.Rows[i].At, i-1, c.Rows[i-1].At)
		}
	}
	venues := map[string]int{}
	for _, r := range c.Rows[:50] {
		venues[r.Venue]++
	}
	if venues["kalshi"] != 25 || venues["polymarket"] != 25 {
		t.Errorf("first 50 rows by venue = %v, want 25 each", venues)
	}
	if !c.Rows[0].At.Equal(seedNow.Add(59*time.Minute + 10*time.Second)) {
		t.Errorf("first row at %v, want the newest point", c.Rows[0].At)
	}
}

// TestHistoryMarkMatched: the match time, the count of later (live) points
// and the chart marker (M6 P5).
func TestHistoryMarkMatched(t *testing.T) {
	c := buildHistoryChart("kalshi:A", "polymarket:B", canonical.RelEquivalent, historyPoints(10), TzUTC)
	c.markMatched(seedNow.Add(4*time.Minute + 30*time.Second))
	if c.LaterPoints != 10 || c.PinX == "" {
		t.Errorf("LaterPoints = %d, PinX = %q; want 10 and a marker", c.LaterPoints, c.PinX)
	}
	before := buildHistoryChart("kalshi:A", "polymarket:B", canonical.RelEquivalent, historyPoints(3), TzUTC)
	before.markMatched(seedNow.Add(-time.Hour))
	if before.LaterPoints != 6 || before.PinX != "" {
		t.Errorf("match before the span: LaterPoints = %d, PinX = %q", before.LaterPoints, before.PinX)
	}
	empty := historyChart{Empty: true}
	empty.markMatched(seedNow)
	if !empty.MatchedAt.IsZero() {
		t.Error("empty chart marked")
	}
}

func TestFactDate(t *testing.T) {
	if got := factDate(time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)); got != "2026-09-16" {
		t.Errorf("factDate = %q", got)
	}
	if got := factDate(time.Time{}); got != "unknown" {
		t.Errorf("factDate(zero) = %q", got)
	}
}

// TestPairPinnedLabels: the history names the match time in the selected
// timezone and the live points after it; fact as-of dates read as the
// calendar dates the explanation uses, in every timezone (M6 P5).
func TestPairPinnedLabels(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	ctx := context.Background()
	rel, err := st.GetRelationship(ctx, sd.eqID)
	if err != nil {
		t.Fatal(err)
	}
	asOf := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	rel.FactsUsed = []canonical.FactUse{{Key: "fed_upper_bound", Value: "4.00", Source: "https://www.federalreserve.gov/", AsOf: asOf}}
	rel.Explanation = "Facts used: fed_upper_bound=4.00 (as of 2026-09-16)."
	if _, err := st.UpsertRelationship(ctx, rel); err != nil {
		t.Fatal(err)
	}
	// A later round's quotes: live collection after the match.
	round2, err := st.CreateRound(ctx, seedNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{rel.AID, rel.BID} {
		q := canonical.Quote{InstrumentID: id, FetchedAt: seedNow.Add(time.Hour),
			YesBids: []canonical.Level{{Price: dec("0.51"), Size: dec("100")}}, YesAsks: []canonical.Level{{Price: dec("0.52"), Size: dec("100")}}}
		if err := st.InsertQuote(ctx, round2.ID, q); err != nil {
			t.Fatal(err)
		}
	}
	for tz, matched := range map[string]string{
		TzET:  formatTime(sd.round.SnapshotAt(), TzET),
		TzUTC: formatTime(sd.round.SnapshotAt(), TzUTC),
	} {
		req, _ := http.NewRequest(http.MethodGet, "/pairs/"+strconv.FormatInt(sd.eqID, 10), nil)
		//nolint:gosec // G124: test cookie needs no security attributes
		req.AddCookie(&http.Cookie{Name: timezoneCookie, Value: tz})
		rec := serve(s.Handler(), req)
		wantHTML(t, rec, http.StatusOK)
		body := rec.Body.String()
		wantContains(t, body,
			"Pairs matched at "+matched+"; later points (2) are live collection.",
			`<time datetime="2026-09-16">2026-09-16</time>`,
			"(as of 2026-09-16)",
			"Both venues, newest first.",
		)
		if strings.Contains(body, "2026-09-15") {
			t.Errorf("tz %s: fact as-of shifted to the previous day", tz)
		}
	}
}

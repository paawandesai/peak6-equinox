package web

import (
	"context"
	"fmt"
	"html/template"
	"net/url"
	"strings"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/match"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// TestQuoteAgeAt pins the one freshness rule (M6 P3): age against the
// reference, stale exactly when the router excludes (age > staleAfter).
func TestQuoteAgeAt(t *testing.T) {
	ref := seedNow
	stale := 120 * time.Second
	tests := []struct {
		name    string
		ref     time.Time
		fetched time.Time
		age     time.Duration
		want    FreshState
	}{
		{"zero reference", time.Time{}, ref, 0, FreshStateUnknown},
		{"zero fetch", ref, time.Time{}, 0, FreshStateUnknown},
		{"fetched after reference", ref, ref.Add(time.Second), 0, FreshStateFresh},
		{"at the limit", ref, ref.Add(-stale), stale, FreshStateFresh},
		{"past the limit", ref, ref.Add(-stale - time.Second), stale + time.Second, FreshStateStale},
		{"well inside, no warn state", ref, ref.Add(-90 * time.Second), 90 * time.Second, FreshStateFresh},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			age, st := quoteAgeAt(tt.ref, tt.fetched, stale)
			if age != tt.age || st != tt.want {
				t.Errorf("quoteAgeAt = %v, %q; want %v, %q", age, st, tt.age, tt.want)
			}
		})
	}
	if got := worseFresh(FreshStateUnknown, FreshStateFresh); got != FreshStateFresh {
		t.Errorf("worseFresh(unknown, fresh) = %q", got)
	}
	if got := worseFresh(FreshStateStale, FreshStateFresh); got != FreshStateStale {
		t.Errorf("worseFresh(stale, fresh) = %q", got)
	}
}

// seedFreshnessRound stores one finished round (snapshot seedNow+1m) with a
// routable Kalshi/Polymarket pair. The Kalshi book has bids only; its quote is
// fetched kalshiAge before the snapshot. The wall clock is an hour later, so a
// rule measured against now would call every quote stale.
func seedFreshnessRound(t *testing.T, kalshiAge time.Duration) (*Server, int64) {
	t.Helper()
	_, st := newTestServer(t)
	s := New(Config{MinRouteConfidence: dec("0.9"), StaleAfter: 120 * time.Second},
		Deps{Store: st, Now: func() time.Time { return seedNow.Add(time.Hour) }})
	ctx := context.Background()
	round, err := st.CreateRound(ctx, seedNow)
	if err != nil {
		t.Fatal(err)
	}
	snap := seedNow.Add(time.Minute)
	if err := st.FinishRound(ctx, round.ID, store.RoundOK, nil, snap); err != nil {
		t.Fatal(err)
	}
	a := seedInstrument("kalshi:KB", canonical.VenueKalshi, "Kalshi bid-only", "rules", "BTC", "src", kalshiFee)
	b := seedInstrument("polymarket:PB", canonical.VenuePolymarket, "Poly two-sided", "rules", "BTC", "src", polyFee)
	for _, in := range []canonical.Instrument{a, b} {
		if err := st.UpsertInstrument(ctx, round.ID, in); err != nil {
			t.Fatal(err)
		}
	}
	quotes := []canonical.Quote{
		{InstrumentID: a.ID, YesBids: []canonical.Level{{Price: dec("0.40"), Size: dec("100")}}, FetchedAt: snap.Add(-kalshiAge)},
		{InstrumentID: b.ID, YesBids: []canonical.Level{{Price: dec("0.41"), Size: dec("100")}},
			YesAsks: []canonical.Level{{Price: dec("0.43"), Size: dec("500")}}, FetchedAt: snap.Add(-5 * time.Second)},
	}
	for _, q := range quotes {
		if err := st.InsertQuote(ctx, round.ID, q); err != nil {
			t.Fatal(err)
		}
	}
	id, err := st.UpsertRelationship(ctx, canonical.Relationship{
		RoundID: round.ID, AID: a.ID, BID: b.ID, Type: canonical.RelEquivalent, Confidence: dec("0.97"),
		Method: canonical.MethodRules, Diff: match.Diff(a.Terms, b.Terms), Explanation: "same bet",
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, id
}

// TestMatchesKalshiBidOnlyBook: a bid-only Kalshi book reads as one-sided,
// not as a missing or stale quote, on / and on the pair page (M6 P3).
func TestMatchesKalshiBidOnlyBook(t *testing.T) {
	s, id := seedFreshnessRound(t, 30*time.Second)
	rec := get(s.Handler(), "/")
	wantHTML(t, rec, 200)
	body := rec.Body.String()
	wantContains(t, body, "no gap: kalshi one-sided: no YES ask", `class="badge f-fresh"`)
	for _, bad := range []string{"no kalshi quote", "no quote: kalshi", "f-stale"} {
		if strings.Contains(body, bad) {
			t.Errorf("matches page shows %q for a fresh bid-only book", bad)
		}
	}

	rec = get(s.Handler(), fmt.Sprintf("/pairs/%d", id))
	wantHTML(t, rec, 200)
	body = rec.Body.String()
	wantContains(t, body, "one-sided: no YES ask", "Ages are measured against that round")
	if strings.Contains(body, "f-stale") {
		t.Error("pair page calls a quote stale that routing treats as fresh")
	}
}

// TestFreshnessMatchesTraceAgree: for the same stored quotes the matches
// badge, the pair page badge and the route trace badge are the same state and
// text, fresh or stale (M6 P3).
func TestFreshnessMatchesTraceAgree(t *testing.T) {
	for _, tc := range []struct {
		name      string
		kalshiAge time.Duration
		state     FreshState
	}{
		{"fresh", 30 * time.Second, FreshStateFresh},
		{"stale", 5 * time.Minute, FreshStateStale},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, id := seedFreshnessRound(t, tc.kalshiAge)
			want := renderFBadge(t, FreshBadgeAged(tc.state, tc.kalshiAge, 120*time.Second, FreshKindQuote))

			matches := get(s.Handler(), "/").Body.String()
			pair := get(s.Handler(), fmt.Sprintf("/pairs/%d", id)).Body.String()
			form := url.Values{"pair": {fmt.Sprint(id)}, "target": {"polymarket:PB"}, "side": {"buy"}, "outcome": {"yes"},
				"quantity": {"10"}, "type": {"market"}, "tif": {"IOC"}}
			rec := postForm(s.Handler(), "/route", form, true)
			if rec.Code != 200 {
				t.Fatalf("route status %d: %s", rec.Code, rec.Body.String())
			}
			trace := rec.Body.String()
			for name, body := range map[string]string{"matches": matches, "pair": pair, "trace": trace} {
				if !strings.Contains(body, want) {
					t.Errorf("%s page lacks badge %s", name, want)
				}
			}
		})
	}
}

// renderFBadge renders a badge with the fbadge template, as every page does.
func renderFBadge(t *testing.T, b Badge) string {
	t.Helper()
	tpl := template.Must(template.New("x").Parse(`<span class="badge {{.Class}}"{{if .Title}} title="{{.Title}}"{{end}}><span class="g" aria-hidden="true">{{.Glyph}}</span>{{.Text}}</span>`))
	var sb strings.Builder
	if err := tpl.Execute(&sb, b); err != nil {
		t.Fatal(err)
	}
	return sb.String()
}

type roundsByID map[int64]store.Round

func (m roundsByID) GetRound(_ context.Context, id int64) (store.Round, error) {
	r, ok := m[id]
	if !ok {
		return store.Round{}, fmt.Errorf("round %d: %w", id, store.ErrNotFound)
	}
	return r, nil
}

// TestStoredDecisionSnapshot: a stored decision's ages are measured from the
// newest input round's snapshot, as routing measured them (M6 P3).
func TestStoredDecisionSnapshot(t *testing.T) {
	pinned := seedNow
	live := seedNow.Add(time.Hour)
	rounds := roundsByID{7: {ID: 7, FinishedAt: live}}
	d := canonical.Decision{RoundID: 3, Inputs: []canonical.DecisionInput{
		{RoundID: 7, FetchedAt: live.Add(-40 * time.Second)},
		{RoundID: 3, FetchedAt: pinned.Add(-time.Second)},
	}}
	if got := storedDecisionSnapshot(context.Background(), rounds, d, pinned); !got.Equal(live) {
		t.Errorf("got %v, want the live round's snapshot %v", got, live)
	}
	// The input round is gone: fall back to decisionSnapshot.
	if got := storedDecisionSnapshot(context.Background(), roundsByID{}, d, pinned); !got.Equal(live.Add(-40 * time.Second)) {
		t.Errorf("fallback: got %v", got)
	}
	// Inputs from the decision's own round only.
	d.Inputs = d.Inputs[1:]
	if got := storedDecisionSnapshot(context.Background(), rounds, d, pinned); !got.Equal(pinned) {
		t.Errorf("own round: got %v, want %v", got, pinned)
	}
}

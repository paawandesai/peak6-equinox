package polymarket

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues"
)

// fixtureDir holds the responses recorded with `equinox record-fixtures` on 2026-10-06.
var fixtureDir = filepath.Join("..", "..", "..", "testdata", "fixtures", "polymarket")

// recordedAt is roughly when the fixtures were recorded.
var recordedAt = time.Date(2026, 10, 6, 22, 0, 0, 0, time.UTC)

// fixtureServer serves testdata/fixtures/polymarket for one API, mapping each
// request to its file with venues.FixtureFileName, and 404 for anything else.
func fixtureServer(t *testing.T, api string, hits *atomic.Int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			hits.Add(1)
		}
		endpoint := api + r.URL.Path
		if r.URL.RawQuery != "" {
			endpoint += "?" + r.URL.RawQuery
		}
		body, err := os.ReadFile(filepath.Join(fixtureDir, venues.FixtureFileName(endpoint))) //nolint:gosec // FixtureFileName output is a plain file name
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"No orderbook exists for the requested token id"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body) //nolint:gosec // recorded JSON fixture served to the test client
	}))
	t.Cleanup(srv.Close)
	return srv
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func fixtureAdapter(t *testing.T) *Adapter {
	t.Helper()
	a, err := New(Options{
		GammaURL:     fixtureServer(t, apiGamma, nil).URL,
		ClobURL:      fixtureServer(t, apiClob, nil).URL,
		Timeout:      5 * time.Second,
		MaxBodyBytes: 10 << 20,
		Retries:      -1,
		Logger:       quietLogger(),
		Now:          func() time.Time { return recordedAt },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

func repoUniverse(t *testing.T) venues.Universe {
	t.Helper()
	us, err := venues.LoadUniverse(filepath.Join("..", "..", "..", "config", "universe"))
	if err != nil {
		t.Fatalf("LoadUniverse: %v", err)
	}
	return us.For(canonical.VenuePolymarket)
}

func TestFetchInstrumentsFixtures(t *testing.T) {
	a := fixtureAdapter(t)
	ins, raws, err := a.FetchInstruments(context.Background(), repoUniverse(t))
	if err != nil {
		t.Fatalf("FetchInstruments: %v", err)
	}
	if len(ins) < 100 {
		t.Fatalf("got %d instruments, want the recorded bitcoin/ethereum/fed-rates markets", len(ins))
	}
	byCat := map[canonical.Category]int{}
	ids := map[string]bool{}
	for _, in := range ins {
		if err := in.Validate(); err != nil {
			t.Errorf("invalid instrument: %v", err)
		}
		if ids[in.ID] {
			t.Errorf("duplicate instrument %s", in.ID)
		}
		ids[in.ID] = true
		byCat[in.Category]++
		if !strings.HasPrefix(in.ID, "polymarket:") || !tokenIDPattern.MatchString(strings.TrimPrefix(in.ID, "polymarket:")) {
			t.Errorf("id %q is not polymarket:<token id>", in.ID)
		}
		if !strings.HasPrefix(in.VenueMarketID, "0x") {
			t.Errorf("%s: venue_market_id %q is not a condition id", in.ID, in.VenueMarketID)
		}
		if in.VenueEventID == "" || in.Title == "" || in.Status != canonical.StatusOpen {
			t.Errorf("%s: event %q title %q status %q", in.ID, in.VenueEventID, in.Title, in.Status)
		}
		if in.FeeSchedule.ID != canonical.FeeSchedulePolymarketV1 {
			t.Errorf("%s: fee schedule %q", in.ID, in.FeeSchedule.ID)
		}
		// A few recorded politics markets have no endDate; the adapter must flag those.
		if in.CloseTime.IsZero() && !in.Quality.Has(canonical.QualityMissingField) {
			t.Errorf("%s: zero close time without a missing_field flag", in.ID)
		}
		if !in.CloseTime.IsZero() && in.CloseTime.Location() != time.UTC {
			t.Errorf("%s: close time %v not UTC", in.ID, in.CloseTime)
		}
		if in.Terms.Subject != canonical.Unknown {
			t.Errorf("%s: adapter must leave terms unknown, got %+v", in.ID, in.Terms)
		}
	}
	if byCat[canonical.CategoryCrypto] == 0 || byCat[canonical.CategoryFed] == 0 || byCat[canonical.CategoryPolitics] == 0 {
		t.Errorf("instruments per category = %v, want crypto, fed and politics", byCat)
	}
	if len(raws) < 3 {
		t.Errorf("got %d raw responses, want one per page", len(raws))
	}
	for _, r := range raws {
		if r.Venue != canonical.VenuePolymarket || !strings.HasPrefix(r.Endpoint, "gamma/events?") || r.HTTPStatus != 200 || len(r.Body) == 0 {
			t.Errorf("raw response %+v", r.Endpoint)
		}
	}
}

// TestFetchInstrumentsKnownMarket pins one recorded market field by field:
// the YES token is chosen by the outcome label, not by position.
func TestFetchInstrumentsKnownMarket(t *testing.T) {
	ins, _, err := fixtureAdapter(t).FetchInstruments(context.Background(), repoUniverse(t))
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(fixtureDir, venues.FixtureFileName( //nolint:gosec // fixed fixture name
		"gamma/events?active=true&closed=false&exclude_tag_id=102127&limit=50&offset=0&tag_slug=fed-rates")))
	if err != nil {
		t.Fatal(err)
	}
	events, _, err := parseEvents(body)
	if err != nil {
		t.Fatal(err)
	}
	var want binaryMarket
	var wantEvent gammaEvent
	for _, ev := range events {
		for _, raw := range ev.Markets {
			m, err := parseMarket(raw)
			if err == nil && !m.Closed && ev.NegRisk && want.YesToken == "" {
				want, wantEvent = m, ev
			}
		}
	}
	if want.YesToken == "" {
		t.Fatal("no open negRisk binary market in the fed-rates fixture")
	}
	var got *canonical.Instrument
	var siblings int
	for i := range ins {
		if ins[i].ID == "polymarket:"+want.YesToken {
			got = &ins[i]
		}
		if ins[i].VenueEventID == wantEvent.Slug {
			siblings++
		}
	}
	if got == nil {
		t.Fatalf("instrument for YES token %s missing", want.YesToken)
	}
	wantClose, _ := parseGammaTime(want.EndDate)
	if got.Title != strings.TrimSpace(want.Question) || got.RulesText != strings.TrimSpace(want.Description) ||
		got.VenueMarketID != want.ConditionID || got.VenueEventID != wantEvent.Slug || !got.CloseTime.Equal(wantClose) ||
		got.Category != canonical.CategoryFed {
		t.Errorf("instrument = %+v", *got)
	}
	if siblings < 2 {
		t.Errorf("negRisk event %s has %d instruments sharing venue_event_id, want >= 2", wantEvent.Slug, siblings)
	}
}

func TestFetchQuotesFixtures(t *testing.T) {
	a := fixtureAdapter(t)
	entries, err := os.ReadDir(fixtureDir)
	if err != nil {
		t.Fatal(err)
	}
	var ins []canonical.Instrument
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "clob_book_token_id-") {
			continue
		}
		token := strings.TrimPrefix(name, "clob_book_token_id-")
		token = token[:strings.IndexByte(token, '.')]
		ins = append(ins, canonical.Instrument{ID: "polymarket:" + token})
	}
	if len(ins) < 10 {
		t.Fatalf("found %d recorded books, want at least 10 (5 per tag)", len(ins))
	}
	// One instrument with no recorded book: the server answers 404.
	ins = append(ins, canonical.Instrument{ID: "polymarket:123"})
	quotes, raws, err := a.FetchQuotes(context.Background(), ins)
	if err != nil {
		t.Fatalf("FetchQuotes: %v", err)
	}
	if len(quotes) != len(ins) || len(raws) != len(ins) {
		t.Fatalf("got %d quotes and %d raws for %d instruments", len(quotes), len(raws), len(ins))
	}
	withBook := 0
	for i, q := range quotes {
		if q.InstrumentID != ins[i].ID {
			t.Errorf("quote %d is for %s, want input order (%s)", i, q.InstrumentID, ins[i].ID)
		}
		if err := q.Validate(); err != nil {
			t.Errorf("invalid quote: %v", err)
		}
		if !q.FetchedAt.Equal(recordedAt) {
			t.Errorf("%s: fetched_at %v", q.InstrumentID, q.FetchedAt)
		}
		if q.Quality.Has(canonical.QualityNoBook) {
			continue
		}
		withBook++
		if q.VenueTS == nil || q.VenueTS.Location() != time.UTC || q.VenueTS.Year() != 2026 {
			t.Errorf("%s: venue_ts %v", q.InstrumentID, q.VenueTS)
		}
	}
	if withBook < 10 {
		t.Errorf("%d quotes have a book, want >= 10", withBook)
	}
	last := quotes[len(quotes)-1]
	if !last.Quality.Has(canonical.QualityNoBook) || len(last.YesBids)+len(last.YesAsks) != 0 {
		t.Errorf("missing book: quote %+v, want empty and flagged no_book", last)
	}
	if raws[len(raws)-1].HTTPStatus != http.StatusNotFound {
		t.Errorf("404 raw response not kept: %+v", raws[len(raws)-1].HTTPStatus)
	}
}

func TestFetchSettledFixtures(t *testing.T) {
	a := fixtureAdapter(t)
	// The ethereum settled page was not committed (repo size, D-057). No Fed
	// market closed in the day before recording, so fed-rates looks back 30 days;
	// both windows end within the single recorded page.
	cases := []struct {
		tag   string
		cat   canonical.Category
		since time.Time
	}{
		{"bitcoin", canonical.CategoryCrypto, recordedAt.AddDate(0, 0, -1)},
		{"fed-rates", canonical.CategoryFed, recordedAt.AddDate(0, 0, -30)},
	}
	for _, tc := range cases {
		t.Run(tc.tag, func(t *testing.T) {
			u := venues.Universe{Venue: canonical.VenuePolymarket, Categories: map[canonical.Category]venues.Selector{
				tc.cat: {TagSlugs: []string{tc.tag}},
			}}
			settled, raws, err := a.FetchSettled(context.Background(), u, tc.since)
			if err != nil {
				t.Fatalf("FetchSettled: %v", err)
			}
			outcomes := map[canonical.Outcome]int{}
			for _, s := range settled {
				if err := s.Validate(); err != nil {
					t.Errorf("invalid settled: %v", err)
				}
				if s.Instrument.Status != canonical.StatusSettled || s.Instrument.Category != tc.cat {
					t.Errorf("%s: status %q category %q", s.Instrument.ID, s.Instrument.Status, s.Instrument.Category)
				}
				if !s.SettledAt.IsZero() && s.SettledAt.Before(tc.since) {
					t.Errorf("%s settled at %v, before since %v", s.Instrument.ID, s.SettledAt, tc.since)
				}
				outcomes[s.Outcome]++
			}
			if outcomes[canonical.OutcomeYes] == 0 || outcomes[canonical.OutcomeNo] == 0 {
				t.Errorf("outcomes = %v, want both yes and no", outcomes)
			}
			if len(raws) != 1 || !strings.Contains(raws[0].Endpoint, "closed=true") || !strings.Contains(raws[0].Endpoint, "order=endDate") {
				t.Errorf("settled raws = %d, want the one recorded page", len(raws))
			}
		})
	}
}

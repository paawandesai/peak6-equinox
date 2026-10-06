package collector

import (
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

func TestSelectForQuotesInterleavesAndOrdersByClose(t *testing.T) {
	ins := []canonical.Instrument{
		instrument(canonical.VenueKalshi, "F-LATE", canonical.CategoryFed, t0.Add(72*time.Hour)),
		instrument(canonical.VenueKalshi, "C-B", canonical.CategoryCrypto, t0.Add(time.Hour)),
		instrument(canonical.VenueKalshi, "C-A", canonical.CategoryCrypto, t0.Add(time.Hour)),
		instrument(canonical.VenueKalshi, "C-NOCLOSE", canonical.CategoryCrypto, time.Time{}),
		instrument(canonical.VenueKalshi, "F-SOON", canonical.CategoryFed, t0.Add(24*time.Hour)),
		instrument(canonical.VenueKalshi, "C-SOONEST", canonical.CategoryCrypto, t0),
		instrument(canonical.VenueKalshi, "C-PAST-OLD", canonical.CategoryCrypto, t0.Add(-48*time.Hour)),
		instrument(canonical.VenueKalshi, "C-PAST-NEW", canonical.CategoryCrypto, t0.Add(-time.Hour)),
	}
	got := SelectForQuotes(ins, 4, t0)
	want := []string{"C-SOONEST", "F-SOON", "C-A", "F-LATE"}
	if len(got) != len(want) {
		t.Fatalf("got %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].VenueMarketID != w {
			t.Errorf("got[%d] = %s, want %s", i, got[i].VenueMarketID, w)
		}
	}
	all := SelectForQuotes(ins, 0, t0)
	gotIDs := make([]string, 0, len(all))
	for _, in := range all {
		gotIDs = append(gotIDs, in.VenueMarketID)
	}
	wantAll := "C-SOONEST F-SOON C-A F-LATE C-B C-PAST-NEW C-PAST-OLD C-NOCLOSE"
	if got := strings.Join(gotIDs, " "); got != wantAll {
		t.Errorf("no cap order:\n%s\nwant\n%s", got, wantAll)
	}
	odd := []canonical.Instrument{
		instrument(canonical.VenueKalshi, "X", "", t0),
		instrument(canonical.VenueKalshi, "Y", canonical.CategoryFed, t0),
	}
	if got := SelectForQuotes(odd, 0, t0); len(got) != 2 || got[0].VenueMarketID != "Y" || got[1].VenueMarketID != "X" {
		t.Errorf("unknown category should sort last, got %+v", got)
	}
	if len(SelectForQuotes(nil, 5, t0)) != 0 {
		t.Error("nil input")
	}
}

func TestSelectForQuotesProperties(t *testing.T) {
	cats := canonical.Categories()
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(0, 60).Draw(rt, "n")
		ins := make([]canonical.Instrument, 0, n)
		ids := map[string]bool{}
		for i := 0; i < n; i++ {
			key := rapid.StringMatching(`[A-Z]{1,3}`).Draw(rt, "key")
			if ids[key] {
				continue
			}
			ids[key] = true
			var closeAt time.Time
			if rapid.Bool().Draw(rt, "hasClose") {
				closeAt = t0.Add(time.Duration(rapid.IntRange(-48, 48).Draw(rt, "hours")) * time.Hour)
			}
			ins = append(ins, instrument(canonical.VenueKalshi, key, cats[rapid.IntRange(0, len(cats)-1).Draw(rt, "cat")], closeAt))
		}
		limit := rapid.IntRange(-1, 70).Draw(rt, "limit")
		got := SelectForQuotes(ins, limit, t0)

		wantLen := len(ins)
		if limit > 0 && limit < wantLen {
			wantLen = limit
		}
		if len(got) != wantLen {
			rt.Fatalf("len = %d, want %d", len(got), wantLen)
		}
		chosen := map[string]bool{}
		for _, g := range got {
			if chosen[g.ID] {
				rt.Fatalf("duplicate %s", g.ID)
			}
			chosen[g.ID] = true
		}
		total := map[canonical.Category]int{}
		picked := map[canonical.Category]int{}
		for _, in := range ins {
			total[in.Category]++
			if chosen[in.ID] {
				picked[in.Category]++
			}
		}
		// Within a category, nothing unchosen closes before something chosen.
		for _, a := range ins {
			if chosen[a.ID] {
				continue
			}
			for _, b := range ins {
				if chosen[b.ID] && a.Category == b.Category && quoteBefore(a, b, t0) {
					rt.Fatalf("%s (close %v) left out while lower-priority %s (close %v) was chosen", a.ID, a.CloseTime, b.ID, b.CloseTime)
				}
			}
		}
		// Categories are balanced: a category with instruments left over has
		// at least as many picks as any other category, give or take one.
		for c1, p1 := range picked {
			if p1 == total[c1] {
				continue // exhausted
			}
			for c2, p2 := range picked {
				if p2 > p1+1 {
					rt.Fatalf("category %s got %d picks while %s got %d with %d left", c2, p2, c1, p1, total[c1]-p1)
				}
			}
		}
	})
}

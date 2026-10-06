package canonical

import (
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"pgregory.net/rapid"
)

var (
	t0  = time.Date(2026, 10, 6, 17, 0, 0, 0, time.UTC)
	cdt = time.FixedZone("CDT", -5*3600)
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func lvl(p, s string) Level { return Level{Price: d(p), Size: d(s)} }

// genDecimal draws arbitrary decimals: any int64 mantissa with an exponent
// from -20 to 5, so values range from tiny fractions to huge magnitudes.
func genDecimal() *rapid.Generator[decimal.Decimal] {
	return rapid.Custom(func(t *rapid.T) decimal.Decimal {
		return decimal.New(rapid.Int64().Draw(t, "mantissa"), rapid.Int32Range(-20, 5).Draw(t, "exp"))
	})
}

func TestLevelValidateProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		l := Level{Price: genDecimal().Draw(t, "price"), Size: genDecimal().Draw(t, "size")}
		inRange := !l.Price.IsNegative() && l.Price.LessThanOrEqual(one)
		want := inRange && l.Size.IsPositive()
		if got := l.Validate() == nil; got != want {
			t.Fatalf("Level{%s, %s}.Validate() ok=%v, want %v", l.Price, l.Size, got, want)
		}
	})
}

func TestValidatePriceProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		p := genDecimal().Draw(t, "price")
		want := p.GreaterThanOrEqual(zero) && p.LessThanOrEqual(one)
		if got := ValidatePrice(p) == nil; got != want {
			t.Fatalf("ValidatePrice(%s) ok=%v, want %v", p, got, want)
		}
	})
}

func TestValidatePriceBoundaries(t *testing.T) {
	for _, p := range []string{"0", "1", "0.5", "0.001", "0.999", "1.000"} {
		if err := ValidatePrice(d(p)); err != nil {
			t.Errorf("ValidatePrice(%s) = %v", p, err)
		}
	}
	for _, p := range []string{"-0.0000001", "1.0000001", "1.5", "-1", "100"} {
		if err := ValidatePrice(d(p)); err == nil {
			t.Errorf("ValidatePrice(%s) accepted", p)
		}
	}
}

func TestLevelRejectsNonPositiveSize(t *testing.T) {
	for _, s := range []string{"0", "-1", "-0.0001"} {
		if err := lvl("0.5", s).Validate(); err == nil || !strings.Contains(err.Error(), "size") {
			t.Errorf("size %s: err = %v", s, err)
		}
	}
}

func validQuote() Quote {
	return Quote{
		InstrumentID: "kalshi:KXBTCD-26OCT0617-T120000",
		YesBids:      []Level{lvl("0.45", "10"), lvl("0.44", "5")},
		YesAsks:      []Level{lvl("0.47", "3"), lvl("0.50", "100")},
		FetchedAt:    t0,
	}
}

func TestQuoteValidate(t *testing.T) {
	ts := t0.Add(-time.Second)
	localTS := ts.In(cdt)
	cases := []struct {
		name    string
		mutate  func(q *Quote)
		wantErr string
	}{
		{"valid", func(*Quote) {}, ""},
		{"valid with venue ts", func(q *Quote) { q.VenueTS = &ts }, ""},
		{"empty books", func(q *Quote) { q.YesBids, q.YesAsks = nil, nil }, ""},
		{"bad id", func(q *Quote) { q.InstrumentID = "'; DROP TABLE quotes; --" }, "instrument id"},
		{"missing fetched_at", func(q *Quote) { q.FetchedAt = time.Time{} }, "fetched_at is required"},
		{"non-utc fetched_at", func(q *Quote) { q.FetchedAt = t0.In(cdt) }, "fetched_at must be UTC"},
		{"non-utc venue ts", func(q *Quote) { q.VenueTS = &localTS }, "venue_ts must be UTC"},
		{"bid price > 1", func(q *Quote) { q.YesBids[0] = lvl("1.5", "1") }, "yes_bids[0]: price 1.5 is outside [0,1]"},
		{"ask negative size", func(q *Quote) { q.YesAsks[1] = lvl("0.6", "-2") }, "yes_asks[1]: size -2"},
		{"bids unsorted", func(q *Quote) { q.YesBids = []Level{lvl("0.40", "1"), lvl("0.44", "1")} }, "yes_bids[1]"},
		{"asks unsorted", func(q *Quote) { q.YesAsks = []Level{lvl("0.50", "1"), lvl("0.47", "1")} }, "yes_asks[1]"},
		{"duplicate level price", func(q *Quote) { q.YesAsks = []Level{lvl("0.50", "1"), lvl("0.50", "1")} }, "yes_asks[1]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := validQuote()
			tc.mutate(&q)
			err := q.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

// TestQuoteRejectsNonUTCProperty: any non-UTC location is rejected, even a
// zero-offset zone that is not time.UTC itself.
func TestQuoteRejectsNonUTCProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		offset := rapid.IntRange(-14*3600, 14*3600).Draw(t, "offset")
		q := validQuote()
		q.FetchedAt = t0.In(time.FixedZone("X", offset))
		if q.Validate() == nil {
			t.Fatalf("accepted fetched_at in zone offset %d", offset)
		}
	})
}

// TestQuoteLevelsProperty: a quote with one arbitrary level per side is valid
// exactly when both levels are.
func TestQuoteLevelsProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		bid := Level{Price: genDecimal().Draw(t, "bid"), Size: genDecimal().Draw(t, "bidSize")}
		ask := Level{Price: genDecimal().Draw(t, "ask"), Size: genDecimal().Draw(t, "askSize")}
		q := Quote{InstrumentID: "polymarket:123", YesBids: []Level{bid}, YesAsks: []Level{ask}, FetchedAt: t0}
		want := bid.Validate() == nil && ask.Validate() == nil
		if got := q.Validate() == nil; got != want {
			t.Fatalf("Validate ok=%v, want %v (bid %v ask %v)", got, want, bid, ask)
		}
	})
}

func TestQuoteTouchAndMid(t *testing.T) {
	q := validQuote()
	if bid, ok := q.BestBid(); !ok || !bid.Equal(d("0.45")) {
		t.Errorf("BestBid = %s %v", bid, ok)
	}
	if ask, ok := q.BestAsk(); !ok || !ask.Equal(d("0.47")) {
		t.Errorf("BestAsk = %s %v", ask, ok)
	}
	if mid, ok := q.Mid(); !ok || !mid.Equal(d("0.46")) {
		t.Errorf("Mid = %s %v", mid, ok)
	}
	q.YesAsks = nil
	if _, ok := q.BestAsk(); ok {
		t.Error("BestAsk ok on empty side")
	}
	if _, ok := q.Mid(); ok {
		t.Error("Mid ok with one side empty")
	}
	q.YesBids = nil
	if _, ok := q.BestBid(); ok {
		t.Error("BestBid ok on empty side")
	}
}

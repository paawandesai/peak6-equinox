package kalshi

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	"pgregory.net/rapid"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

func TestKalshiParseDollars(t *testing.T) {
	for _, ok := range []string{"0", "1", "0.5", "0.9900", "1.0000", "0.0001", "0.00000001"} {
		if _, err := parseDollars(ok); err != nil {
			t.Errorf("parseDollars(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", "1.5", "1.0001", "-0.1", "NaN", "Inf", "1e-2", "0x1", " 0.5", "0.5 ", ".5", "5.", "0.123456789", "abc", "1,5", "+0.5"} {
		if d, err := parseDollars(bad); err == nil {
			t.Errorf("parseDollars(%q) = %v, want error", bad, d)
		}
	}
}

func TestKalshiParseFP(t *testing.T) {
	for _, ok := range []string{"0", "0.00", "7246.15", "777958.00", "999999999999999.99999999"} {
		if _, err := parseFP(ok); err != nil {
			t.Errorf("parseFP(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", "-1", "-0.01", "NaN", "1e9", "1e1000000000", "1234567890123456", "1.", "x", "١٢"} {
		if d, err := parseFP(bad); err == nil {
			t.Errorf("parseFP(%q) = %v, want error", bad, d)
		}
	}
}

// TestKalshiPropDollarParser: every price the parser accepts is in [0,1] and
// round-trips; every formatted value in [0,1] is accepted unchanged.
func TestKalshiPropDollarParser(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := rapid.OneOf(
			rapid.StringMatching(`-?[0-9]{0,8}(\.[0-9]{0,10})?`),
			rapid.String(),
		).Draw(t, "s")
		d, err := parseDollars(s)
		if err != nil {
			return
		}
		if d.IsNegative() || d.GreaterThan(decimal.NewFromInt(1)) {
			t.Fatalf("parseDollars(%q) = %s outside [0,1]", s, d)
		}
		if !d.Equal(decimal.RequireFromString(s)) {
			t.Fatalf("parseDollars(%q) = %s does not round-trip", s, d)
		}
	})
	rapid.Check(t, func(t *rapid.T) {
		cents := rapid.Int64Range(0, 10000).Draw(t, "ten-thousandths")
		want := decimal.New(cents, -4)
		got, err := parseDollars(want.StringFixed(4))
		if err != nil || !got.Equal(want) {
			t.Fatalf("parseDollars(%s) = %s, %v", want.StringFixed(4), got, err)
		}
	})
}

// TestKalshiPropFPParser: accepted sizes are non-negative and round-trip;
// negative or exponent forms are always rejected.
func TestKalshiPropFPParser(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := rapid.OneOf(
			rapid.StringMatching(`[-+]?[0-9]{0,17}(\.[0-9]{0,10})?([eE][-+]?[0-9]{1,3})?`),
			rapid.String(),
		).Draw(t, "s")
		d, err := parseFP(s)
		if err != nil {
			return
		}
		if d.IsNegative() || strings.ContainsAny(s, "-+eE") {
			t.Fatalf("parseFP(%q) = %s accepted", s, d)
		}
		if !d.Equal(decimal.RequireFromString(s)) {
			t.Fatalf("parseFP(%q) = %s does not round-trip", s, d)
		}
	})
	rapid.Check(t, func(t *rapid.T) {
		hundredths := rapid.Int64Range(0, 1<<40).Draw(t, "hundredths")
		want := decimal.New(hundredths, -2)
		got, err := parseFP(want.StringFixed(2))
		if err != nil || !got.Equal(want) {
			t.Fatalf("parseFP(%s) = %s, %v", want.StringFixed(2), got, err)
		}
	})
}

// TestKalshiPropOrderbook: for any book of well-formed levels, the derived
// quote validates, YES bids are the YES levels, and each YES ask is 1 − a NO
// bid with the same total size.
func TestKalshiPropOrderbook(t *testing.T) {
	level := func(t *rapid.T, label string) [2]string {
		p := rapid.Int64Range(0, 100).Draw(t, label+"-price")
		s := rapid.Int64Range(1, 1_000_000).Draw(t, label+"-size")
		return [2]string{decimal.New(p, -2).StringFixed(4), decimal.New(s, -2).StringFixed(2)}
	}
	rapid.Check(t, func(t *rapid.T) {
		yes := rapid.SliceOfN(rapid.Custom(func(t *rapid.T) [2]string { return level(t, "yes") }), 0, 30).Draw(t, "yes")
		no := rapid.SliceOfN(rapid.Custom(func(t *rapid.T) [2]string { return level(t, "no") }), 0, 30).Draw(t, "no")
		body := fmt.Sprintf(`{"orderbook_fp":{"yes_dollars":%s,"no_dollars":%s}}`, levelsJSON(yes), levelsJSON(no))
		b, err := parseOrderbook([]byte(body))
		if err != nil || !b.quality.OK() {
			t.Fatalf("parseOrderbook: %v %+v", err, b.quality)
		}
		q := canonical.Quote{InstrumentID: "kalshi:X", FetchedAt: fixedNow, YesBids: b.yesBids, YesAsks: yesAsks(b.noBids)}
		if err := q.Validate(); err != nil {
			t.Fatalf("derived quote invalid: %v", err)
		}
		if !sumSizes(q.YesBids).Equal(sumRaw(yes)) || !sumSizes(q.YesAsks).Equal(sumRaw(no)) {
			t.Fatal("sizes not carried over")
		}
		for k, a := range q.YesAsks {
			if !a.Price.Add(b.noBids[k].Price).Equal(decimal.NewFromInt(1)) {
				t.Fatalf("ask %s + no bid %s != 1", a.Price, b.noBids[k].Price)
			}
		}
	})
}

func levelsJSON(ls [][2]string) string {
	parts := make([]string, len(ls))
	for k, l := range ls {
		parts[k] = fmt.Sprintf(`[%q,%q]`, l[0], l[1])
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func sumSizes(ls []canonical.Level) decimal.Decimal {
	s := decimal.Zero
	for _, l := range ls {
		s = s.Add(l.Size)
	}
	return s
}

func sumRaw(ls [][2]string) decimal.Decimal {
	s := decimal.Zero
	for _, l := range ls {
		s = s.Add(decimal.RequireFromString(l[1]))
	}
	return s
}

func TestKalshiParseOrderbookMalformed(t *testing.T) {
	body := `{"orderbook_fp":{"yes_dollars":[["0.40","10.00"],["1.5","5.00"],["NaN","1.00"],["0.30","-2.00"],["0.30","0.00"],
		[0.2,"1.00"],["0.20"],"junk",["0.40","2.50"]],"no_dollars":{"not":"an array"}}}`
	b, err := parseOrderbook([]byte(body))
	if err != nil {
		t.Fatalf("parseOrderbook: %v", err)
	}
	if len(b.yesBids) != 1 || !b.yesBids[0].Price.Equal(decimal.RequireFromString("0.4")) || !b.yesBids[0].Size.Equal(decimal.RequireFromString("12.5")) {
		t.Errorf("yes bids = %+v, want one merged level 0.40 x 12.50", b.yesBids)
	}
	if len(b.noBids) != 0 {
		t.Errorf("no bids = %+v", b.noBids)
	}
	for _, f := range []canonical.QualityFlag{canonical.QualityInvalidPrice, canonical.QualityInvalidSize, canonical.QualityRecordSkipped} {
		if !b.quality.Has(f) {
			t.Errorf("quality lacks %s: %+v", f, b.quality)
		}
	}
	for _, bad := range []string{`not json`, `{}`, `{"orderbook_fp":null}`, `[]`} {
		if _, err := parseOrderbook([]byte(bad)); err == nil {
			t.Errorf("parseOrderbook(%s) accepted", bad)
		}
	}
	if b, err := parseOrderbook([]byte(`{"orderbook_fp":{"yes_dollars":null}}`)); err != nil || len(b.yesBids) != 0 || len(b.noBids) != 0 {
		t.Errorf("empty book = %+v, %v", b, err)
	}
}

func TestKalshiToInstrument(t *testing.T) {
	good := market{
		Ticker: "KXFED-26OCT-T3.25", EventTicker: "KXFED-26OCT", Title: "Fed above 3.25%?", YesSubTitle: "Above 3.25%",
		RulesPrimary: "If above, Yes.", RulesSecondary: "Additional details here.", CloseTime: "2026-10-28T12:55:00-05:00", ExpirationTime: "2026-11-04T18:05:00Z", Status: "active",
	}
	ins, err := toInstrument(good, canonical.CategoryFed)
	if err != nil {
		t.Fatal(err)
	}
	if ins.CloseTime.Location().String() != "UTC" || ins.CloseTime.Hour() != 17 {
		t.Errorf("close time %v not converted to UTC", ins.CloseTime)
	}
	if ins.RulesText != "If above, Yes.\n\nAdditional details here." || ins.Title != "Fed above 3.25%? — Above 3.25%" {
		t.Errorf("rules %q title %q", ins.RulesText, ins.Title)
	}
	// UI-21: rule layers (summary, clarifications)
	if ins.RulesSummary != "" {
		t.Errorf("RulesSummary = %q, want empty (Kalshi doesn't publish)", ins.RulesSummary)
	}
	if ins.Clarifications != "Additional details here." {
		t.Errorf("Clarifications = %q, want rules_secondary", ins.Clarifications)
	}
	if !ins.ClarificationsUpdatedAt.IsZero() {
		t.Errorf("ClarificationsUpdatedAt = %v, want zero (Kalshi doesn't publish timestamp)", ins.ClarificationsUpdatedAt)
	}
	// M3-05: pin FeeSchedule.Verified and Source (Q-8 answered 2026-10-07)
	if ins.FeeSchedule.Verified != true {
		t.Errorf("FeeSchedule.Verified = %v, want true (Q-8 answered)", ins.FeeSchedule.Verified)
	}
	if ins.FeeSchedule.Source != "https://kalshi.com/docs/kalshi-fee-schedule.pdf" {
		t.Errorf("FeeSchedule.Source = %q, want PDF URL", ins.FeeSchedule.Source)
	}

	for name, mod := range map[string]func(*market){
		"missing ticker": func(m *market) { m.Ticker = "  " },
		"bad ticker":     func(m *market) { m.Ticker = "../../admin" },
		"missing title":  func(m *market) { m.Title = "\x00" },
	} {
		m := good
		mod(&m)
		if _, err := toInstrument(m, canonical.CategoryFed); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	m := good
	m.CloseTime, m.ExpirationTime, m.RulesPrimary, m.EventTicker, m.Status = "", "yesterday", "", "", "weird"
	m.Title = strings.Repeat("é", maxTitleBytes)
	ins, err = toInstrument(m, canonical.CategoryCrypto)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []canonical.QualityFlag{canonical.QualityMissingField, canonical.QualityInvalidTime, "text_truncated"} {
		if !ins.Quality.Has(f) {
			t.Errorf("quality lacks %s: %+v", f, ins.Quality)
		}
	}
	if !ins.CloseTime.IsZero() || !ins.ExpirationTime.IsZero() || ins.Status != canonical.StatusUnknown || len(ins.Title) > maxTitleBytes+len(" — Above 3.25%") {
		t.Errorf("degraded instrument = %+v", ins)
	}

	// UI-21: test empty and oversized clarifications (AB-02)
	m2 := good
	m2.RulesSecondary = ""
	ins2, err := toInstrument(m2, canonical.CategoryFed)
	if err != nil {
		t.Fatal(err)
	}
	if ins2.Clarifications != "" {
		t.Errorf("empty rules_secondary: Clarifications = %q, want empty", ins2.Clarifications)
	}

	m3 := good
	m3.RulesSecondary = strings.Repeat("x", maxRulesBytes+1000)
	ins3, err := toInstrument(m3, canonical.CategoryFed)
	if err != nil {
		t.Fatal(err)
	}
	if len(ins3.Clarifications) > maxRulesBytes {
		t.Errorf("oversized clarifications: len = %d, want ≤ %d", len(ins3.Clarifications), maxRulesBytes)
	}
	if !ins3.Quality.Has("text_truncated") {
		t.Errorf("oversized clarifications not flagged: %+v", ins3.Quality)
	}
}

func TestKalshiMapStatusAndResult(t *testing.T) {
	for in, want := range map[string]canonical.InstrumentStatus{
		"active": canonical.StatusOpen, "closed": canonical.StatusClosed, "finalized": canonical.StatusSettled,
		"determined": canonical.StatusSettled, "initialized": canonical.StatusUnknown, "": canonical.StatusUnknown,
	} {
		if got := mapStatus(in); got != want {
			t.Errorf("mapStatus(%q) = %s, want %s", in, got, want)
		}
	}
	for in, want := range map[string]canonical.Outcome{"yes": canonical.OutcomeYes, "NO": canonical.OutcomeNo} {
		if got, ok := mapResult(in); !ok || got != want {
			t.Errorf("mapResult(%q) = %s", in, got)
		}
	}
	for _, in := range []string{"", "void", "scalar", "all_no"} {
		if _, ok := mapResult(in); ok {
			t.Errorf("mapResult(%q) accepted", in)
		}
	}
}

func TestKalshiParseMarketsSkipsBadRecords(t *testing.T) {
	body := `{"cursor":"c2","markets":[{"ticker":"A-1","title":"A","result":"yes"},{"ticker":5},"x",{"ticker":"B-1","title":"B","result":"no"}]}`
	p, err := parseMarkets([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.markets) != 2 || len(p.skipped) != 2 || p.cursor != "c2" {
		t.Errorf("page = %+v", p)
	}
	for _, bad := range []string{``, `[]`, `{"markets":{}}`, `{"markets":[1]`} {
		if _, err := parseMarkets([]byte(bad)); err == nil {
			t.Errorf("parseMarkets(%q) accepted", bad)
		}
	}
	if _, err := parseEvents([]byte(`{"events":7}`)); err == nil {
		t.Error("parseEvents accepted a non-array")
	}
	ep, err := parseEvents([]byte(`{"events":[{"event_ticker":"E","markets":[{"ticker":"E-1","title":"t"},[]]},"bad"]}`))
	if err != nil || len(ep.events) != 1 || len(ep.skipped) != 1 || len(ep.events[0].markets) != 1 || len(ep.events[0].skipped) != 1 {
		t.Errorf("events page = %+v, %v", ep, err)
	}
}

// fuzzSeeds adds the recorded fixtures whose names start with prefix.
func fuzzSeeds(f *testing.F, prefix string) {
	entries, err := os.ReadDir(fixtureDir)
	if err != nil {
		f.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			b, err := os.ReadFile(filepath.Join(fixtureDir, e.Name()))
			if err != nil {
				f.Fatal(err)
			}
			if len(b) < 64<<10 {
				f.Add(b)
			}
		}
	}
	f.Add([]byte(`{"markets":[{"ticker":"KXFED-1","title":"t","close_time":"2026-01-01T00:00:00Z","result":"yes","mve_collection_ticker":""}],"cursor":""}`))
	f.Add([]byte(`{"markets":[{"ticker":"","title":"t"},{"ticker":"A","title":"\u0000","close_time":"9999-99-99"}]}`))
	f.Add([]byte(`{"events":[{"event_ticker":"E","markets":[{"ticker":"E-1","title":"t","mve_collection_ticker":"M"}]}]}`))
	f.Add([]byte(`{"orderbook_fp":{"yes_dollars":[["0.5","1.00"],["1.5","1"],["NaN","-1"]],"no_dollars":[["0.4","2"]]}}`))
	f.Add([]byte(strings.Repeat("[", 20000)))
}

// FuzzParseMarkets: parseMarkets and toInstrument never panic, and every
// instrument or settled record they produce validates.
func FuzzParseMarkets(f *testing.F) {
	fuzzSeeds(f, "markets_series_ticker-KXFED")
	f.Fuzz(func(t *testing.T, body []byte) {
		p, err := parseMarkets(body)
		if err != nil {
			return
		}
		settled, _ := settledFrom(p.markets, canonical.CategoryFed)
		for _, s := range settled {
			if err := s.Validate(); err != nil {
				t.Fatalf("invalid settled record: %v", err)
			}
		}
		for _, m := range p.markets {
			if ins, err := toInstrument(m, canonical.CategoryFed); err == nil {
				if err := ins.Validate(); err != nil {
					t.Fatalf("invalid instrument: %v", err)
				}
			}
		}
	})
}

// FuzzParseEvents: parseEvents never panics and its instruments validate.
func FuzzParseEvents(f *testing.F) {
	fuzzSeeds(f, "events_")
	f.Fuzz(func(t *testing.T, body []byte) {
		p, err := parseEvents(body)
		if err != nil {
			return
		}
		a := newTestAdapter(t, "http://127.0.0.1:1", nil)
		for _, ev := range p.events {
			for _, ins := range a.eventInstruments("/events", ev, canonical.CategoryCrypto) {
				if err := ins.Validate(); err != nil {
					t.Fatalf("invalid instrument: %v", err)
				}
			}
		}
	})
}

// FuzzParseOrderbook: parseOrderbook never panics and its derived quote validates.
func FuzzParseOrderbook(f *testing.F) {
	fuzzSeeds(f, "markets_KX")
	f.Fuzz(func(t *testing.T, body []byte) {
		b, err := parseOrderbook(body)
		if err != nil {
			return
		}
		q := canonical.Quote{InstrumentID: "kalshi:X", FetchedAt: fixedNow, YesBids: b.yesBids, YesAsks: yesAsks(b.noBids)}
		if err := q.Validate(); err != nil {
			t.Fatalf("derived quote invalid: %v", err)
		}
	})
}

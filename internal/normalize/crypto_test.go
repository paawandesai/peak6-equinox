package normalize

import (
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// Real venue text recorded on 2026-10-06 (testdata/fixtures) and M1-05's
// Polymarket fixtures. Rules are the primary and secondary Kalshi rules joined
// the way the Kalshi adapter joins them.
const (
	kalshiBTCRules = "If the simple average of the sixty seconds of CF Benchmarks' Bitcoin Real-Time Index (BRTI) before 6 PM EDT " +
		"is above 76599.99 at 6 PM EDT on Oct 6, 2026, then the market resolves to Yes.\n\n" +
		"Not all cryptocurrency price data is the same. While checking a source like Google or Coinbase may help guide your " +
		"decision, the price used to determine this market is based on CF Benchmarks' corresponding Real Time Index (RTI). " +
		"At the last minute before expiration, 60 RTI prices are collected. The official and final value is the average of these prices."
	kalshiETHRules = "If the simple average of the sixty seconds of CF Benchmarks' Ethereum Real-Time Index (ERTI) before 5 PM EDT " +
		"is above 1899.99 at 5 PM EDT on Oct 7, 2026, then the market resolves to Yes."
	polyAboveRules = `This market will resolve to "Yes" if the Binance 1 minute candle for BTC/USDT closing at 12:00 in the ET timezone (noon) ` +
		`on the date specified in the title has a final "Close" price higher than the price specified in the title. Otherwise, this market will resolve to "No".` +
		"\n\nThe resolution source for this market is Binance, specifically the BTC/USDT \"Close\" prices currently available at " +
		"https://www.binance.com/en/trade/BTC_USDT with \"1m\" and \"Candles\" selected on the top bar.\n\n" +
		"Please note that this market is about the price according to Binance BTC/USDT, not according to other exchanges or trading pairs."
	polyBucketRules = `This market will resolve according to the final "Close" price of the Binance 1 minute candle for BTC/USDT closing at 12:00 ` +
		`in the ET timezone (noon) on the date specified in the title. Otherwise, this market will resolve to "No".` +
		"\n\nThe resolution source for this market is Binance, specifically the BTC/USDT \"Close\" prices currently available at " +
		"https://www.binance.com/en/trade/BTC_USDT with \"1m\" and \"Candles\" selected on the top bar.\n\n" +
		"If the reported value falls exactly between two brackets, then this market will resolve to the higher range bracket."
	polyHitRules = `This market will immediately resolve to "Yes" if any Binance 1 minute candle for Bitcoin (BTC/USDT) has a final "High" price ` +
		`equal to or greater than the price specified in the title by 11:59PM ET on the date specified in the title. Otherwise, this market will resolve to "No."` +
		"\n\nThe resolution source for this market is Binance, specifically the BTC/USDT \"High\" prices available at https://www.binance.com/en/trade/BTC_USDT."
	polyDipRules = `This market will resolve to "Yes" if any Binance 1 minute candle for BTC/USDT from the creation of this market through 11:59 PM ET ` +
		`on the last day of the year specified in the title has a final Low price equal to or lower than the price specified in the title. ` +
		`Otherwise, this market will resolve to "No". The resolution source for this market is Binance, specifically the BTC/USDT Low prices.`
)

var (
	closeOct6  = time.Date(2026, 10, 6, 22, 0, 0, 0, time.UTC)
	closeOct7  = time.Date(2026, 10, 7, 21, 0, 0, 0, time.UTC)
	closeNoon7 = time.Date(2026, 10, 7, 16, 0, 0, 0, time.UTC)
)

func crypto(title, rules string, closeTime time.Time) canonical.Instrument {
	return canonical.Instrument{
		ID: "kalshi:T", Venue: canonical.VenueKalshi, VenueMarketID: "T", Category: canonical.CategoryCrypto,
		Title: title, RulesText: rules, Terms: canonical.UnknownTerms(), CloseTime: closeTime, Status: canonical.StatusOpen,
		FeeSchedule: canonical.FeeSchedule{ID: canonical.FeeScheduleKalshiTakerV1},
	}
}

func dec(s string) *decimal.Decimal {
	d := decimal.RequireFromString(s)
	return &d
}

func utc(y int, m time.Month, d, h, minute int) *time.Time {
	t := time.Date(y, m, d, h, minute, 0, 0, time.UTC)
	return &t
}

type want struct {
	subject, metric string
	comparator      canonical.Comparator
	threshold, high *decimal.Decimal
	unit            string
	deadline        *time.Time
	tz, source      string
	polarity        canonical.Polarity
	flags           []canonical.QualityFlag
}

func TestExtractCryptoRealVenueText(t *testing.T) {
	cases := []struct {
		name string
		in   canonical.Instrument
		want want
	}{
		{"kalshi btc ladder", crypto("Bitcoin price on Oct 6, 2026? — $76,600 or above", kalshiBTCRules, closeOct6), want{
			"BTC", MetricPrice, canonical.ComparatorGT, dec("76599.99"), nil, UnitUSD,
			utc(2026, 10, 6, 22, 0), "America/New_York", "cf_benchmarks_brti", canonical.PolarityPositive, nil}},
		{"kalshi eth ladder", crypto("Ethereum price at Oct 7, 2026 at 5pm EDT? — $1,900 or above", kalshiETHRules, closeOct7), want{
			"ETH", MetricPrice, canonical.ComparatorGT, dec("1899.99"), nil, UnitUSD,
			utc(2026, 10, 7, 21, 0), "America/New_York", "cf_benchmarks_erti", canonical.PolarityPositive, nil}},
		{"polymarket above, year from close", crypto("Will the price of Bitcoin be above $74,000 on October 7?", polyAboveRules, closeNoon7), want{
			"BTC", MetricPrice, canonical.ComparatorGT, dec("74000"), nil, UnitUSD,
			utc(2026, 10, 7, 16, 0), "America/New_York", "binance_btc_usdt", canonical.PolarityPositive,
			[]canonical.QualityFlag{QualityDeadlineYearInferred}}},
		{"polymarket between bucket", crypto("Will the price of Bitcoin be between $76,000 and $78,000 on October 7?", polyBucketRules, closeNoon7), want{
			"BTC", MetricPrice, canonical.ComparatorBetween, dec("76000"), dec("78000"), UnitUSD,
			utc(2026, 10, 7, 16, 0), "America/New_York", "binance_btc_usdt", canonical.PolarityPositive,
			[]canonical.QualityFlag{QualityDeadlineYearInferred}}},
		{"polymarket less than bucket", crypto("Will the price of Bitcoin be less than $76,000 on October 7?", polyBucketRules, closeNoon7), want{
			"BTC", MetricPrice, canonical.ComparatorLT, dec("76000"), nil, UnitUSD,
			utc(2026, 10, 7, 16, 0), "America/New_York", "binance_btc_usdt", canonical.PolarityPositive,
			[]canonical.QualityFlag{QualityDeadlineYearInferred}}},
		{"polymarket greater than bucket", crypto("Will the price of Bitcoin be greater than $94,000 on October 7?", polyBucketRules, closeNoon7), want{
			"BTC", MetricPrice, canonical.ComparatorGT, dec("94000"), nil, UnitUSD,
			utc(2026, 10, 7, 16, 0), "America/New_York", "binance_btc_usdt", canonical.PolarityPositive,
			[]canonical.QualityFlag{QualityDeadlineYearInferred}}},
		{"polymarket hit with k and no year", crypto("Will Bitcoin hit $150k by September 30?", polyHitRules, time.Date(2025, 10, 1, 4, 0, 0, 0, time.UTC)), want{
			"BTC", MetricPriceHigh, canonical.ComparatorGTE, dec("150000"), nil, UnitUSD,
			utc(2025, 10, 1, 3, 59), "America/New_York", "binance_btc_usdt", canonical.PolarityPositive,
			[]canonical.QualityFlag{QualityDeadlineYearInferred}}},
		{"polymarket dip", crypto("Will Bitcoin dip to $60,000 by December 31, 2026?", polyDipRules, time.Date(2027, 1, 1, 5, 0, 0, 0, time.UTC)), want{
			"BTC", MetricPriceLow, canonical.ComparatorLTE, dec("60000"), nil, UnitUSD,
			utc(2027, 1, 1, 4, 59), "America/New_York", "binance_btc_usdt", canonical.PolarityPositive, nil}},
		{"dominance is not a price", crypto("Will Bitcoin dominance be greater than 60% on October 7?", "Resolves per TradingView BTC.D.", closeNoon7), want{
			"BTC", canonical.Unknown, canonical.ComparatorGT, dec("60"), nil, "percent",
			&closeNoon7, canonical.Unknown, canonical.Unknown, canonical.PolarityPositive,
			[]canonical.QualityFlag{canonical.QualityDeadlineDefaulted}}},
		{"two assets is unknown", crypto("Will ETH/BTC reach 0.05 by October 7?", "Binance ETH/BTC", closeNoon7), want{
			canonical.Unknown, canonical.Unknown, canonical.ComparatorUnknown, nil, nil, canonical.Unknown,
			&closeNoon7, canonical.Unknown, "binance", canonical.PolarityUnknown,
			[]canonical.QualityFlag{canonical.QualityDeadlineDefaulted}}},
		{"reserve market: metric unknown, far date rejected", crypto("US national Bitcoin reserve before 2027?",
			"Resolves Yes if the US government holds Bitcoin at any point by December 31, 2026, 11:59 PM ET.", closeNoon7), want{
			"BTC", canonical.Unknown, canonical.ComparatorUnknown, nil, nil, canonical.Unknown,
			&closeNoon7, canonical.Unknown, canonical.Unknown, canonical.PolarityUnknown,
			[]canonical.QualityFlag{canonical.QualityDeadlineDefaulted}}},
		{"negated proposition", crypto("Bitcoin price on Oct 6, 2026? — not above $76,600",
			"If the CF Benchmarks BRTI is not above 76599.99 at 6 PM EDT on Oct 6, 2026, the market resolves to Yes.", closeOct6), want{
			"BTC", MetricPrice, canonical.ComparatorGT, dec("76599.99"), nil, UnitUSD,
			utc(2026, 10, 6, 22, 0), "America/New_York", "cf_benchmarks_brti", canonical.PolarityNegative, nil}},
		{"title-only inclusive comparator", crypto("Bitcoin price on Oct 6, 2026 at 6 PM EDT? — $76,600 or above", "", closeOct6), want{
			"BTC", MetricPrice, canonical.ComparatorGTE, dec("76600"), nil, UnitUSD,
			utc(2026, 10, 6, 22, 0), "America/New_York", canonical.Unknown, canonical.PolarityPositive, nil}},
		{"rules state two conditions: ambiguous", crypto("Bitcoin price on Oct 6, 2026? — $76,600 or above",
			"Resolves Yes if BRTI is above 76599.99 at 6 PM EDT on Oct 6, 2026, or if it is above 80000 at that time.", closeOct6), want{
			"BTC", MetricPrice, canonical.ComparatorUnknown, nil, nil, UnitUSD,
			utc(2026, 10, 6, 22, 0), "America/New_York", canonical.Unknown, canonical.PolarityUnknown,
			[]canonical.QualityFlag{QualityTermsAmbiguous}}},
		{"title with several $ amounts when rules defer to it", crypto("Will Bitcoin be above $74,000 or $75,000 on October 7?",
			polyAboveRules, closeNoon7), want{
			"BTC", MetricPrice, canonical.ComparatorUnknown, nil, nil, UnitUSD,
			utc(2026, 10, 7, 16, 0), "America/New_York", "binance_btc_usdt", canonical.PolarityUnknown,
			[]canonical.QualityFlag{QualityTermsAmbiguous}}},
		{"no close time and no deadline", crypto("Will Bitcoin hit $150k by September 30?", "", time.Time{}), want{
			"BTC", MetricPriceHigh, canonical.ComparatorUnknown, nil, nil, UnitUSD,
			nil, canonical.Unknown, canonical.Unknown, canonical.PolarityUnknown,
			[]canonical.QualityFlag{canonical.QualityMissingField}}},
		{"title says close, rules say high: metric conflict", crypto("Will the price of Bitcoin be above $74,000 on October 7?",
			polyHitRules, closeNoon7), want{
			"BTC", canonical.Unknown, canonical.ComparatorGTE, dec("74000"), nil, UnitUSD,
			utc(2026, 10, 8, 3, 59), "America/New_York", "binance_btc_usdt", canonical.PolarityPositive,
			[]canonical.QualityFlag{QualityDeadlineYearInferred}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := Normalize(tc.in)
			got := out.Terms
			if err := got.Validate(); err != nil {
				t.Fatalf("terms invalid: %v", err)
			}
			if got.Subject != tc.want.subject || got.Metric != tc.want.metric || got.Comparator != tc.want.comparator ||
				got.Unit != tc.want.unit || got.DeadlineTZ != tc.want.tz || got.ResolutionSource != tc.want.source ||
				got.Polarity != tc.want.polarity || got.EdgeRules != canonical.Unknown {
				t.Errorf("terms = %+v, want %+v", got, tc.want)
			}
			if !decEqual(got.Threshold, tc.want.threshold) || !decEqual(got.ThresholdHigh, tc.want.high) {
				t.Errorf("thresholds = %s/%s, want %s/%s", str(got.Threshold), str(got.ThresholdHigh), str(tc.want.threshold), str(tc.want.high))
			}
			switch {
			case (got.Deadline == nil) != (tc.want.deadline == nil):
				t.Errorf("deadline = %v, want %v", got.Deadline, tc.want.deadline)
			case got.Deadline != nil && (!got.Deadline.Equal(*tc.want.deadline) || got.Deadline.Location() != time.UTC):
				t.Errorf("deadline = %v, want %v (UTC)", got.Deadline, tc.want.deadline)
			}
			for _, f := range tc.want.flags {
				if !out.Quality.Has(f) {
					t.Errorf("quality %v lacks %q", out.Quality, f)
				}
			}
			if len(tc.want.flags) == 0 && !out.Quality.OK() {
				t.Errorf("unexpected quality issues %v", out.Quality)
			}
			if !strings.Contains(out.Title, tc.in.Title) {
				t.Error("Normalize changed the title")
			}
		})
	}
}

func decEqual(a, b *decimal.Decimal) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

func str(d *decimal.Decimal) string {
	if d == nil {
		return "nil"
	}
	return d.String()
}

func TestExtractCryptoExtractMatchesNormalize(t *testing.T) {
	in := crypto("Bitcoin price on Oct 6, 2026? — $76,600 or above", kalshiBTCRules, closeOct6)
	if Extract(in).Subject != Normalize(in).Terms.Subject || Extract(in).Threshold.String() != "76599.99" {
		t.Error("Extract and Normalize disagree")
	}
}

func TestExtractNonCryptoIsUnknown(t *testing.T) {
	in := crypto("Will the upper bound of the federal funds rate be above 3.00% following the Fed's Oct 28, 2026 meeting?",
		"If the upper bound is greater than 3.00% following the Oct 28, 2026 meeting, then the market resolves to Yes.", closeOct6)
	in.Category = canonical.CategoryFed
	out := Normalize(in)
	if out.Terms != canonical.UnknownTerms() || !out.Quality.OK() {
		t.Errorf("fed terms = %+v, quality %v; want all unknown and no flags", out.Terms, out.Quality)
	}
}

func TestExtractCryptoQualityIsAppendedNotReplaced(t *testing.T) {
	in := crypto("Bitcoin price on Oct 6, 2026?", "", closeOct6)
	in.Quality.Add(canonical.QualityMissingField, "rules_primary", "no rules text")
	out := Normalize(in)
	if !out.Quality.Has(canonical.QualityMissingField) || !out.Quality.Has(canonical.QualityDeadlineDefaulted) {
		t.Errorf("quality = %v", out.Quality)
	}
	if len(in.Quality) != 1 || in.Quality[0].Flag != canonical.QualityMissingField {
		t.Error("Normalize mutated its input")
	}
}

func TestCryptoDeadlineZones(t *testing.T) {
	for zone, want := range map[string]time.Time{
		"ET":  time.Date(2026, 1, 15, 22, 0, 0, 0, time.UTC), // EST in January
		"EDT": time.Date(2026, 1, 15, 22, 0, 0, 0, time.UTC), // the venue's abbreviation is ignored; the zone decides
		"CT":  time.Date(2026, 1, 15, 23, 0, 0, 0, time.UTC),
		"PT":  time.Date(2026, 1, 16, 1, 0, 0, 0, time.UTC),
		"UTC": time.Date(2026, 1, 15, 17, 0, 0, 0, time.UTC),
		"GMT": time.Date(2026, 1, 15, 17, 0, 0, 0, time.UTC),
	} {
		in := crypto("Bitcoin price on Jan 15, 2026?", "If BRTI is above 100000 at 5 PM "+zone+" on Jan 15, 2026, Yes.", want)
		got := Normalize(in).Terms.Deadline
		if got == nil || !got.Equal(want) {
			t.Errorf("%s: deadline %v, want %v", zone, got, want)
		}
	}
}

func TestCryptoDeadlineAmbiguousClockIsSkipped(t *testing.T) {
	in := crypto("Bitcoin price on Jan 15, 2026?", "If BRTI is above 100000 at 5 ET on Jan 15, 2026, Yes.", closeOct6)
	out := Normalize(in)
	if !out.Quality.Has(canonical.QualityDeadlineDefaulted) || out.Terms.DeadlineTZ != canonical.Unknown {
		t.Errorf("ambiguous '5 ET' accepted: %+v %v", out.Terms, out.Quality)
	}
	in = crypto("Bitcoin price on Jan 15, 2026?", "If BRTI is above 100000 at 5 PM ET or 6 PM ET on Jan 15, 2026, Yes.", closeOct6)
	out = Normalize(in)
	if !out.Quality.Has(canonical.QualityDeadlineDefaulted) {
		t.Errorf("two clocks accepted: %+v %v", out.Terms, out.Quality)
	}
}

func TestCryptoSourceSeveralAuthoritiesIsUnknown(t *testing.T) {
	rules := "The resolution source for this market is Binance. The price is based on Coinbase."
	if got := cryptoSource(rules); got != canonical.Unknown {
		t.Errorf("two authoritative sources gave %q", got)
	}
	if got := cryptoSource("Prices from Coinbase and Kraken may differ."); got != canonical.Unknown {
		t.Errorf("two mentions gave %q", got)
	}
	if got := cryptoSource("Resolves per Kraken."); got != "kraken" {
		t.Errorf("single mention gave %q", got)
	}
}

func TestCryptoHostileTextDoesNotPanic(t *testing.T) {
	huge := strings.Repeat("above $1,000 or above 2,000 between $3 and $4 5 PM ET Oct 6, 2026 ", 2000)
	in := crypto(huge, huge+"\x00\xff\xfe", closeOct6)
	out := Normalize(in)
	if err := out.Terms.Validate(); err != nil {
		t.Fatal(err)
	}
	if out.Terms.Comparator != canonical.ComparatorUnknown {
		t.Errorf("repeated contradictory conditions gave %q", out.Terms.Comparator)
	}
}

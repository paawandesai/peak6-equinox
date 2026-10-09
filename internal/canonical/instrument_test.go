package canonical

import (
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func validInstrument() Instrument {
	return Instrument{
		ID:            "kalshi:KXBTCD-26OCT0617-T120000",
		Venue:         VenueKalshi,
		VenueMarketID: "KXBTCD-26OCT0617-T120000",
		VenueEventID:  "KXBTCD-26OCT0617",
		Category:      CategoryCrypto,
		Title:         "Bitcoin above $120,000 on Oct 6, 2026 at 5pm EDT?",
		RulesText:     "If the CF Benchmarks BRTI is above 120000 at 5pm EDT ...",
		Terms:         UnknownTerms(),
		CloseTime:     t0,
		Status:        StatusOpen,
		FeeSchedule:   FeeSchedule{ID: FeeScheduleKalshiTakerV1, Params: map[string]decimal.Decimal{"multiplier": d("0.07")}},
	}
}

func TestInstrumentValidate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(i *Instrument)
		wantErr string
	}{
		{"valid", func(*Instrument) {}, ""},
		{"zero close time allowed", func(i *Instrument) { i.CloseTime = time.Time{} }, ""},
		{"polymarket", func(i *Instrument) {
			i.ID, i.Venue, i.FeeSchedule.ID = "polymarket:7132910459", VenuePolymarket, FeeSchedulePolymarketV1
		}, ""},
		{"bad id", func(i *Instrument) { i.ID = "kalshi:'; DROP TABLE instruments; --" }, "instrument id"},
		{"empty key", func(i *Instrument) { i.ID = "kalshi:" }, "instrument id"},
		{"venue mismatch", func(i *Instrument) { i.ID = "polymarket:123" }, "does not match venue"},
		{"bad venue", func(i *Instrument) { i.Venue = "nyse" }, "venue \"nyse\" is not supported"},
		{"bad category", func(i *Instrument) { i.Category = "sports" }, "category"},
		{"bad status", func(i *Instrument) { i.Status = "" }, "status"},
		{"no market id", func(i *Instrument) { i.VenueMarketID = "" }, "venue_market_id"},
		{"no title", func(i *Instrument) { i.Title = "" }, "title is required"},
		{"non-utc close", func(i *Instrument) { i.CloseTime = t0.In(cdt) }, "close_time must be UTC"},
		{"non-utc expiration", func(i *Instrument) { i.ExpirationTime = t0.In(cdt) }, "expiration_time must be UTC"},
		{"non-utc clarifications", func(i *Instrument) { i.ClarificationsUpdatedAt = t0.In(cdt) }, "clarifications_updated_at must be UTC"},
		{"empty term", func(i *Instrument) { i.Terms.Subject = "" }, "terms.subject is empty"},
		{"bad fee id", func(i *Instrument) { i.FeeSchedule.ID = "Bad ID" }, "fee schedule id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			i := validInstrument()
			tc.mutate(&i)
			err := i.Validate()
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

func TestValidateInstrumentID(t *testing.T) {
	good := []string{"kalshi:KXBTCD-26OCT0617-T120000", "polymarket:71321045679252212594626385532706912750332728571942532289631379312455583992563", "kalshi:A.B_C-1"}
	for _, id := range good {
		if err := ValidateInstrumentID(id); err != nil {
			t.Errorf("%q: %v", id, err)
		}
	}
	bad := []string{"", "kalshi", "kalshi:", "nyse:ABC", "kalshi:-ABC", "kalshi:A B", "kalshi:A;B", "kalshi:'--", "KALSHI:ABC",
		"kalshi:" + strings.Repeat("A", 129), "kalshi:ABC\n", "polymarket:1' OR '1'='1"}
	for _, id := range bad {
		if err := ValidateInstrumentID(id); err == nil {
			t.Errorf("%q accepted", id)
		}
	}
	if got := InstrumentID(VenuePolymarket, "42"); got != "polymarket:42" {
		t.Errorf("InstrumentID = %q", got)
	}
	if VenueOf("kalshi:X") != VenueKalshi || VenueOf("nocolon") != "" {
		t.Error("VenueOf")
	}
}

func TestSettledValidate(t *testing.T) {
	s := Settled{Instrument: validInstrument(), Outcome: OutcomeYes, SettledAt: t0}
	if err := s.Validate(); err != nil {
		t.Fatalf("valid settled: %v", err)
	}
	s.Outcome = "maybe"
	s.SettledAt = t0.In(cdt)
	s.Instrument.Title = ""
	err := s.Validate()
	for _, want := range []string{"outcome", "settled_at must be UTC", "title is required"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want %q", err, want)
		}
	}
}

func TestEnums(t *testing.T) {
	for _, v := range Venues() {
		if !v.Valid() {
			t.Errorf("venue %q invalid", v)
		}
	}
	for _, c := range Categories() {
		if !c.Valid() {
			t.Errorf("category %q invalid", c)
		}
	}
	if Venue("").Valid() || Category("").Valid() || Outcome("").Valid() || Side("").Valid() ||
		OrderType("").Valid() || TimeInForce("").Valid() || Method("").Valid() || DiffStatus("").Valid() ||
		Comparator("").Valid() || Polarity("").Valid() || InstrumentStatus("").Valid() {
		t.Error("empty enum value accepted")
	}
	for _, c := range []Comparator{ComparatorGT, ComparatorGTE, ComparatorLT, ComparatorLTE, ComparatorEQ, ComparatorBetween, ComparatorUnknown} {
		if !c.Valid() {
			t.Errorf("comparator %q invalid", c)
		}
	}
}

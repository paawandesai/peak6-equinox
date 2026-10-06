package canonical

import (
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	"pgregory.net/rapid"
)

func dp(s string) *decimal.Decimal { v := d(s); return &v }

func validOrder() Order {
	return Order{Side: SideBuy, Outcome: OutcomeYes, Quantity: dp("100"), Type: OrderMarket, TIF: TIFIOC}
}

func TestOrderValidate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(o *Order)
		wantErr string
	}{
		{"market", func(*Order) {}, ""},
		{"limit", func(o *Order) { o.Type, o.LimitPrice, o.TIF = OrderLimit, dp("0.55"), TIFFOK }, ""},
		{"notional", func(o *Order) { o.Quantity, o.Notional = nil, dp("250") }, ""},
		{"bad side", func(o *Order) { o.Side = "short" }, "side"},
		{"bad outcome", func(o *Order) { o.Outcome = "maybe" }, "outcome"},
		{"bad type", func(o *Order) { o.Type = "stop" }, "type"},
		{"bad tif", func(o *Order) { o.TIF = "GTC" }, "tif"},
		{"no size", func(o *Order) { o.Quantity = nil }, "exactly one"},
		{"both sizes", func(o *Order) { o.Notional = dp("1") }, "exactly one"},
		{"zero qty", func(o *Order) { o.Quantity = dp("0") }, "quantity 0 must be positive"},
		{"negative notional", func(o *Order) { o.Quantity, o.Notional = nil, dp("-5") }, "notional -5"},
		{"limit without price", func(o *Order) { o.Type = OrderLimit }, "need a limit price"},
		{"market with price", func(o *Order) { o.LimitPrice = dp("0.5") }, "take no limit price"},
		{"limit price > 1", func(o *Order) { o.Type, o.LimitPrice = OrderLimit, dp("1.2") }, "limit price"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := validOrder()
			tc.mutate(&o)
			err := o.Validate()
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

func TestOrderLimitPriceProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		p := genDecimal().Draw(t, "limit")
		o := validOrder()
		o.Type, o.LimitPrice = OrderLimit, &p
		want := ValidatePrice(p) == nil
		if got := o.Validate() == nil; got != want {
			t.Fatalf("limit %s: ok=%v, want %v", p, got, want)
		}
	})
}

func TestDecisionValidate(t *testing.T) {
	dec := Decision{RoundID: 3, TargetID: "kalshi:X", Order: validOrder(), Explanation: "Kalshi is cheapest.", DecidedAt: t0}
	if err := dec.Validate(); err != nil {
		t.Fatalf("valid: %v", err)
	}
	bad := Decision{TargetID: "bad", Order: Order{}, DecidedAt: t0.In(cdt)}
	err := bad.Validate()
	for _, want := range []string{"round_id", "instrument id", "side", "decided_at must be UTC", "explanation is required"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want %q", err, want)
		}
	}
}

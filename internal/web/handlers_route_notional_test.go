package web

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	"pgregory.net/rapid"
)

// Issue #74: USD-notional quantity option

// TestRouteFormNotionalUSD tests routing with USD notional instead of contracts.
func TestRouteFormNotionalUSD(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)

	// Buy $50 worth. The router will convert using the first available touch.
	// From seedPublic: Kalshi best ask is 0.52, so 50 / 0.52 = 96.15... → 96 contracts (floored)
	form := url.Values{
		"pair":          {strconv.FormatInt(sd.eqID, 10)},
		"target":        {seedKalshiA},
		"side":          {"buy"},
		"outcome":       {"yes"},
		"quantity":      {"50.00"},
		"quantity_mode": {"usd"},
		"type":          {"market"},
		"tif":           {"IOC"},
	}

	rec := postForm(s.Handler(), "/route", form, true)
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	wantContains(t, body,
		`id="route-result"`,
		"Decision #1",
		"buy $50",              // Order line should show notional
		"Notional conversion:", // Conversion line should be present
		"contracts",            // Shows converted contracts (actual value depends on seeded touch)
		"rounded down",         // Indicates floor() was applied
	)

	// Verify the decision was stored with Notional, not Quantity
	dec, err := st.GetRoutingDecision(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Decision.Order.Notional == nil {
		t.Error("stored order has nil Notional, want $50")
	}
	if dec.Decision.Order.Quantity != nil {
		t.Error("stored order has non-nil Quantity, want nil (notional order)")
	}
	if dec.Decision.Order.Notional != nil && !dec.Decision.Order.Notional.Equal(decimal.NewFromInt(50)) {
		t.Errorf("stored order Notional = %s, want $50", dec.Decision.Order.Notional)
	}
}

// TestRouteFormContractsMode tests the default contracts mode still works.
func TestRouteFormContractsMode(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)

	form := url.Values{
		"pair":          {strconv.FormatInt(sd.eqID, 10)},
		"target":        {seedKalshiA},
		"side":          {"buy"},
		"outcome":       {"yes"},
		"quantity":      {"100"},
		"quantity_mode": {"contracts"},
		"type":          {"market"},
		"tif":           {"IOC"},
	}

	rec := postForm(s.Handler(), "/route", form, true)
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	wantContains(t, body,
		`id="route-result"`,
		"Decision #1",
		"buy 100 contracts YES", // Order line should show contracts
	)

	// Notional conversion line should NOT be present
	if strings.Contains(body, "Notional conversion:") {
		t.Error("contracts mode shows notional conversion, want none")
	}

	// Verify the decision was stored with Quantity, not Notional
	dec, err := st.GetRoutingDecision(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Decision.Order.Quantity == nil {
		t.Error("stored order has nil Quantity, want 100")
	}
	if dec.Decision.Order.Notional != nil {
		t.Error("stored order has non-nil Notional, want nil (contracts order)")
	}
}

// TestRouteFormNotionalValidation tests AB-08 validation for notional mode.
func TestRouteFormNotionalValidation(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)

	cases := []struct {
		name     string
		notional string
		wantErr  string
	}{
		{"zero", "0", "notional must be between $0.01 and $100,000"},
		{"negative", "-10", "notional must be between $0.01 and $100,000"},
		{"too small", "0.001", "notional must be between $0.01 and $100,000"},
		{"too large", "100001", "notional must be between $0.01 and $100,000"},
		{"non-numeric", "abc", "notional must be a USD amount"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{
				"pair":          {strconv.FormatInt(sd.eqID, 10)},
				"target":        {seedKalshiA},
				"side":          {"buy"},
				"outcome":       {"yes"},
				"quantity":      {tc.notional},
				"quantity_mode": {"usd"},
				"type":          {"market"},
				"tif":           {"IOC"},
			}

			rec := postForm(s.Handler(), "/route", form, true)
			wantHTML(t, rec, http.StatusBadRequest)
			body := rec.Body.String()
			wantContains(t, body, tc.wantErr)

			// No decision should be stored
			decs, _ := countDecisions(t, st)
			if decs != 0 {
				t.Errorf("invalid form stored %d decisions, want 0", decs)
			}
		})
	}
}

// TestRouteFormContractsValidation tests AB-08 validation for contracts mode.
func TestRouteFormContractsValidation(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)

	cases := []struct {
		name     string
		quantity string
		wantErr  string
	}{
		{"zero", "0", "quantity must be between 1 and 100000 contracts"},
		{"negative", "-10", "quantity must be between 1 and 100000 contracts"},
		{"too large", "100001", "quantity must be between 1 and 100000 contracts"},
		{"non-numeric", "xyz", "quantity must be a number of contracts"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{
				"pair":          {strconv.FormatInt(sd.eqID, 10)},
				"target":        {seedKalshiA},
				"side":          {"buy"},
				"outcome":       {"yes"},
				"quantity":      {tc.quantity},
				"quantity_mode": {"contracts"},
				"type":          {"market"},
				"tif":           {"IOC"},
			}

			rec := postForm(s.Handler(), "/route", form, true)
			wantHTML(t, rec, http.StatusBadRequest)
			body := rec.Body.String()
			wantContains(t, body, tc.wantErr)

			// No decision should be stored
			decs, _ := countDecisions(t, st)
			if decs != 0 {
				t.Errorf("invalid form stored %d decisions, want 0", decs)
			}
		})
	}
}

// TestRouteFormBackwardCompatibility tests that forms without quantity_mode default to contracts.
func TestRouteFormBackwardCompatibility(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)

	// Submit form without quantity_mode (simulates old clients or bookmarks)
	form := url.Values{
		"pair":     {strconv.FormatInt(sd.eqID, 10)},
		"target":   {seedKalshiA},
		"side":     {"buy"},
		"outcome":  {"yes"},
		"quantity": {"100"},
		"type":     {"market"},
		"tif":      {"IOC"},
	}

	rec := postForm(s.Handler(), "/route", form, true)
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	wantContains(t, body,
		`id="route-result"`,
		"buy 100 contracts YES", // Should default to contracts mode
	)

	// Verify the decision was stored with Quantity
	dec, err := st.GetRoutingDecision(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Decision.Order.Quantity == nil {
		t.Error("backward-compatible order has nil Quantity, want 100")
	}
	if dec.Decision.Order.Notional != nil {
		t.Error("backward-compatible order has non-nil Notional, want nil")
	}
}

// TestNotionalConversionRoundsDown tests that fractional contracts round down.
func TestNotionalConversionRoundsDown(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)

	// Buy $52.67 worth. The router uses the first touch (0.52 from Kalshi).
	// 52.67 / 0.52 = 101.28... which should round down to 101 contracts
	form := url.Values{
		"pair":          {strconv.FormatInt(sd.eqID, 10)},
		"target":        {seedKalshiA},
		"side":          {"buy"},
		"outcome":       {"yes"},
		"quantity":      {"52.67"},
		"quantity_mode": {"usd"},
		"type":          {"market"},
		"tif":           {"IOC"},
	}

	rec := postForm(s.Handler(), "/route", form, true)
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	wantContains(t, body,
		`id="route-result"`,
		"Notional conversion:",
		"contracts", // Verifies conversion happened
		"rounded down",
	)
}

// TestNotionalConversionProperty is a property test for notional-to-contracts conversion.
func TestNotionalConversionProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random notional amount between $0.01 and $100,000
		notionalCents := rapid.Int64Range(1, 10_000_000).Draw(t, "notional_cents")
		notional := decimal.NewFromInt(notionalCents).Div(decimal.NewFromInt(100))

		// Generate a random touch price between 0.01 and 0.99
		touchCents := rapid.Int64Range(1, 99).Draw(t, "touch_cents")
		touch := decimal.NewFromInt(touchCents).Div(decimal.NewFromInt(100))

		// Compute conversion: contracts = floor(notional / touch)
		computed := notional.Div(touch).Floor()

		// Properties:
		// 1. Result is non-negative
		if computed.IsNegative() {
			t.Fatalf("conversion produced negative contracts: %s / %s = %s",
				notional, touch, computed)
		}

		// 2. Result * touch <= notional (you can't buy more than you paid for)
		cost := computed.Mul(touch)
		if cost.GreaterThan(notional) {
			t.Fatalf("converted contracts cost more than notional: %s * %s = %s > %s",
				computed, touch, cost, notional)
		}

		// 3. (Result + 1) * touch > notional (you can't afford one more contract)
		nextCost := computed.Add(decimal.NewFromInt(1)).Mul(touch)
		if nextCost.LessThanOrEqual(notional) {
			t.Fatalf("could afford one more contract: (%s + 1) * %s = %s <= %s",
				computed, touch, nextCost, notional)
		}

		// 4. Result is an integer (floor guarantees this)
		if !computed.Equal(computed.Floor()) {
			t.Fatalf("conversion produced fractional contracts: %s", computed)
		}
	})
}

// TestNotionalConversionEdgeCases tests edge cases for notional conversion.
func TestNotionalConversionEdgeCases(t *testing.T) {
	cases := []struct {
		name     string
		notional string
		touch    decimal.Decimal
		want     decimal.Decimal
	}{
		{"exact division", "50", decimal.NewFromFloat(0.50), decimal.NewFromInt(100)},
		{"rounds down", "50", decimal.NewFromFloat(0.51), decimal.NewFromInt(98)}, // 50/0.51 = 98.03...
		{"very small notional", "0.01", decimal.NewFromFloat(0.99), decimal.Zero}, // 0.01/0.99 = 0.01, floors to 0
		{"large notional", "100000", decimal.NewFromFloat(0.01), decimal.NewFromInt(10_000_000)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			notional, _ := decimal.NewFromString(tc.notional)
			got := notional.Div(tc.touch).Floor()
			if !got.Equal(tc.want) {
				t.Errorf("notional %s / touch %s = %s (floored), want %s",
					notional, tc.touch, got, tc.want)
			}
		})
	}
}

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

const (
	routeKalshiID     = "kalshi:KXBTCD-26OCT0617-T120000"
	routePolymarketID = "polymarket:12345678901234567890"
)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestParseRouteArgs(t *testing.T) {
	ra, o, err := parseRouteArgs([]string{"--target", routeKalshiID, "--side", "buy", "--outcome", "yes", "--qty", "100"})
	if err != nil {
		t.Fatal(err)
	}
	if ra.target != routeKalshiID || ra.round != 0 || ra.json {
		t.Errorf("args = %+v", ra)
	}
	if o.Side != canonical.SideBuy || o.Outcome != canonical.OutcomeYes || !o.Quantity.Equal(dec("100")) ||
		o.Type != canonical.OrderMarket || o.TIF != canonical.TIFIOC || o.LimitPrice != nil || o.Notional != nil {
		t.Errorf("order = %+v", o)
	}

	ra, o, err = parseRouteArgs([]string{"--target", routeKalshiID, "--side", "SELL", "--outcome", "NO", "--notional", "50",
		"--limit", "0.45", "--tif", "fok", "--round", "3", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	if ra.round != 3 || !ra.json {
		t.Errorf("args = %+v", ra)
	}
	if o.Side != canonical.SideSell || o.Outcome != canonical.OutcomeNo || !o.Notional.Equal(dec("50")) ||
		o.Type != canonical.OrderLimit || !o.LimitPrice.Equal(dec("0.45")) || o.TIF != canonical.TIFFOK {
		t.Errorf("order = %+v", o)
	}

	bad := [][]string{
		{},
		{"--target", "nowhere:1", "--qty", "1"},
		{"--target", routeKalshiID, "--qty", "1", "--notional", "5"},
		{"--target", routeKalshiID},
		{"--target", routeKalshiID, "--qty", "abc"},
		{"--target", routeKalshiID, "--qty", "1", "--side", "hold"},
		{"--target", routeKalshiID, "--qty", "1", "--limit", "1.5"},
		{"--target", routeKalshiID, "--qty", "1", "--type", "market", "--limit", "0.5"},
		{"--target", routeKalshiID, "--qty", "1", "--type", "limit"},
		{"--target", routeKalshiID, "--qty", "1", "--tif", "GTC"},
		{"--target", routeKalshiID, "--qty", "1", "extra"},
		{"--target", routeKalshiID, "--qty", "1", "--bogus"},
	}
	for _, args := range bad {
		if _, _, err := parseRouteArgs(args); !errors.Is(err, errUsage) {
			t.Errorf("%v: err = %v, want a usage error", args, err)
		}
	}
}

func sampleDecision() canonical.Decision {
	q := dec("100")
	leg := canonical.Leg{InstrumentID: routePolymarketID, Venue: canonical.VenuePolymarket, Side: canonical.SideBuy, Outcome: canonical.OutcomeYes,
		Quantity: q, VWAP: dec("0.515"), FeePerContract: dec("0.0175"), AllInPerContract: dec("0.5325"), Route: "direct"}
	return canonical.Decision{
		RoundID: 7, TargetID: routeKalshiID,
		Order:        canonical.Order{Side: canonical.SideBuy, Outcome: canonical.OutcomeYes, Quantity: &q, Type: canonical.OrderMarket, TIF: canonical.TIFIOC},
		Chosen:       []canonical.Leg{leg},
		Alternatives: []canonical.Alternative{{Rank: 1, Legs: []canonical.Leg{leg}, AllInPerContract: leg.AllInPerContract, Note: "chosen"}},
		Exclusions:   []canonical.Exclusion{{InstrumentID: routeKalshiID, Venue: canonical.VenueKalshi, Reason: "no_book", Detail: "no asks on the yes side"}},
		Warnings:     []string{"basis-risk penalty is not applied in v0"},
		Explanation:  "Simulated buy of 100 YES contracts.",
		DecidedAt:    time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC),
	}
}

func TestPrintDecision(t *testing.T) {
	var out bytes.Buffer
	if err := printDecision(&out, 42, sampleDecision()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Routing decision #42 (round 7) for " + routeKalshiID,
		"Order:    buy 100 contracts YES market IOC",
		"Chosen:   polymarket " + routePolymarketID + " (direct): 100 contracts, VWAP 0.5150, fee 0.0175, basis 0.0000, all-in 0.5325",
		"#1 polymarket " + routePolymarketID + ": all-in 0.5325",
		"[chosen]",
		"kalshi " + routeKalshiID + ": no_book (no asks on the yes side)",
		"- basis-risk penalty",
		"Explanation:\n  Simulated buy",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}

	d := sampleDecision()
	d.Chosen, d.Alternatives = nil, nil
	n := dec("25")
	d.Order.Quantity, d.Order.Notional, d.Order.Type, d.Order.LimitPrice = nil, &n, canonical.OrderLimit, &[]decimal.Decimal{dec("0.5")}[0]
	out.Reset()
	if err := printDecision(&out, 1, d); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Chosen:   none") || !strings.Contains(out.String(), "$25 notional YES limit IOC limit 0.5000") {
		t.Errorf("no-fill output:\n%s", out.String())
	}
}

func TestLogSummary(t *testing.T) {
	d := sampleDecision()
	if got := logSummary(d); got != "route buy 100 contracts yes on "+routeKalshiID+": polymarket "+routePolymarketID+" at 0.5325 per contract" {
		t.Errorf("summary = %q", got)
	}
	d.Chosen = nil
	n := dec("25")
	d.Order.Quantity, d.Order.Notional = nil, &n
	if got := logSummary(d); got != "route buy $25 yes on "+routeKalshiID+": no venue can fill (1 excluded)" {
		t.Errorf("summary = %q", got)
	}
}

// seedRouteRound stores a finished round with a Kalshi target, an equivalent
// Polymarket partner, and books on both.
func seedRouteRound(t *testing.T, s *store.Store) store.Round {
	t.Helper()
	ctx := context.Background()
	t0 := time.Now().UTC()
	round, err := s.CreateRound(ctx, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRound(ctx, round.ID, store.RoundOK, nil, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	target := canonical.Instrument{
		ID: routeKalshiID, Venue: canonical.VenueKalshi, VenueMarketID: "KXBTCD-26OCT0617-T120000", VenueEventID: "KXBTCD-26OCT0617",
		Category: canonical.CategoryCrypto, Title: "Bitcoin above $120,000?", Terms: canonical.UnknownTerms(), CloseTime: t0, Status: canonical.StatusOpen,
		FeeSchedule: canonical.FeeSchedule{ID: canonical.FeeScheduleKalshiTakerV1, Params: map[string]decimal.Decimal{"multiplier": dec("0.07")}},
	}
	partner := canonical.Instrument{
		ID: routePolymarketID, Venue: canonical.VenuePolymarket, VenueMarketID: "0xabc", VenueEventID: "btc-120k",
		Category: canonical.CategoryCrypto, Title: "Will Bitcoin be above $120,000?", Terms: canonical.UnknownTerms(), CloseTime: t0, Status: canonical.StatusOpen,
		FeeSchedule: canonical.FeeSchedule{ID: canonical.FeeSchedulePolymarketV1, Params: map[string]decimal.Decimal{"rate": dec("0.07")},
			Verified: true, Source: "https://docs.polymarket.com/trading/fees"},
	}
	for _, in := range []canonical.Instrument{target, partner} {
		if err := s.UpsertInstrument(ctx, round.ID, in); err != nil {
			t.Fatal(err)
		}
	}
	quotes := []canonical.Quote{
		{InstrumentID: routeKalshiID, YesBids: []canonical.Level{{Price: dec("0.48"), Size: dec("200")}}, YesAsks: []canonical.Level{{Price: dec("0.52"), Size: dec("100")}}, FetchedAt: t0},
		{InstrumentID: routePolymarketID, YesBids: []canonical.Level{{Price: dec("0.49"), Size: dec("80")}}, YesAsks: []canonical.Level{{Price: dec("0.50"), Size: dec("50")}, {Price: dec("0.53"), Size: dec("100")}}, FetchedAt: t0},
	}
	for _, q := range quotes {
		if err := s.InsertQuote(ctx, round.ID, q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.UpsertRelationship(ctx, canonical.Relationship{RoundID: round.ID, AID: routeKalshiID, BID: routePolymarketID,
		Type: canonical.RelEquivalent, Confidence: dec("0.97"), Method: canonical.MethodRules, Explanation: "same proposition"}); err != nil {
		t.Fatal(err)
	}
	return round
}

// TestRouteCommandEndToEnd runs `equinox route` against a seeded round, then
// checks the decision was printed, stored and logged.
func TestRouteCommandEndToEnd(t *testing.T) {
	setAppEnv(t, tdb.DSN(t))
	tdb.Reset(t)
	ctx := context.Background()
	s := store.New(tdb.Pool(t))
	round := seedRouteRound(t, s)

	var out, errOut bytes.Buffer
	args := []string{"route", "--target", routeKalshiID, "--side", "buy", "--outcome", "yes", "--qty", "100"}
	if code := run(ctx, args, &out, &errOut); code != 0 {
		t.Fatalf("exit %d, stderr %s", code, errOut.String())
	}
	for _, want := range []string{"Chosen:   polymarket " + routePolymarketID, "#2 kalshi " + routeKalshiID, "Best route: polymarket"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}

	out.Reset()
	args = append(args, "--json", "--round", "0", "--tif", "FOK")
	if code := run(ctx, args, &out, &errOut); code != 0 {
		t.Fatalf("json: exit %d, stderr %s", code, errOut.String())
	}
	var d canonical.Decision
	if err := json.Unmarshal(out.Bytes(), &d); err != nil {
		t.Fatalf("decoding json output: %v\n%s", err, out.String())
	}
	if d.RoundID != round.ID || d.TargetID != routeKalshiID || len(d.Chosen) != 1 || d.Chosen[0].InstrumentID != routePolymarketID || d.Order.TIF != canonical.TIFFOK {
		t.Errorf("json decision = %+v", d)
	}

	stored, err := s.ListRoutingDecisions(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 || stored[0].Decision.Explanation == "" {
		t.Errorf("stored decisions = %+v", stored)
	}
	var logged int
	if err := tdb.Pool(t).QueryRow(ctx, `SELECT count(*) FROM decision_log WHERE kind = 'routing'`).Scan(&logged); err != nil {
		t.Fatal(err)
	}
	if logged != 2 {
		t.Errorf("decision_log routing entries = %d, want 2", logged)
	}

	// A missing quote becomes an empty no_book candidate rather than an error.
	if _, err := tdb.Pool(t).Exec(ctx, `DELETE FROM quotes WHERE instrument_id = $1`, routeKalshiID); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := run(ctx, []string{"route", "--target", routeKalshiID, "--qty", "10"}, &out, &errOut); code != 0 {
		t.Fatalf("no-quote: exit %d, stderr %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), routeKalshiID+": no_book") {
		t.Errorf("no-quote output:\n%s", out.String())
	}
}

func TestRouteCommandErrors(t *testing.T) {
	setAppEnv(t, tdb.DSN(t))
	tdb.Reset(t)
	ctx := context.Background()
	s := store.New(tdb.Pool(t))
	round := seedRouteRound(t, s)

	cases := []struct {
		name string
		args []string
		code int
		want string
	}{
		{"usage", []string{"route"}, 2, "--target"},
		{"unknown target", []string{"route", "--target", "kalshi:NOPE", "--qty", "1"}, 1, "loading target"},
		{"unknown round", []string{"route", "--target", routeKalshiID, "--qty", "1", "--round", "999999"}, 1, "loading round"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if code := run(ctx, tc.args, &out, &errOut); code != tc.code {
				t.Fatalf("exit %d, want %d; stderr %s", code, tc.code, errOut.String())
			}
			if !strings.Contains(errOut.String(), tc.want) {
				t.Errorf("stderr %q lacks %q", errOut.String(), tc.want)
			}
		})
	}
	_ = round
}

func TestRouteCommandNoDatabase(t *testing.T) {
	setAppEnv(t, "postgres://nobody@127.0.0.1:1/none?connect_timeout=1")
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"route", "--target", routeKalshiID, "--qty", "1"}, &out, &errOut); code != 1 {
		t.Fatalf("exit %d, stderr %s", code, errOut.String())
	}
}

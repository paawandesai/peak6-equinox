package web

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// The guided demo path on / (M6 P1). Every step is resolved by instrument ID
// in the current round, because pair ids change with every match round. A
// step that cannot be resolved is left out rather than linked to nothing.

// Fed vocabulary as the normaliser stores it (internal/normalize/fed.go). The
// web layer reads stored terms only; the strings are repeated here, as
// internal/match does, rather than importing normalize.
const (
	fedMetricUpperBound = "rate_upper_bound"
	fedMetricChange     = "rate_change"
	factFedUpperBound   = "fed_upper_bound"
	factFedNextMeeting  = "fed_next_meeting"
	kalshiFedPrefix     = "kalshi:KXFED-"
	// demoStrikeHigh and demoStrikeLow are the two Kalshi ladder strikes the
	// demo walks: above 4.25 % (same bet as "increase 50+ bps" given the
	// fed_upper_bound fact) and above 3.50 % (complement of "decrease 50+ bps").
	demoStrikeHigh = "T4.25"
	demoStrikeLow  = "T3.50"
	// demoQty is the preset order size, in contracts.
	demoQty = "100"
)

// startStep is one step of the "Start here" card.
type startStep struct {
	Text string // the link text
	Href string // a pair page URL, with preset query params and an anchor
	Note string // one line of context under the link (codes; not rendered on /)
	Demo bool   // true for the two demo-pair steps (diff and route)
	// ui/plain-language: the card title and one plain sentence, no codes.
	Title    string // "See a match"
	Sentence string // "Kalshi's 'Fed above 4.25%' and Polymarket's 'Fed +50 bps' pay out on the same thing."
	Num      int    // 1-based card number
}

// numberSteps numbers the cards in display order.
func numberSteps(steps []startStep) []startStep {
	for i := range steps {
		steps[i].Num = i + 1
	}
	return steps
}

// withFeaturedPair puts the featured routable pair's diff and route steps
// first when the Fed demo pair did not resolve, so the card always opens on a
// routable pair while keeping the other steps that did resolve (M5-05).
func withFeaturedPair(steps []startStep, featuredID int64) []startStep {
	if featuredID == 0 {
		return steps
	}
	for _, st := range steps {
		if st.Demo {
			return steps
		}
	}
	featured := []startStep{
		{Text: "See why two contracts are the same bet", Href: fmt.Sprintf("/pairs/%d", featuredID), Demo: true,
			Title: "See a match", Sentence: "Two contracts that pay out on the same thing, side by side."},
		{Text: "Route a hypothetical order on it", Href: fmt.Sprintf("/pairs/%d#route", featuredID), Demo: true,
			Title: "Route an order", Sentence: "Buy 100 YES and see which venue is cheaper, with the numbers."},
	}
	return append(featured, steps...)
}

// fedMeetingCode turns a meeting date (YYYY-MM-DD, the fed_next_meeting fact)
// into Kalshi's KXFED event code, e.g. 2026-10-28 -> 26OCT.
func fedMeetingCode(date string) (string, bool) {
	d, err := time.Parse("2006-01-02", strings.TrimSpace(date))
	if err != nil {
		return "", false
	}
	return strings.ToUpper(d.Format("06Jan")), true
}

// storedNextMeeting returns the fed_next_meeting value the matcher recorded on
// any relationship of the round, for when the server has no facts file.
func storedNextMeeting(rels []canonical.Relationship) string {
	for _, r := range rels {
		for _, f := range r.FactsUsed {
			if f.Key == factFedNextMeeting {
				return f.Value
			}
		}
	}
	return ""
}

// demoPresetHref builds a pair-page link that pre-fills the route form
// (applyRoutePreset) and jumps to it. Nothing is submitted.
func demoPresetHref(pairID int64, target, side, outcome string) string {
	q := url.Values{}
	q.Set("target", target)
	q.Set("side", side)
	q.Set("outcome", outcome)
	q.Set("qty", demoQty)
	q.Set("type", string(canonical.OrderMarket))
	q.Set("tif", string(canonical.TIFIOC))
	return fmt.Sprintf("/pairs/%d?%s#route", pairID, q.Encode())
}

// demoPair is a resolved demo relationship with its two sides.
type demoPair struct {
	rel    canonical.Relationship
	kalshi canonical.Instrument
	poly   canonical.Instrument
}

// fedSides returns the Kalshi KXFED level contract and the Polymarket change
// market of rel, in that order, when rel pairs those two kinds.
func fedSides(rel canonical.Relationship, insts map[string]store.InstrumentRecord) (canonical.Instrument, canonical.Instrument, bool) {
	a, okA := insts[rel.AID]
	b, okB := insts[rel.BID]
	if !okA || !okB {
		return canonical.Instrument{}, canonical.Instrument{}, false
	}
	k, p := a.Instrument, b.Instrument
	if k.Venue != canonical.VenueKalshi {
		k, p = p, k
	}
	if k.Venue != canonical.VenueKalshi || p.Venue != canonical.VenuePolymarket ||
		!strings.HasPrefix(k.ID, kalshiFedPrefix) ||
		k.Terms.Metric != fedMetricUpperBound || p.Terms.Metric != fedMetricChange {
		return canonical.Instrument{}, canonical.Instrument{}, false
	}
	return k, p, true
}

// findRoutableFedPair finds the routable pair between the Kalshi contract
// kalshiID and a Polymarket change market moving in the given direction
// (wantUp: an increase bucket, else a decrease bucket). Ties go to the higher
// confidence, then the lower pair id.
func findRoutableFedPair(rels []canonical.Relationship, insts map[string]store.InstrumentRecord, kalshiID string, wantUp bool) (demoPair, bool) {
	var found []demoPair
	for _, r := range rels {
		if !r.Type.Routable() || (r.AID != kalshiID && r.BID != kalshiID) {
			continue
		}
		k, p, ok := fedSides(r, insts)
		if !ok || k.ID != kalshiID || p.Terms.Threshold == nil {
			continue
		}
		if p.Terms.Threshold.IsPositive() != wantUp || p.Terms.Threshold.IsZero() {
			continue
		}
		found = append(found, demoPair{rel: r, kalshi: k, poly: p})
	}
	if len(found) == 0 {
		return demoPair{}, false
	}
	sort.SliceStable(found, func(i, j int) bool {
		if c := found[i].rel.Confidence.Cmp(found[j].rel.Confidence); c != 0 {
			return c > 0
		}
		return found[i].rel.ID < found[j].rel.ID
	})
	return found[0], true
}

// findFactDidNotApply finds the lowest-id related pair between a Kalshi KXFED
// level contract and a Polymarket change market where the matcher consulted
// fed_upper_bound and recorded that the fact does not apply (a later
// meeting's change depends on the prior decision).
func findFactDidNotApply(rels []canonical.Relationship, insts map[string]store.InstrumentRecord) (demoPair, bool) {
	var best demoPair
	ok := false
	for _, r := range rels {
		if r.Type != canonical.RelRelated || !consulted(r.FactsUsed, factFedUpperBound) ||
			!strings.Contains(r.Explanation, "does not apply") {
			continue
		}
		k, p, sides := fedSides(r, insts)
		if !sides {
			continue
		}
		if !ok || r.ID < best.rel.ID {
			best, ok = demoPair{rel: r, kalshi: k, poly: p}, true
		}
	}
	return best, ok
}

func consulted(used []canonical.FactUse, key string) bool {
	for _, f := range used {
		if f.Key == key {
			return true
		}
	}
	return false
}

// resolveStartHere builds the guided demo steps for the round. meeting is the
// fed_next_meeting date; when empty, the value the matcher stored on the
// round's relationships is used. Steps that do not resolve are omitted.
func resolveStartHere(rels []canonical.Relationship, insts map[string]store.InstrumentRecord, meeting string) []startStep {
	if meeting == "" {
		meeting = storedNextMeeting(rels)
	}
	var steps []startStep
	code, haveCode := fedMeetingCode(meeting)
	if haveCode {
		high := kalshiFedPrefix + code + "-" + demoStrikeHigh
		if p, ok := findRoutableFedPair(rels, insts, high, true); ok {
			steps = append(steps,
				startStep{
					Text:  "See why two contracts are the same bet",
					Href:  fmt.Sprintf("/pairs/%d", p.rel.ID),
					Note:  fmt.Sprintf("Kalshi %s vs Polymarket %q (%s)", p.kalshi.VenueMarketID, p.poly.Title, p.rel.Type),
					Demo:  true,
					Title: "See a match",
					Sentence: fmt.Sprintf("Kalshi's '%s' and Polymarket's '%s' pay out on the same thing.",
						nameContract(p.kalshi).Short, nameContract(p.poly).Short),
				},
				startStep{
					Text:     "Route a hypothetical order on it",
					Href:     demoPresetHref(p.rel.ID, p.kalshi.ID, string(canonical.SideBuy), string(canonical.OutcomeYes)),
					Note:     fmt.Sprintf("Pre-filled: buy YES %s on Kalshi %s, market IOC. Nothing is routed until you press Route it.", demoQty, p.kalshi.VenueMarketID),
					Demo:     true,
					Title:    "Route an order",
					Sentence: fmt.Sprintf("Buy %s YES and see which venue is cheaper. Nothing is sent anywhere.", demoQty),
				})
		}
	}
	if p, ok := findFactDidNotApply(rels, insts); ok {
		steps = append(steps, startStep{
			Text:  "See a near miss the Fed fact did not close",
			Href:  fmt.Sprintf("/pairs/%d", p.rel.ID),
			Title: "See a rejection",
			Sentence: fmt.Sprintf("Kalshi's '%s' and Polymarket's '%s' look alike, but they are not the same bet.",
				nameContract(p.kalshi).Short, nameContract(p.poly).Short),
			Note: fmt.Sprintf("Kalshi %s vs Polymarket %q: %s is the level before the next meeting only, so it does not apply here.",
				p.kalshi.VenueMarketID, p.poly.Title, factFedUpperBound),
		})
	}
	if haveCode {
		low := kalshiFedPrefix + code + "-" + demoStrikeLow
		if p, ok := findRoutableFedPair(rels, insts, low, false); ok {
			steps = append(steps, startStep{
				Text:  "Route the opposite side on the complement pair",
				Href:  demoPresetHref(p.rel.ID, p.kalshi.ID, string(canonical.SideBuy), string(canonical.OutcomeNo)),
				Title: "Trade the opposite",
				Sentence: fmt.Sprintf("Buy NO on '%s' by buying YES on its opposite, '%s'.",
					nameContract(p.kalshi).Short, nameContract(p.poly).Short),
				Note: fmt.Sprintf("Kalshi %s vs Polymarket %q (%s). Pre-filled: buy NO %s on Kalshi, market IOC.",
					p.kalshi.VenueMarketID, p.poly.Title, p.rel.Type, demoQty),
			})
		}
	}
	return steps
}

// applyRoutePreset pre-fills f from the query params target, side, outcome,
// qty, type and tif. Each value is checked on its own; an invalid or missing
// one leaves the default in place (AB-08). target is one of the pair's
// instrument IDs or a venue name that picks exactly one side. It reports
// whether any value was applied.
func applyRoutePreset(f routeFormView, q url.Values) (routeFormView, bool) {
	applied := false
	get := func(name string) string {
		v := strings.TrimSpace(q.Get(name))
		if len(v) > maxFieldLen {
			return ""
		}
		return v
	}
	if t, ok := presetTarget(f.Targets, get("target")); ok {
		f.Target, applied = t, true
	}
	if v := canonical.Side(strings.ToLower(get("side"))); v.Valid() {
		f.Side, applied = string(v), true
	}
	if v := canonical.Outcome(strings.ToLower(get("outcome"))); v.Valid() {
		f.Outcome, applied = string(v), true
	}
	if n, err := strconv.Atoi(get("qty")); err == nil && n >= minQuantity && n <= maxQuantity {
		f.Quantity, f.QuantityMode, applied = strconv.Itoa(n), "contracts", true
	}
	if v := canonical.OrderType(strings.ToLower(get("type"))); v.Valid() {
		f.Type, applied = string(v), true
	}
	if v := canonical.TimeInForce(strings.ToUpper(get("tif"))); v.Valid() {
		f.TIF, applied = string(v), true
	}
	return f, applied
}

// presetTarget resolves a target param against the pair's instrument IDs.
func presetTarget(targets []string, v string) (string, bool) {
	if v == "" {
		return "", false
	}
	for _, t := range targets {
		if t == v {
			return t, true
		}
	}
	var match []string
	for _, t := range targets {
		if string(canonical.VenueOf(t)) == strings.ToLower(v) {
			match = append(match, t)
		}
	}
	if len(match) == 1 {
		return match[0], true
	}
	return "", false
}

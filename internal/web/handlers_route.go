package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/route"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// Order input bounds (ticket M1-09). Quantity is in contracts.
const (
	minQuantity = 1
	maxQuantity = 100_000
	// maxRouteBody bounds the form body; a valid form is well under 1 KB.
	maxRouteBody = 4096
	// maxRoutePartners caps how many relationships one route call loads.
	maxRoutePartners = 1000
	// maxFieldLen bounds any single form value before it is interpreted.
	maxFieldLen = 64
)

// routeFormView is the route form as the pair page renders it: the pair,
// both targets, and the values to pre-fill.
type routeFormView struct {
	PairID       int64
	Targets      []string
	Target       string
	Side         string
	Outcome      string
	Quantity     string
	QuantityMode string // "contracts" or "usd"
	Type         string
	Limit        string
	TIF          string
}

func defaultRouteForm(rel canonical.Relationship) routeFormView {
	return routeFormView{
		PairID: rel.ID, Targets: []string{rel.AID, rel.BID}, Target: rel.AID,
		Side: string(canonical.SideBuy), Outcome: string(canonical.OutcomeYes), Quantity: "100",
		QuantityMode: "contracts",
		Type:         string(canonical.OrderMarket), TIF: string(canonical.TIFIOC),
	}
}

// routeDefaultsText states the route form's defaults in one line (M6 P6), from
// the values defaultRouteForm pre-fills, so the page and the form never drift.
func routeDefaultsText(f routeFormView) string {
	tif := "fill what you can"
	if f.TIF == string(canonical.TIFFOK) {
		tif = "all or nothing"
	}
	return fmt.Sprintf("Defaults: %s %s %s contracts, %s %s (%s), target A.",
		f.Side, strings.ToUpper(f.Outcome), f.Quantity, f.Type, f.TIF, tif)
}

// routeInput is the validated POST /route form.
type routeInput struct {
	PairID int64
	Target string
	Order  canonical.Order
}

// parseRouteForm validates every field and returns the input or the list of
// problems, worded for the person filling in the form. Enums are checked
// against the canonical types; nothing is guessed.
func parseRouteForm(get func(string) string) (routeInput, []string) {
	var (
		in   routeInput
		errs []string
	)
	// raw bounds a value's length; field also trims surrounding whitespace.
	// IDs are taken raw so they must match their format exactly (AB-08).
	raw := func(name string) string {
		v := get(name)
		if len(v) > maxFieldLen {
			errs = append(errs, name+" is too long")
			return ""
		}
		return v
	}
	field := func(name string) string { return strings.TrimSpace(raw(name)) }
	var ok bool
	if in.PairID, ok = parseID(raw("pair")); !ok {
		errs = append(errs, "pair must be a pair ID from this site")
	}
	in.Target = raw("target")
	if err := canonical.ValidateInstrumentID(in.Target); err != nil {
		errs = append(errs, "target must be one of the pair's instrument IDs")
	}
	o := canonical.Order{
		Side:    canonical.Side(strings.ToLower(field("side"))),
		Outcome: canonical.Outcome(strings.ToLower(field("outcome"))),
		Type:    canonical.OrderType(strings.ToLower(field("type"))),
		TIF:     canonical.TimeInForce(strings.ToUpper(field("tif"))),
	}
	if !o.Side.Valid() {
		errs = append(errs, "side must be buy or sell")
	}
	if !o.Outcome.Valid() {
		errs = append(errs, "outcome must be yes or no")
	}
	if !o.Type.Valid() {
		errs = append(errs, "type must be market or limit")
	}
	if !o.TIF.Valid() {
		errs = append(errs, "tif must be IOC or FOK")
	}

	// Parse quantity mode and value (issue #74: contracts or USD notional)
	qtyMode := strings.ToLower(field("quantity_mode"))
	if qtyMode != "contracts" && qtyMode != "usd" {
		qtyMode = "contracts" // default to contracts for backward compatibility
	}

	qtyStr := field("quantity")
	qty, err := decimal.NewFromString(qtyStr)
	switch {
	case err != nil:
		if qtyMode == "usd" {
			errs = append(errs, "notional must be a USD amount between $0.01 and $100,000")
		} else {
			errs = append(errs, fmt.Sprintf("quantity must be a number of contracts between %d and %d", minQuantity, maxQuantity))
		}
	case qtyMode == "usd":
		// USD notional mode: validate notional bounds (AB-08: reject zero, negative, out-of-range)
		minNotional := decimal.NewFromFloat(0.01)
		maxNotional := decimal.NewFromInt(maxQuantity)
		if qty.LessThan(minNotional) || qty.GreaterThan(maxNotional) {
			errs = append(errs, "notional must be between $0.01 and $100,000")
		} else {
			o.Notional = &qty
		}
	default:
		// Contracts mode: validate contracts bounds (AB-08: reject zero, negative, out-of-range)
		if qty.LessThan(decimal.NewFromInt(minQuantity)) || qty.GreaterThan(decimal.NewFromInt(maxQuantity)) {
			errs = append(errs, fmt.Sprintf("quantity must be between %d and %d contracts", minQuantity, maxQuantity))
		} else {
			o.Quantity = &qty
		}
	}
	limit := field("limit")
	switch {
	case o.Type == canonical.OrderLimit && limit == "":
		errs = append(errs, "a limit order needs a limit price between 0 and 1")
	case o.Type == canonical.OrderMarket && limit != "":
		errs = append(errs, "a market order takes no limit price; clear it or choose a limit order")
	case limit != "":
		p, err := decimal.NewFromString(limit)
		if err != nil || canonical.ValidatePrice(p) != nil {
			errs = append(errs, "limit price must be a number between 0 and 1")
		} else {
			o.LimitPrice = &p
		}
	}
	if len(errs) == 0 {
		if err := o.Validate(); err != nil {
			errs = append(errs, "order is not valid: "+err.Error())
		}
	}
	in.Order = o
	return in, errs
}

// routePage is the data for route.html and the route_result fragment.
type routePage struct {
	page
	PairID        int64
	Errors        []string
	DecisionID    int64
	Decision      *decisionView
	RenderContext string // "route" or "decision" to control trace step open/closed state
}

// decisionView is a Decision prepared for the template.
type decisionView struct {
	RoundID          int64
	TargetID         string
	DecidedAt        time.Time // Raw UTC time, formatted with formatTime in templates (M3-08)
	Snapshot         time.Time // Raw UTC time, formatted with formatTime in templates (M3-08)
	OrderLine        string
	Chosen           []legView
	Alternatives     []alternativeView
	Exclusions       []exclusionView
	Warnings         []string
	Explanation      string
	Partners         int
	OutcomePhrase    string
	OutcomeChipClass string
	OutcomeSummary   string // One-line summary for the strip
	LeadSentence     string // M5-08: Plain summary in design.md § 4 format
	TotalFilled      decimal.Decimal
	TotalRequested   decimal.Decimal
	AverageAllIn     decimal.Decimal
	BasisMultiplier  decimal.Decimal   // For display in template
	InstrumentTitles map[string]string // instrument ID -> short title
	// UI-13 additions for v2 trace
	TotalCost        decimal.Decimal
	TotalFees        decimal.Decimal
	TotalBasis       decimal.Decimal
	SlippageTotalBps decimal.Decimal     // Weighted average slippage vs quoted in bps
	MaxQuoteAge      time.Duration       // Oldest input quote age
	MaxQuoteAgeBadge Badge               // Freshness badge for the oldest input
	Inputs           []inputSnapshotView // Input snapshot from stored decision
	KeptCount        int                 // How many candidates were kept (eligible)
	CandidateCount   int                 // Total candidates considered
	InputsRecorded   bool                // True if inputs exist (UI-25), false for old decisions
	UI25CutoffDate   string              // The date before which inputs were not stored
	UnfilledPct      decimal.Decimal     // Unfilled percentage for partial fills (split bar)
	UnfilledX        decimal.Decimal     // X position of unfilled segment (split bar)
	// Issue #74: Notional conversion
	UsedNotional      bool            // True if order was specified as notional
	OriginalNotional  decimal.Decimal // Original USD notional amount
	ConversionTouch   decimal.Decimal // Touch price used to convert notional to contracts
	ConvertedQuantity decimal.Decimal // Contracts after conversion (rounded down)
}

// legView extends Leg with fields for the v2 cost report (UI-13).
type legView struct {
	canonical.Leg
	Quoted      decimal.Decimal // Best price at snapshot (for slippage calculation)
	SlippageBps decimal.Decimal // (VWAP - Quoted) * 10000 in bps
	SharePct    decimal.Decimal // This leg's share of total filled (0-100)
	X           decimal.Decimal // Split bar x position (0-100)
	BasisScore  decimal.Decimal // Basis risk score for this leg
	Levels      int             // Book levels used for VWAP
}

// alternativeView extends Alternative with the gap to the chosen all-in.
type alternativeView struct {
	canonical.Alternative
	GapToChosen decimal.Decimal
	GapBps      decimal.Decimal // Gap in basis points
	BarWidthPct decimal.Decimal // Width for proportional SVG bar (0-100%)
}

// exclusionView extends Exclusion with a human-readable reason phrase.
type exclusionView struct {
	canonical.Exclusion
	ReasonPhrase string
}

// inputSnapshotView represents one candidate's input snapshot at decision time (UI-13, step 1).
type inputSnapshotView struct {
	Venue        canonical.Venue
	InstrumentID string
	TypeBadge    Badge
	BestAsk      decimal.Decimal
	Depth        int // Sum of ask levels (contracts available)
	Age          time.Duration
	FreshState   FreshState
	FetchedAt    time.Time // Raw UTC time for formatTime
}

func newDecisionView(d canonical.Decision, partners int, snapshot time.Time, instrumentTitles map[string]string, basisMult decimal.Decimal, staleAfter time.Duration) *decisionView {
	chosen := len(d.Chosen) > 0
	partial := chosen && d.Order.Quantity != nil && totalFilled(d.Chosen).LessThan(*d.Order.Quantity)
	isComplement := hasComplement(d.Chosen)
	venueCount := countVenues(d.Chosen)

	phrase, chipClass := OutcomePhrase(chosen, partial, isComplement, venueCount)

	// For complement routes, include venue and outcome per design.md § 4
	// Only override when fill is complete AND every leg is a complement (not mixed, not partial)
	if isComplement && !partial && len(d.Chosen) > 0 && allComplements(d.Chosen) {
		for _, leg := range d.Chosen {
			if leg.Route == "complement" {
				phrase = fmt.Sprintf("Routed as %s on %s (complement)", strings.ToUpper(string(leg.Outcome)), leg.Venue)
				break
			}
		}
	}

	totalReq := decimal.Zero
	if d.Order.Quantity != nil {
		totalReq = *d.Order.Quantity
	}
	totalFill := totalFilled(d.Chosen)
	avgAllIn := averageAllIn(d.Chosen)

	// Build outcome summary for the strip
	summary := buildOutcomeSummary(d, totalFill, totalReq, avgAllIn)

	// Build input snapshot map for quoted prices (UI-13)
	inputMap := buildInputMap(d.Inputs)

	// Map chosen legs with v2 fields (UI-13)
	chosenViews := make([]legView, len(d.Chosen))
	var totalCost, totalFees, totalBasis decimal.Decimal
	var weightedSlippageBps decimal.Decimal
	for i, leg := range d.Chosen {
		lv := legView{Leg: leg}

		// Find quoted price from inputs
		if input, ok := inputMap[leg.InstrumentID]; ok {
			lv.Quoted = input.BestAsk
			// Slippage = (VWAP - Quoted) * 10000 in bps
			slippage := leg.VWAP.Sub(lv.Quoted).Mul(decimal.NewFromInt(10000))
			lv.SlippageBps = slippage
			// Weight by this leg's quantity
			weightedSlippageBps = weightedSlippageBps.Add(slippage.Mul(leg.Quantity))
			lv.Levels = len(input.AskDepth)
		}

		// Share percentage for split bar (as percentage of requested, not filled)
		divisor := totalReq
		if divisor.IsZero() {
			divisor = totalFill
		}
		if !divisor.IsZero() {
			lv.SharePct = leg.Quantity.Div(divisor).Mul(decimal.NewFromInt(100))
		}

		// Basis score (from the leg's basis penalty divided by the multiplier)
		if !basisMult.IsZero() {
			lv.BasisScore = leg.BasisPenaltyPerContract.Div(basisMult)
		}

		// Accumulate totals
		totalCost = totalCost.Add(leg.VWAP.Mul(leg.Quantity))
		totalFees = totalFees.Add(leg.FeePerContract.Mul(leg.Quantity))
		totalBasis = totalBasis.Add(leg.BasisPenaltyPerContract.Mul(leg.Quantity))

		chosenViews[i] = lv
	}

	// Compute split bar X positions (cumulative)
	var cumulative decimal.Decimal
	for i := range chosenViews {
		chosenViews[i].X = cumulative
		cumulative = cumulative.Add(chosenViews[i].SharePct)
	}

	// Calculate unfilled segment for partial fills (UI-13 AC item 4)
	unfilledPct := decimal.Zero
	unfilledX := cumulative
	if chosen && !totalReq.IsZero() && totalFill.LessThan(totalReq) {
		unfilledPct = totalReq.Sub(totalFill).Div(totalReq).Mul(decimal.NewFromInt(100))
	}

	// Weighted average slippage
	slippageTotalBps := decimal.Zero
	if !totalFill.IsZero() {
		slippageTotalBps = weightedSlippageBps.Div(totalFill)
	}

	// Map alternatives with gap, gap in bps, and bar width (UI-13)
	chosenAllIn := avgAllIn
	alts := make([]alternativeView, len(d.Alternatives))
	for i, alt := range d.Alternatives {
		gap := alt.AllInPerContract.Sub(chosenAllIn)
		gapBps := decimal.Zero
		if !chosenAllIn.IsZero() {
			gapBps = gap.Div(chosenAllIn).Mul(decimal.NewFromInt(10000))
		}
		// Bar width: chosen (rank 1) is 100%, others scaled relative to it
		barWidth := decimal.NewFromInt(100)
		if alt.Rank != 1 && !chosenAllIn.IsZero() {
			barWidth = alt.AllInPerContract.Div(chosenAllIn).Mul(decimal.NewFromInt(100))
		}
		alts[i] = alternativeView{Alternative: alt, GapToChosen: gap, GapBps: gapBps, BarWidthPct: barWidth}
	}

	// Map exclusions with reason phrases
	excls := make([]exclusionView, len(d.Exclusions))
	for i, excl := range d.Exclusions {
		excls[i] = exclusionView{Exclusion: excl, ReasonPhrase: exclusionReasonPhrase(excl.Reason)}
	}

	// Build input snapshots (UI-13, step 1)
	inputViews, maxAge, maxAgeBadge := buildInputSnapshots(d, snapshot, staleAfter)
	inputsRecorded := len(d.Inputs) > 0

	// Kept vs total candidates (UI-13, step 2)
	// Alternatives already includes the chosen route (rank 1), so don't double-count
	keptCount := len(d.Alternatives)
	candidateCount := keptCount + len(d.Exclusions)

	// Issue #74: Extract notional conversion details from the decision
	usedNotional := d.Order.Notional != nil
	var originalNotional, conversionTouch, convertedQuantity decimal.Decimal
	if usedNotional {
		originalNotional = *d.Order.Notional
		// The router stores a warning when converting notional to contracts
		// Format: "quantity X derived from notional $Y at the Z touch"
		// First try to extract from the warning
		for _, w := range d.Warnings {
			if strings.Contains(w, "derived from notional") {
				// Try to parse: find numbers after key words
				// Split and scan for the pattern
				parts := strings.Fields(w)
				for i, part := range parts {
					if part == "quantity" && i+1 < len(parts) {
						if qty, err := decimal.NewFromString(parts[i+1]); err == nil {
							convertedQuantity = qty
						}
					}
					if part == "touch" && i > 0 {
						// Touch value is before the word "touch"
						if touch, err := decimal.NewFromString(parts[i-1]); err == nil {
							conversionTouch = touch
						}
					}
				}
				break
			}
		}
		// If warning parsing didn't work, compute from inputs
		if conversionTouch.IsZero() && len(d.Inputs) > 0 {
			// The router uses the first available touch (UI-25 inputs are in evaluation order)
			for _, input := range d.Inputs {
				if input.BestAsk.IsPositive() {
					conversionTouch = input.BestAsk
					convertedQuantity = originalNotional.Div(conversionTouch).Floor()
					break
				}
			}
		}
	}

	// M5-08: Build lead sentence per design.md § 4 line 78
	leadSentence := buildLeadSentence(d, totalFill, totalReq, avgAllIn, alts)

	return &decisionView{
		RoundID: d.RoundID, TargetID: d.TargetID, DecidedAt: d.DecidedAt,
		Snapshot: snapshot, OrderLine: orderLine(d.Order),
		Chosen: chosenViews, Alternatives: alts, Exclusions: excls, Warnings: d.Warnings,
		Explanation: d.Explanation, Partners: partners,
		OutcomePhrase: phrase, OutcomeChipClass: chipClass, OutcomeSummary: summary,
		LeadSentence: leadSentence, // M5-08
		TotalFilled:  totalFill, TotalRequested: totalReq, AverageAllIn: avgAllIn,
		BasisMultiplier:  basisMult,
		InstrumentTitles: instrumentTitles,
		// UI-13 fields
		TotalCost:        totalCost,
		TotalFees:        totalFees,
		TotalBasis:       totalBasis,
		SlippageTotalBps: slippageTotalBps,
		MaxQuoteAge:      maxAge,
		MaxQuoteAgeBadge: maxAgeBadge,
		Inputs:           inputViews,
		KeptCount:        keptCount,
		CandidateCount:   candidateCount,
		InputsRecorded:   inputsRecorded,
		UI25CutoffDate:   "2026-10-08", // UI-25 was merged on this date
		UnfilledPct:      unfilledPct,
		UnfilledX:        unfilledX,
		// Issue #74: Notional conversion
		UsedNotional:      usedNotional,
		OriginalNotional:  originalNotional,
		ConversionTouch:   conversionTouch,
		ConvertedQuantity: convertedQuantity,
	}
}

// exclusionReasonPhrase maps a reason code to a human-readable phrase per design.md § 4.
func exclusionReasonPhrase(reason string) string {
	switch reason {
	case "stale":
		return "stale"
	case "skew":
		return "skew"
	case "no_book":
		return "no book"
	case "insufficient_depth":
		return "too shallow"
	case "not_routable":
		return "not routable"
	case "low_confidence":
		return "low confidence"
	case "limit_not_reachable":
		return "limit not reachable"
	default:
		return reason // fallback: show the code as-is
	}
}

// buildOutcomeSummary creates the one-line summary for the outcome strip.
func buildOutcomeSummary(d canonical.Decision, totalFill, totalReq, avgAllIn decimal.Decimal) string {
	if len(d.Chosen) == 0 {
		// Not routed: show reason tally
		tally := tallyExclusionReasons(d.Exclusions)
		if tally == "" {
			return "every candidate was excluded"
		}
		return tally
	}
	// Routed: show filled quantity and average all-in
	var fillMsg string
	if d.Order.Quantity != nil && !totalReq.IsZero() {
		if totalFill.Equal(totalReq) {
			fillMsg = fmt.Sprintf("filled %s contracts", totalFill.String())
		} else {
			fillMsg = fmt.Sprintf("filled %s of %s", totalFill.String(), totalReq.String())
		}
	} else {
		// Notional order or zero quantity: just show contracts filled
		fillMsg = fmt.Sprintf("filled %s contracts", totalFill.String())
	}
	if len(d.Chosen) == 1 {
		leg := d.Chosen[0]
		// Use leg outcome for complements (not order outcome)
		outcome := strings.ToUpper(string(leg.Outcome))
		return fmt.Sprintf("%s %s %s on %s at %s all-in per contract, %s",
			d.Order.Side, totalFill.String(), outcome,
			leg.Venue, avgAllIn.StringFixed(4), fillMsg)
	}
	// Multi-leg: just show total and average
	return fmt.Sprintf("%s, average all-in %s per contract", fillMsg, avgAllIn.StringFixed(4))
}

// tallyExclusionReasons returns a one-line summary of exclusion reasons, sorted for determinism.
func tallyExclusionReasons(exclusions []canonical.Exclusion) string {
	counts := make(map[string]int)
	for _, excl := range exclusions {
		phrase := exclusionReasonPhrase(excl.Reason)
		counts[phrase]++
	}
	if len(counts) == 0 {
		return ""
	}
	// Sort phrases for deterministic output
	phrases := make([]string, 0, len(counts))
	for phrase := range counts {
		phrases = append(phrases, phrase)
	}
	sort.Strings(phrases)

	var parts []string
	for _, phrase := range phrases {
		count := counts[phrase]
		if count == 1 {
			parts = append(parts, "1 "+phrase)
		} else {
			parts = append(parts, fmt.Sprintf("%d %s", count, phrase))
		}
	}
	return strings.Join(parts, ", ")
}

// buildLeadSentence creates the plain summary sentence per design.md § 4 line 78 (M5-08).
func buildLeadSentence(d canonical.Decision, totalFill, totalReq, avgAllIn decimal.Decimal, alternatives []alternativeView) string {
	chosen := len(d.Chosen) > 0
	partial := chosen && d.Order.Quantity != nil && !totalReq.IsZero() && totalFill.LessThan(totalReq)

	if !chosen {
		// Not routed: "Not routed: <reason in words>"
		tally := tallyExclusionReasons(d.Exclusions)
		if tally == "" {
			return "Not routed: every candidate was excluded"
		}
		return fmt.Sprintf("Not routed: %s", tally)
	}

	// Partial fill
	if partial {
		return fmt.Sprintf("Partially routed: %s of %s contracts", totalFill.String(), totalReq.String())
	}

	// Single venue
	if len(d.Chosen) == 1 {
		leg := d.Chosen[0]
		isComplement := leg.Route == "complement"

		// Complement route
		if isComplement {
			// "Routed as NO on polymarket (complement of the target)"
			return fmt.Sprintf("Routed as %s on %s (complement of the target)", strings.ToUpper(string(leg.Outcome)), leg.Venue)
		}

		// Direct route: "Routed: buy 100 YES on kalshi at 0.5312 all-in"
		// With gap: "Routed: buy 100 YES on kalshi at 0.5312 all-in, 1.2 ¢ cheaper than polymarket after fees"
		qtyStr := totalFill.String()
		outcome := strings.ToUpper(string(d.Order.Outcome))
		base := fmt.Sprintf("Routed: %s %s %s on %s at %s all-in",
			d.Order.Side, qtyStr, outcome, leg.Venue, avgAllIn.StringFixed(4))

		// Add gap to next best alternative if there is one
		if len(alternatives) > 1 {
			// Find the rank 2 alternative (rank 1 is the chosen route)
			var nextBest *alternativeView
			for i := range alternatives {
				if alternatives[i].Rank == 2 {
					nextBest = &alternatives[i]
					break
				}
			}
			if nextBest != nil && len(nextBest.Legs) > 0 {
				// Gap = alternative - chosen
				// Positive gap: alternative is more expensive, chosen is cheaper (favorable)
				// Negative gap: alternative is cheaper, chosen is more expensive (should not happen for rank 1)
				gap := nextBest.GapToChosen
				var gapPhrase string
				if gap.IsPositive() {
					// Favorable: chosen is cheaper than alternative
					// Convert to cents: multiply by 100
					cents := gap.Mul(decimal.NewFromInt(100)).StringFixed(2)
					gapPhrase = fmt.Sprintf("%s ¢ cheaper than %s after fees",
						cents, nextBest.Legs[0].Venue)
				} else if gap.IsNegative() {
					// Unfavorable (should not happen for rank 1, but be defensive)
					cents := gap.Abs().Mul(decimal.NewFromInt(100)).StringFixed(2)
					gapPhrase = fmt.Sprintf("(%s ¢) more expensive than %s after fees",
						cents, nextBest.Legs[0].Venue)
				}
				if gapPhrase != "" {
					base = base + ", " + gapPhrase
				}
			}
		}
		return base
	}

	// Multi-venue split: "Routed across 2 venues: 60 + 40 contracts"
	venueCount := countVenues(d.Chosen)
	var contractParts []string
	for _, leg := range d.Chosen {
		contractParts = append(contractParts, leg.Quantity.String())
	}
	return fmt.Sprintf("Routed across %d venues: %s contracts", venueCount, strings.Join(contractParts, " + "))
}

// hasComplement reports whether any chosen leg is a complement route.
func hasComplement(legs []canonical.Leg) bool {
	for _, leg := range legs {
		if leg.Route == "complement" {
			return true
		}
	}
	return false
}

// allComplements reports whether every chosen leg is a complement route.
func allComplements(legs []canonical.Leg) bool {
	if len(legs) == 0 {
		return false
	}
	for _, leg := range legs {
		if leg.Route != "complement" {
			return false
		}
	}
	return true
}

// countVenues returns the number of distinct venues in the chosen legs.
func countVenues(legs []canonical.Leg) int {
	venues := make(map[canonical.Venue]bool)
	for _, leg := range legs {
		venues[leg.Venue] = true
	}
	return len(venues)
}

// totalFilled sums the quantity filled across all chosen legs.
func totalFilled(legs []canonical.Leg) decimal.Decimal {
	total := decimal.Zero
	for _, leg := range legs {
		total = total.Add(leg.Quantity)
	}
	return total
}

// averageAllIn computes the size-weighted average all-in cost per contract.
func averageAllIn(legs []canonical.Leg) decimal.Decimal {
	if len(legs) == 0 {
		return decimal.Zero
	}
	totalCost := decimal.Zero
	totalQty := decimal.Zero
	for _, leg := range legs {
		totalCost = totalCost.Add(leg.AllInPerContract.Mul(leg.Quantity))
		totalQty = totalQty.Add(leg.Quantity)
	}
	if totalQty.IsZero() {
		return decimal.Zero
	}
	return totalCost.Div(totalQty)
}

// orderLine is the order in one line, FIX-style (FR-27).
func orderLine(o canonical.Order) string {
	size := "?"
	switch {
	case o.Quantity != nil:
		size = o.Quantity.String() + " contracts"
	case o.Notional != nil:
		size = "$" + o.Notional.String() + " notional"
	}
	line := fmt.Sprintf("%s %s %s, %s %s", o.Side, size, strings.ToUpper(string(o.Outcome)), o.Type, o.TIF)
	if o.LimitPrice != nil {
		line += " at " + o.LimitPrice.StringFixed(4)
	}
	return line
}

// isHTMX reports whether the request came from htmx and wants a fragment.
func isHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

// handleRoute answers POST /route: validate the form, run the router against
// the pair's round, store the decision, and render the trace (US-03, FR-12).
// It never places an order anywhere; the only write is the decision row and
// its decision-log entry (AB-14).
func (s *Server) handleRoute(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRouteBody)
	if err := r.ParseForm(); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			s.routeFailure(w, r, http.StatusRequestEntityTooLarge, 0, "the form is too large")
			return
		}
		s.routeFailure(w, r, http.StatusBadRequest, 0, "the form could not be read")
		return
	}
	in, problems := parseRouteForm(r.PostForm.Get)
	if len(problems) > 0 {
		s.routeFailure(w, r, http.StatusBadRequest, in.PairID, problems...)
		return
	}
	if s.deps.Store == nil {
		s.routeFailure(w, r, http.StatusServiceUnavailable, in.PairID, "the database is not configured on this server")
		return
	}
	st := s.deps.Store
	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	// Get current round from context (pinned or latest) - NFR-04
	currentRound := roundFromContext(r.Context())

	rel, err := st.GetRelationship(ctx, in.PairID)
	switch {
	case isNotFound(err):
		s.routeFailure(w, r, http.StatusNotFound, in.PairID, "no such pair")
		return
	case err != nil:
		s.log.Error("loading relationship for route", "err", err)
		s.routeFailure(w, r, http.StatusServiceUnavailable, in.PairID, "the database is unavailable; try again shortly")
		return
	}

	// Determine which round to use for routing and banner (M3-07, NFR-04)
	var round store.Round
	if currentRound.Pinned {
		// NFR-04: when a round is pinned, reject pairs from other rounds
		if rel.RoundID != currentRound.ID {
			s.routeFailure(w, r, http.StatusNotFound, in.PairID, "this pair belongs to a different round")
			return
		}
		round = currentRound
	} else {
		// Unpinned or no current round: use the pair's own round (allows routing older pairs from /log)
		round, err = st.GetRound(ctx, rel.RoundID)
		if err != nil {
			s.log.Error("loading round for route", "err", err)
			s.routeFailure(w, r, http.StatusServiceUnavailable, in.PairID, "the database is unavailable; try again shortly")
			return
		}
	}

	if in.Target != rel.AID && in.Target != rel.BID {
		s.routeFailure(w, r, http.StatusBadRequest, in.PairID, "target must be one of the pair's instrument IDs")
		return
	}
	target, partners, err := s.loadRouteInputs(ctx, st, round.ID, in.Target)
	switch {
	case isNotFound(err):
		s.routeFailure(w, r, http.StatusNotFound, in.PairID, "the target instrument is no longer in the store")
		return
	case err != nil:
		s.log.Error("loading route inputs", "err", err)
		s.routeFailure(w, r, http.StatusServiceUnavailable, in.PairID, "the database is unavailable; try again shortly")
		return
	}
	cfg := route.Config{
		RoundID:                round.ID,
		MinRouteConfidence:     s.cfg.MinRouteConfidence,
		BasisPenaltyMultiplier: s.cfg.BasisPenaltyMult,
		Eligibility: route.EligibilityConfig{
			Snapshot:     quoteSnapshot(ctx, st, round),
			StaleAfter:   s.cfg.StaleAfter,
			SkewWarn:     s.cfg.SkewWarn,
			SkewMax:      s.cfg.SkewMax,
			MinBookDepth: s.cfg.MinBookDepth,
		},
	}
	dec, err := route.Route(in.Order, target, partners, cfg, rel.Type, s.now())
	if err != nil {
		// Route only errors on invalid input, which the form checks already
		// caught; report it as a form problem rather than a server fault.
		s.routeFailure(w, r, http.StatusBadRequest, in.PairID, "order rejected: "+err.Error())
		return
	}
	id, err := st.InsertRoutingDecision(ctx, dec)
	if err != nil {
		s.log.Error("storing routing decision", "err", err)
		s.routeFailure(w, r, http.StatusServiceUnavailable, in.PairID, "the decision could not be stored; try again shortly")
		return
	}
	if _, err := st.AppendLog(ctx, store.LogRouting, strconv.FormatInt(id, 10), routeLogSummary(dec), dec, dec.DecidedAt); err != nil {
		s.log.Error("logging routing decision", "decision_id", id, "err", err)
		s.routeFailure(w, r, http.StatusServiceUnavailable, in.PairID, "the decision could not be logged; try again shortly")
		return
	}
	s.log.Info("routing decision stored", "decision_id", id, "round_id", dec.RoundID, "target", dec.TargetID,
		"chosen", len(dec.Chosen), "alternatives", len(dec.Alternatives), "exclusions", len(dec.Exclusions))

	tz := timezoneFromRequest(r)
	p := s.routePageHeader(in.PairID, round, tz)
	p.StaleWarning = s.staleRoundWarning(round)

	// Build instrument titles map from candidates
	titles := make(map[string]string)
	titles[target.Instrument.ID] = target.Instrument.Title
	for _, c := range partners {
		titles[c.Instrument.ID] = c.Instrument.Title
	}

	data := routePage{
		page:          p,
		PairID:        in.PairID,
		DecisionID:    id,
		Decision:      newDecisionView(dec, len(partners), decisionSnapshot(dec, cfg.Eligibility.Snapshot), titles, s.cfg.BasisPenaltyMult, s.cfg.StaleAfter),
		RenderContext: "route",
	}
	s.renderRoute(w, r, http.StatusOK, data)
}

func (s *Server) routePageHeader(pairID int64, round store.Round, tz string) page {
	return s.pageBase(round, "Routing trace", fmt.Sprintf("Where would this order fill best, and why? (pair %d)", pairID), tz, "route")
}

// routeFailure renders the problems at the given status: as a fragment for
// htmx (which swaps 400 responses in, see the htmx-config meta) or as a
// full page otherwise.
func (s *Server) routeFailure(w http.ResponseWriter, r *http.Request, status int, pairID int64, problems ...string) {
	round := roundFromContext(r.Context())
	tz := timezoneFromRequest(r)
	s.renderRoute(w, r, status, routePage{page: s.routePageHeader(pairID, round, tz), PairID: pairID, Errors: problems})
}

func (s *Server) renderRoute(w http.ResponseWriter, r *http.Request, status int, data routePage) {
	if isHTMX(r) {
		s.renderTemplate(w, status, "route", "route_result", data)
		return
	}
	s.render(w, status, "route", data)
}

// loadRouteInputs reads the target with its quote and every instrument
// related to it in the round, each with its quote and relationship, the way
// `equinox route` does. A partner whose instrument is missing is logged and
// skipped; a missing quote becomes an empty, flagged book.
func (s *Server) loadRouteInputs(ctx context.Context, st *store.Store, roundID int64, targetID string) (canonical.Candidate, []canonical.Candidate, error) {
	now := s.now()
	target, err := loadCandidate(ctx, st, roundID, targetID, now)
	if err != nil {
		return canonical.Candidate{}, nil, err
	}
	rels, err := st.ListRelationships(ctx, store.RelationshipFilter{RoundID: roundID, InstrumentID: targetID, Limit: maxRoutePartners})
	if err != nil {
		return canonical.Candidate{}, nil, err
	}
	var partners []canonical.Candidate
	for i := range rels {
		rel := rels[i]
		otherID := rel.BID
		if otherID == targetID {
			otherID = rel.AID
		}
		c, err := loadCandidate(ctx, st, roundID, otherID, now)
		if err != nil {
			if !isNotFound(err) {
				return canonical.Candidate{}, nil, err
			}
			s.log.Warn("skipping route partner", "instrument", otherID, "relationship_id", rel.ID, "err", err.Error())
			continue
		}
		c.Relationship = &rel
		partners = append(partners, c)
	}
	return target, partners, nil
}

func loadCandidate(ctx context.Context, st *store.Store, roundID int64, id string, now time.Time) (canonical.Candidate, error) {
	rec, err := st.GetInstrument(ctx, id)
	if err != nil {
		return canonical.Candidate{}, err
	}
	q, err := quoteFor(ctx, st, roundID, id, now)
	if err != nil {
		return canonical.Candidate{}, err
	}
	return canonical.Candidate{Instrument: rec.Instrument, Quote: q}, nil
}

// routeLogSummary is the one-line decision-log entry for a routing decision
// (the same wording as `equinox route`).
func routeLogSummary(dec canonical.Decision) string {
	size := "?"
	switch {
	case dec.Order.Quantity != nil:
		size = dec.Order.Quantity.String() + " contracts"
	case dec.Order.Notional != nil:
		size = "$" + dec.Order.Notional.String()
	}
	if len(dec.Chosen) == 0 {
		return fmt.Sprintf("route %s %s %s on %s: no venue can fill (%d excluded)", dec.Order.Side, size, dec.Order.Outcome, dec.TargetID, len(dec.Exclusions))
	}
	l := dec.Chosen[0]
	return fmt.Sprintf("route %s %s %s on %s: %s %s at %s per contract", dec.Order.Side, size, dec.Order.Outcome, dec.TargetID,
		l.Venue, l.InstrumentID, l.AllInPerContract.StringFixed(4))
}

// loadDecisionInstrumentTitles loads the titles for all instruments mentioned in a decision
// using batch GetInstruments to avoid N+1 queries. If an instrument is not found, its title is empty.
func (s *Server) loadDecisionInstrumentTitles(ctx context.Context, st *store.Store, d canonical.Decision) map[string]string {
	ids := collectInstrumentIDs(d)
	if len(ids) == 0 {
		return nil
	}
	idList := make([]string, 0, len(ids))
	for id := range ids {
		idList = append(idList, id)
	}
	recs, err := st.GetInstruments(ctx, idList)
	if err != nil {
		s.log.Warn("loading instrument titles for decision", "count", len(idList), "err", err.Error())
		return nil
	}
	titles := make(map[string]string, len(recs))
	for _, rec := range recs {
		titles[rec.ID] = rec.Title
	}
	return titles
}

// collectInstrumentIDs returns all instrument IDs mentioned in a decision.
func collectInstrumentIDs(d canonical.Decision) map[string]bool {
	ids := make(map[string]bool)
	ids[d.TargetID] = true
	for _, leg := range d.Chosen {
		ids[leg.InstrumentID] = true
	}
	for _, alt := range d.Alternatives {
		for _, leg := range alt.Legs {
			ids[leg.InstrumentID] = true
		}
	}
	for _, ex := range d.Exclusions {
		ids[ex.InstrumentID] = true
	}
	return ids
}

// buildInputMap creates a map from instrument ID to DecisionInput for quick lookup (UI-13).
func buildInputMap(inputs []canonical.DecisionInput) map[string]canonical.DecisionInput {
	m := make(map[string]canonical.DecisionInput, len(inputs))
	for _, input := range inputs {
		m[input.InstrumentID] = input
	}
	return m
}

// buildInputSnapshots creates the input snapshot views for the trace step 1 (UI-13).
// Returns the snapshot views, the oldest quote age, and its freshness badge.
// Ages and states use the one freshness rule (quoteAgeAt, M6 P3) against
// snapshot, so the trace agrees with the matches list and the pair page.
func buildInputSnapshots(d canonical.Decision, snapshot time.Time, staleAfter time.Duration) ([]inputSnapshotView, time.Duration, Badge) {
	if len(d.Inputs) == 0 {
		return nil, 0, Badge{}
	}

	views := make([]inputSnapshotView, len(d.Inputs))
	var maxAge time.Duration
	oldestState := FreshStateUnknown

	for i, input := range d.Inputs {
		age, freshState := quoteAgeAt(snapshot, input.FetchedAt, staleAfter)
		maxAge = max(maxAge, age)
		oldestState = worseFresh(oldestState, freshState)

		// Compute depth (sum of ask levels)
		depth := 0
		for _, level := range input.AskDepth {
			depth += int(level.Size.IntPart())
		}

		views[i] = inputSnapshotView{
			Venue:        input.Venue,
			InstrumentID: input.InstrumentID,
			TypeBadge:    TypeBadge(input.RelationshipType),
			BestAsk:      input.BestAsk,
			Depth:        depth,
			Age:          age,
			FreshState:   freshState,
			FetchedAt:    input.FetchedAt,
		}
	}

	return views, maxAge, FreshBadgeAged(oldestState, maxAge, staleAfter, FreshKindQuote)
}

package web

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// The decision presenter (ui/plain-language) turns a stored Decision into the
// three layers the routing result shows: a plain answer, a trader's
// comparison table, and the audit layer (the existing trace, unchanged). All
// copy is built from structured fields with fixed templates; it never edits
// the router's Explanation, which the audit layer shows verbatim. No LLM is
// used (NFR-11).

var (
	staleDetailRe   = regexp.MustCompile(`^fetched (\d+) s ago$`)
	skewDetailRe    = regexp.MustCompile(`^(\d+) s behind$`)
	lowConfDetailRe = regexp.MustCompile(`^confidence (\S+) is below the (\S+) floor$`)
	limitDetailRe   = regexp.MustCompile(`^(\S+) of (\S+) contracts at or better than limit (\S+)$`)
)

// decisionContext is what the presenter needs besides the Decision itself.
type decisionContext struct {
	// Instruments are the decision's instruments by ID, for plain names.
	Instruments map[string]canonical.Instrument
	// TargetQuote is the target's book, for "the market prices it at about
	// 99%"; nil when it could not be loaded.
	TargetQuote *canonical.Quote
	// Suggestion is the opposite-order hint for a decision with no fill.
	Suggestion *routeSuggestion
}

// plainDecision is the answer layer and the trader layer of one decision.
type plainDecision struct {
	Badge     string // "Routed to Polymarket", "Split across 2 venues", "No trade"
	BadgeKind string // "ok", "warn" or "none", for the badge colour
	Answer    string // one to five short sentences, no codes or IDs
	NextStep  *nextStep
	OrderText string // "buy 100 YES at market, fill what you can"

	CompareTitle string // "Where it costs least", or "Where it pays most" for a sell
	CompareQty   string // column header for the order total, "100 contracts"
	Compare      []compareRow
	CompareSkip  string // the grey line shown instead of an empty table
	Bars         *barChart
	WhyNot       []whyNotGroup
}

// nextStep is the "What to do next" line: a button-styled link and a note.
type nextStep struct {
	Label  string // "Try the other side: buy NO on Kalshi at 1¢"
	Detail string // "8,912 on offer"
	Href   string // "" when no pair is known; the label is then plain text
}

// Text is the whole next step as one sentence, for tests and the log.
func (n nextStep) Text() string {
	if n.Detail == "" {
		return n.Label + "."
	}
	return n.Label + " (" + n.Detail + ")."
}

// compareRow is one priced candidate in the comparison table.
type compareRow struct {
	Winner     bool
	Excluded   bool // priced but ruled out (a sure loss): muted, red verdict
	Where      string
	Contract   string
	Title      string // the venue's own title, for the title attribute
	Opposite   bool   // filled through the opposite contract
	Price      string
	Fee        string
	Safety     string
	Total      string // all-in per contract, in cents
	OrderTotal string // all-in for the order, in dollars
	Verdict    string // "Best", "+0.27¢ more", or a translated exclusion
}

// whyNotGroup is one excluded group in "Why not the others?".
type whyNotGroup struct {
	Summary string
	Items   []whyNotItem
}

// whyNotItem is one excluded contract, named in words.
type whyNotItem struct {
	Name   string // "Polymarket · Fed cuts 25 bps in October"
	Reason string // "only covers some of your outcomes"
}

// presentDecision builds the plain view of d.
func presentDecision(d canonical.Decision, dc decisionContext) *plainDecision {
	p := &plainDecision{OrderText: plainOrder(d.Order), CompareTitle: "Where it costs least"}
	if d.Order.Side == canonical.SideSell {
		p.CompareTitle = "Where it pays most"
	}
	qty := orderQty(d)
	p.CompareQty = countText(qty) + " " + plural(int(qty.IntPart()), "contract", "contracts")
	p.Compare = compareRows(d, dc, qty)
	if len(p.Compare) == 0 {
		p.CompareSkip = "Skipped: no venue was eligible, so nothing was priced."
	}
	p.Bars = buildCostBars(d)
	p.WhyNot = whyNotGroups(d, dc)
	switch {
	case len(d.Chosen) == 0:
		p.Badge, p.BadgeKind = "No trade", "none"
		p.Answer = noTradeAnswer(d, dc)
		p.NextStep = nextStepFor(d, dc.Suggestion)
	case countVenues(d.Chosen) > 1:
		p.Badge, p.BadgeKind = fmt.Sprintf("Split across %d venues", countVenues(d.Chosen)), "ok"
		p.Answer = splitAnswer(d)
	case isPartial(d):
		p.Badge, p.BadgeKind = "Partly filled on "+venueName(d.Chosen[0].Venue), "warn"
		p.Answer = partialAnswer(d)
	default:
		p.Badge, p.BadgeKind = "Routed to "+venueName(d.Chosen[0].Venue), "ok"
		p.Answer = routedAnswer(d, dc)
	}
	return p
}

// orderQty is the order size in contracts: the requested quantity, or what
// filled for a notional order.
func orderQty(d canonical.Decision) decimal.Decimal {
	if d.Order.Quantity != nil {
		return *d.Order.Quantity
	}
	return totalFilled(d.Chosen)
}

func isPartial(d canonical.Decision) bool {
	return d.Order.Quantity != nil && totalFilled(d.Chosen).LessThan(*d.Order.Quantity)
}

// plainOrder words the order: "buy 100 YES at market, fill what you can".
func plainOrder(o canonical.Order) string {
	size := "?"
	switch {
	case o.Quantity != nil:
		size = countText(*o.Quantity)
	case o.Notional != nil:
		size = dollarsText(*o.Notional) + " of"
	}
	s := fmt.Sprintf("%s %s %s", o.Side, size, strings.ToUpper(string(o.Outcome)))
	if o.Type == canonical.OrderLimit && o.LimitPrice != nil {
		s += " with a limit of " + centsText(*o.LimitPrice)
	} else {
		s += " at market"
	}
	if o.TIF == canonical.TIFFOK {
		return s + ", all or nothing"
	}
	return s + ", fill what you can"
}

// nameOf is a contract's plain name, from the loaded instrument or its ID.
func (dc decisionContext) nameOf(id string) contractName {
	if in, ok := dc.Instruments[id]; ok {
		return nameContract(in)
	}
	return contractName{Long: truncateID(id), Short: truncateID(id)}
}

func (dc decisionContext) titleOf(id string) string {
	return dc.Instruments[id].Title
}

func verb(side canonical.Side) string { return capitalize(string(side)) }

// legTotal is a leg's all-in for its whole size.
func legTotal(l canonical.Leg) decimal.Decimal { return l.AllInPerContract.Mul(l.Quantity) }

// runnerUp is the rank-2 alternative, or nil.
func runnerUp(d canonical.Decision) *canonical.Alternative {
	for i := range d.Alternatives {
		if d.Alternatives[i].Rank == 2 && len(d.Alternatives[i].Legs) > 0 {
			return &d.Alternatives[i]
		}
	}
	return nil
}

// altPlace says where an alternative fills, relative to the chosen venue:
// "on Kalshi", or "on another Polymarket contract".
func altPlace(alt canonical.Alternative, chosen canonical.Venue) string {
	v := alt.Legs[0].Venue
	if v == chosen {
		return "on another " + venueName(v) + " contract"
	}
	return "on " + venueName(v)
}

// routedAnswer is the plain answer for a full fill on one venue.
func routedAnswer(d canonical.Decision, dc decisionContext) string {
	leg := d.Chosen[0]
	venue := venueName(leg.Venue)
	filled := totalFilled(d.Chosen)
	total := legTotal(leg)
	alt := runnerUp(d)
	buy := d.Order.Side == canonical.SideBuy
	var b strings.Builder
	if leg.Route == "complement" {
		fmt.Fprintf(&b, "%s on %s, using the opposite contract. ", verb(d.Order.Side), venue)
		fmt.Fprintf(&b, "Betting %s on '%s' pays exactly when your %s %s would. ",
			strings.ToUpper(string(leg.Outcome)), dc.nameOf(leg.InstrumentID).Long,
			venueName(canonical.VenueOf(d.TargetID)), strings.ToUpper(string(d.Order.Outcome)))
		word := "costs"
		if !buy {
			word = "brings in"
		}
		fmt.Fprintf(&b, "It %s %s for %s", word, dollarsText(total), countText(filled))
		if alt != nil {
			fmt.Fprintf(&b, " versus %s %s", dollarsText(alt.AllInPerContract.Mul(filled)), altPlace(*alt, leg.Venue))
		}
		b.WriteString(".")
		return b.String()
	}
	fmt.Fprintf(&b, "%s on %s. ", verb(d.Order.Side), venue)
	n := countText(filled)
	switch {
	case alt != nil && buy:
		altTotal := alt.AllInPerContract.Mul(filled)
		fmt.Fprintf(&b, "%s %s cost %s there versus %s %s, so you save %s.", n, plural(int(filled.IntPart()), "contract", "contracts"),
			dollarsText(total), dollarsText(altTotal), altPlace(*alt, leg.Venue), savingText(altTotal.Sub(total)))
	case alt != nil:
		altTotal := alt.AllInPerContract.Mul(filled)
		fmt.Fprintf(&b, "%s %s bring in %s there versus %s %s, so you get %s more.", n, plural(int(filled.IntPart()), "contract", "contracts"),
			dollarsText(total), dollarsText(altTotal), altPlace(*alt, leg.Venue), savingText(total.Sub(altTotal)))
	case buy:
		fmt.Fprintf(&b, "%s %s cost %s. No other venue could fill this order.", n, plural(int(filled.IntPart()), "contract", "contracts"), dollarsText(total))
	default:
		fmt.Fprintf(&b, "%s %s bring in %s. No other venue could fill this order.", n, plural(int(filled.IntPart()), "contract", "contracts"), dollarsText(total))
	}
	if leg.BasisPenaltyPerContract.IsPositive() {
		fmt.Fprintf(&b, " We added a small safety charge (%s each) because the two contracts could settle differently in rare cases.",
			centsText(leg.BasisPenaltyPerContract))
		if alt != nil {
			if buy {
				fmt.Fprintf(&b, " %s is still cheaper.", venue)
			} else {
				fmt.Fprintf(&b, " %s still pays more.", venue)
			}
		}
	}
	return b.String()
}

// savingText is a dollar difference, or "less than a cent" when it rounds away.
func savingText(d decimal.Decimal) string {
	if d.Abs().LessThan(decimal.New(5, -3)) {
		return "less than a cent"
	}
	return dollarsText(d)
}

// partialAnswer is the plain answer when only part of the order fills.
func partialAnswer(d canonical.Decision) string {
	leg := d.Chosen[0]
	filled := totalFilled(d.Chosen)
	return fmt.Sprintf("Partly filled. You can %s %s of your %s contracts on %s; that is all that was on offer. They come to %s, about %s each.",
		d.Order.Side, countText(filled), countText(*d.Order.Quantity), venueName(leg.Venue),
		dollarsText(sumTotals(d.Chosen)), centsText(averageAllIn(d.Chosen)))
}

// splitAnswer is the plain answer when the order fills on several venues.
func splitAnswer(d canonical.Decision) string {
	parts := make([]string, 0, len(d.Chosen))
	for _, l := range d.Chosen {
		parts = append(parts, countText(l.Quantity)+" on "+venueName(l.Venue))
	}
	filled := totalFilled(d.Chosen)
	all := "All " + countText(filled)
	if isPartial(d) {
		all = countText(filled) + " of your " + countText(*d.Order.Quantity)
	}
	return fmt.Sprintf("Split across %s venues: %s %s. No single venue had enough on offer. %s come to %s, about %s each.",
		numberWord(countVenues(d.Chosen)), d.Order.Side, joinAnd(parts), all, dollarsText(sumTotals(d.Chosen)), centsText(averageAllIn(d.Chosen)))
}

func sumTotals(legs []canonical.Leg) decimal.Decimal {
	t := decimal.Zero
	for _, l := range legs {
		t = t.Add(legTotal(l))
	}
	return t
}

// marketPercent is the target's price for the ordered outcome, as the chance
// the market gives it: the mid when both sides are quoted, else the one side
// there is. ok is false when the book is empty or unknown.
func marketPercent(q *canonical.Quote, outcome canonical.Outcome) (decimal.Decimal, bool) {
	if q == nil {
		return decimal.Zero, false
	}
	bid, hasBid := bestPrice(q.YesBids)
	ask, hasAsk := bestPrice(q.YesAsks)
	var yes decimal.Decimal
	switch {
	case hasBid && hasAsk:
		yes = bid.Add(ask).Div(decimal.NewFromInt(2))
	case hasBid:
		yes = bid
	case hasAsk:
		yes = ask
	default:
		return decimal.Zero, false
	}
	if outcome == canonical.OutcomeNo {
		return one.Sub(yes), true
	}
	return yes, true
}

func bestPrice(levels []canonical.Level) (decimal.Decimal, bool) {
	for _, l := range levels {
		if l.Validate() == nil && l.Size.IsPositive() {
			return l.Price, true
		}
	}
	return decimal.Zero, false
}

// noTradeAnswer is the plain answer when nothing filled: what was asked, then
// one sentence per deciding reason (the target, the true substitutes, the
// other partners), from the stored exclusions.
func noTradeAnswer(d canonical.Decision, dc decisionContext) string {
	s := []string{"No trade."}
	ask := fmt.Sprintf("You asked to %s %s on '%s'", d.Order.Side, strings.ToUpper(string(d.Order.Outcome)), dc.nameOf(d.TargetID).Long)
	if p, ok := marketPercent(dc.TargetQuote, d.Order.Outcome); ok {
		ask += ", which the market prices at about " + percentText(p)
	}
	s = append(s, ask+".")
	for _, g := range classGroups(d) {
		s = append(s, g.sentence(d))
	}
	return strings.Join(s, " ")
}

// classGroup is a group of exclusions about one kind of candidate.
type classGroup struct {
	kind  groupKind
	relT  canonical.RelationshipType // for partners
	items []canonical.Exclusion
	// substitutes is how many routable partners the decision considered.
	substitutes int
}

type groupKind int

const (
	kindTarget groupKind = iota
	kindSubstitute
	kindOther
)

// classGroups groups exclusions as the answer reads them: the target first
// (its first reason only), then routable partners ("substitutes") by
// reason, then other partners by relationship type.
func classGroups(d canonical.Decision) []classGroup {
	types := relTypes(d)
	subs := 0
	for _, in := range d.Inputs {
		if in.InstrumentID != d.TargetID && in.RelationshipType.Routable() {
			subs++
		}
	}
	var groups []classGroup
	index := map[string]int{}
	targetSeen := false
	for _, ex := range d.Exclusions {
		var key string
		g := classGroup{substitutes: subs}
		if ex.InstrumentID == d.TargetID {
			if targetSeen {
				continue
			}
			targetSeen = true
			g.kind, key = kindTarget, "target"
		} else {
			rt := partnerType(ex, types)
			g.relT = rt
			if rt.Routable() {
				g.kind, key = kindSubstitute, "sub|"+ex.Reason
			} else {
				g.kind, key = kindOther, "other|"+string(rt)+"|"+ex.Reason
			}
		}
		if i, ok := index[key]; ok {
			groups[i].items = append(groups[i].items, ex)
			continue
		}
		index[key] = len(groups)
		g.items = []canonical.Exclusion{ex}
		groups = append(groups, g)
	}
	ordered := make([]classGroup, 0, len(groups))
	for _, k := range []groupKind{kindTarget, kindSubstitute, kindOther} {
		for _, g := range groups {
			if g.kind == k {
				ordered = append(ordered, g)
			}
		}
	}
	return ordered
}

// partnerType is a partner's relationship type: from the stored inputs, or
// from a not_routable detail for decisions stored before inputs were.
func partnerType(ex canonical.Exclusion, types map[string]canonical.RelationshipType) canonical.RelationshipType {
	if m := notRoutableRe.FindStringSubmatch(ex.Detail); m != nil {
		return canonical.RelationshipType(m[1])
	}
	return types[ex.InstrumentID]
}

// venuesOf names the venue a group's contracts share, or "" when mixed.
func venuesOf(items []canonical.Exclusion) string {
	v := items[0].Venue
	for _, ex := range items[1:] {
		if ex.Venue != v {
			return ""
		}
	}
	return venueName(v)
}

// sentence words one group for the plain answer.
func (g classGroup) sentence(d canonical.Decision) string {
	ex := g.items[0]
	n := len(g.items)
	switch g.kind {
	case kindTarget:
		return targetSentence(d, ex)
	case kindSubstitute:
		var subject string
		switch {
		case n == 1 && g.substitutes <= 1:
			subject = "The only true substitute"
		case n == 1:
			subject = "One substitute on " + venueName(ex.Venue)
		default:
			subject = capitalize(numberWord(n)) + " substitutes"
		}
		return subject + " " + substitutePredicate(d, ex, n > 1) + "."
	}
	venue := venuesOf(g.items)
	noun := plural(n, "contract", "contracts")
	subject := capitalize(numberWord(n)) + " other " + noun
	if venue != "" {
		subject = capitalize(numberWord(n)) + " other " + venue + " " + noun
	}
	return subject + " " + otherPredicate(g.relT, ex, n > 1) + "."
}

// targetSentence words why the ordered contract itself could not fill.
func targetSentence(d canonical.Decision, ex canonical.Exclusion) string {
	venue := venueName(ex.Venue)
	switch ex.Reason {
	case exNoBook:
		return venue + " has no prices for it right now."
	case exInsufficientDepth:
		if n, ok := depthAvailable(ex.Detail); ok && n.IsPositive() {
			return fmt.Sprintf("Only %s are on offer on %s.", countText(n), venue)
		}
		if m := fokDetailRe.FindStringSubmatch(ex.Detail); m != nil {
			return fmt.Sprintf("Only %s of your %s are on offer on %s, and you asked for all or nothing.", m[2], m[1], venue)
		}
		if d.Order.Side == canonical.SideSell {
			return "Nobody on " + venue + " is buying it."
		}
		return "Nobody on " + venue + " is selling it."
	}
	return "The " + venue + " contract " + reasonPhrase(d, ex, false) + "."
}

// depthAvailable reads the contracts on offer from an insufficient_depth detail.
func depthAvailable(detail string) (decimal.Decimal, bool) {
	if m := depthDetailRe.FindStringSubmatch(detail); m != nil {
		if n, err := decimal.NewFromString(m[1]); err == nil {
			return n, true
		}
	}
	return decimal.Zero, false
}

// substitutePredicate words why a routable partner could not be used.
func substitutePredicate(d canonical.Decision, ex canonical.Exclusion, many bool) string {
	if ex.Reason == exSureLoss {
		if many {
			return "would each cost more than they can ever pay out"
		}
		if m := sureLossBuyRe.FindStringSubmatch(ex.Detail); m != nil {
			if c, err := decimal.NewFromString(m[1]); err == nil {
				return "would cost " + perDollarText(c) + " for a contract that pays at most $1, a guaranteed loss"
			}
		}
		if m := sureLossSelRe.FindStringSubmatch(ex.Detail); m != nil {
			if c, err := decimal.NewFromString(m[1]); err == nil {
				return "would pay you " + perDollarText(c) + " or less after costs, a guaranteed loss"
			}
		}
	}
	return reasonPhrase(d, ex, many)
}

// otherPredicate words why a non-routable partner is never used.
func otherPredicate(t canonical.RelationshipType, ex canonical.Exclusion, many bool) string {
	if ex.Reason != exNotRoutable {
		return reasonPhrase(canonical.Decision{}, ex, many)
	}
	switch t {
	case canonical.RelImplies:
		if many {
			return "only cover some of your outcomes, so they aren't substitutes"
		}
		return "only covers some of your outcomes, so it isn't a substitute"
	case canonical.RelRelated:
		if many {
			return "look similar but pay on something different, so they aren't substitutes"
		}
		return "looks similar but pays on something different, so it isn't a substitute"
	}
	if many {
		return "pay on something unrelated, so they aren't substitutes"
	}
	return "pays on something unrelated, so it isn't a substitute"
}

// reasonPhrase translates an exclusion code into a verb phrase, with the
// numbers from its detail. many picks plural verbs.
func reasonPhrase(d canonical.Decision, ex canonical.Exclusion, many bool) string {
	is, has := "is", "has"
	if many {
		is, has = "are", "have"
	}
	switch ex.Reason {
	case exStale:
		if m := staleDetailRe.FindStringSubmatch(ex.Detail); m != nil {
			return has + " a price " + ageWords(m[1]) + " old, too old to trust"
		}
		return has + " a price too old to trust"
	case exSkew:
		if m := skewDetailRe.FindStringSubmatch(ex.Detail); m != nil {
			return has + " prices taken " + m[1] + " seconds apart from the others, too far to compare"
		}
		return has + " prices taken too far apart to compare"
	case exLowConfidence:
		if m := lowConfDetailRe.FindStringSubmatch(ex.Detail); m != nil {
			return "might be the same bet, but we're not sure enough (" + twoPlaces(m[1]) + ", need " + twoPlaces(m[2]) + ")"
		}
		return "might be the same bet, but we're not sure enough"
	case exInsufficientDepth:
		if n, ok := depthAvailable(ex.Detail); ok && n.IsPositive() {
			return has + " only " + countText(n) + " contracts on offer"
		}
		if m := fokDetailRe.FindStringSubmatch(ex.Detail); m != nil {
			return "can fill only " + m[2] + " of the " + m[1] + " contracts you need all at once"
		}
		if d.Order.Side == canonical.SideSell {
			return has + " nobody buying at any price"
		}
		return has + " nobody selling at any price"
	case exNoBook:
		return has + " no prices at all right now"
	case exSureLoss:
		return "would cost more than " + pick(many, "it", "they") + " can ever pay out"
	case exLimitNotReachable:
		if m := limitDetailRe.FindStringSubmatch(ex.Detail); m != nil {
			return has + " only " + m[1] + " of " + m[2] + " contracts at your limit"
		}
		return has + " nothing on offer at your limit price"
	case exNotOpen:
		return is + " not open for trading"
	case exNotRoutable:
		return otherPredicate(canonical.RelationshipType(""), ex, many)
	case exFeeUnknown:
		return has + " a fee we don't know, so we can't price " + pick(many, "it", "them")
	case exBasisRiskUnknown:
		return has + " a safety risk we can't size, so we skipped " + pick(many, "it", "them")
	case exComplementUnsupp:
		return is + " an opposite contract, which wasn't supported when this was decided"
	case exNoQuantity:
		return "couldn't be sized for this order"
	}
	return is + " missing data we need, so we skipped " + pick(many, "it", "them")
}

// pick returns many when the subject is plural, else single.
func pick(isMany bool, single, many string) string {
	if isMany {
		return many
	}
	return single
}

// Exclusion codes the presenter words that route_notrouted.go does not list.
const (
	exNoQuantity       = "no_quantity"
	exFeeUnknown       = "fee_unknown"
	exBasisRiskUnknown = "basis_risk_unknown"
)

// ageWords turns a seconds count into "5 minutes" or "40 seconds".
func ageWords(secs string) string {
	n, err := strconv.Atoi(secs)
	if err != nil {
		return secs + " seconds"
	}
	d := time.Duration(n) * time.Second
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d %s", n, plural(n, "second", "seconds"))
	case d < time.Hour:
		m := int(d.Minutes())
		return fmt.Sprintf("%d %s", m, plural(m, "minute", "minutes"))
	}
	h := int(d.Hours())
	return fmt.Sprintf("%d %s", h, plural(h, "hour", "hours"))
}

// twoPlaces formats a decimal string to two places ("0.9" → "0.90").
func twoPlaces(s string) string {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return s
	}
	return d.StringFixed(2)
}

// whyNotGroups is "Why not the others?": one line per excluded group, each
// expandable to the contracts in it.
func whyNotGroups(d canonical.Decision, dc decisionContext) []whyNotGroup {
	var out []whyNotGroup
	for _, g := range classGroups(d) {
		items := make([]whyNotItem, 0, len(g.items))
		for _, ex := range g.items {
			items = append(items, whyNotItem{
				Name:   venueName(ex.Venue) + " · " + dc.nameOf(ex.InstrumentID).Long,
				Reason: itemReason(d, g, ex),
			})
		}
		out = append(out, whyNotGroup{Summary: g.summary(d), Items: items})
	}
	return out
}

// summary is a group's one line, with digits: "4 Polymarket contracts only
// cover some of your outcomes".
func (g classGroup) summary(d canonical.Decision) string {
	ex := g.items[0]
	n := len(g.items)
	switch g.kind {
	case kindTarget:
		return "Your " + venueName(ex.Venue) + " contract " + targetPhrase(d, ex)
	case kindSubstitute:
		lead := fmt.Sprintf("%d %s", n, plural(n, "substitute", "substitutes"))
		if v := venuesOf(g.items); v != "" {
			lead = fmt.Sprintf("%d %s %s", n, v, plural(n, "substitute", "substitutes"))
		}
		return lead + " " + itemReason(d, g, ex)
	}
	lead := fmt.Sprintf("%d %s", n, plural(n, "contract", "contracts"))
	if v := venuesOf(g.items); v != "" {
		lead = fmt.Sprintf("%d %s %s", n, v, plural(n, "contract", "contracts"))
	}
	return lead + " " + strings.SplitN(otherPredicate(g.relT, ex, n > 1), ", so", 2)[0]
}

// targetPhrase is the target's reason as a phrase after "Your Kalshi contract".
func targetPhrase(d canonical.Decision, ex canonical.Exclusion) string {
	if ex.Reason == exInsufficientDepth {
		if n, ok := depthAvailable(ex.Detail); !ok || n.IsZero() {
			if fokDetailRe.MatchString(ex.Detail) {
				return reasonPhrase(d, ex, false)
			}
			if d.Order.Side == canonical.SideSell {
				return "has nobody buying at any price"
			}
			return "has nobody selling at any price"
		}
	}
	return reasonPhrase(d, ex, false)
}

// itemReason is one contract's reason, in the singular.
func itemReason(d canonical.Decision, g classGroup, ex canonical.Exclusion) string {
	switch g.kind {
	case kindTarget:
		return targetPhrase(d, ex)
	case kindSubstitute:
		if ex.Reason == exSureLoss {
			if m := sureLossBuyRe.FindStringSubmatch(ex.Detail); m != nil {
				if c, err := decimal.NewFromString(m[1]); err == nil {
					return "would cost more than it can ever pay out (" + perDollarText(c) + " per $1)"
				}
			}
			return "would cost more than it can ever pay out"
		}
		return substitutePredicate(d, ex, false)
	}
	return strings.SplitN(otherPredicate(g.relT, ex, false), ", so", 2)[0]
}

// nextStepFor turns the opposite-order suggestion into the next step.
func nextStepFor(d canonical.Decision, s *routeSuggestion) *nextStep {
	if s == nil {
		return nil
	}
	where := "on " + venueName(s.Venue)
	if s.Complement {
		where = "through the opposite " + venueName(s.Venue) + " contract"
	}
	word := "on offer"
	if d.Order.Side == canonical.SideSell {
		word = "wanted"
	}
	return &nextStep{
		Label:  fmt.Sprintf("Try the other side: %s %s %s at %s", d.Order.Side, strings.ToUpper(string(s.Outcome)), where, centsText(s.Price)),
		Detail: countText(s.Size) + " " + word,
		Href:   s.Link,
	}
}

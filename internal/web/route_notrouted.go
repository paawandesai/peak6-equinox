package web

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// M6 P2: a "Not routed" result reads as a decision. The lead sentence names
// the deciding reason per candidate class (the target, complements, other
// partners), built only from the stored Decision's Exclusions and Inputs, and
// a suggestion offers the opposite outcome when a stored book shows it would
// have a candidate. The router (internal/route) is not changed; its reason
// codes are mirrored here as strings.

// Router exclusion reason codes (internal/route/route.go).
const (
	exNoRelationship       = "no_relationship"
	exRelationshipMismatch = "relationship_mismatch"
	exNotRoutable          = "not_routable"
	exComplementUnsupp     = "complement_unsupported"
	exLowConfidence        = "low_confidence"
	exQuoteMismatch        = "quote_mismatch"
	exNotOpen              = "not_open"
	exNoBook               = "no_book"
	exInsufficientDepth    = "insufficient_depth"
	exLimitNotReachable    = "limit_not_reachable"
	exStale                = "stale"
	exSkew                 = "skew"
	exSureLoss             = "sure_loss"
)

var (
	depthDetailRe = regexp.MustCompile(`^(\S+) contracts available on consumed side$`)
	fokDetailRe   = regexp.MustCompile(`^FOK requires (\S+) contracts; \S+ can fill (\S+)$`)
	sureLossBuyRe = regexp.MustCompile(`all-in cost (\S+) per \$1 payout`)
	sureLossSelRe = regexp.MustCompile(`net proceeds (\S+) per contract`)
	notRoutableRe = regexp.MustCompile(`relationship type (\S+) is never routed`)
)

// candidateClass is who an exclusion is about.
type candidateClass int

const (
	classTarget candidateClass = iota
	classComplement
	classPartner
)

// exclusionGroup is the exclusions of one class sharing a reason (and, for
// not_routable, a relationship type).
type exclusionGroup struct {
	class   candidateClass
	reason  string
	relType string
	items   []canonical.Exclusion
}

// venueLabel capitalises a venue id for prose: "kalshi" → "Kalshi".
func venueLabel(v canonical.Venue) string {
	s := string(v)
	if s == "" {
		return "venue"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// relTypes maps each input instrument to its relationship type to the target.
func relTypes(d canonical.Decision) map[string]canonical.RelationshipType {
	m := make(map[string]canonical.RelationshipType, len(d.Inputs))
	for _, in := range d.Inputs {
		m[in.InstrumentID] = in.RelationshipType
	}
	return m
}

// classify puts one exclusion in its class and returns its relationship type.
func classify(d canonical.Decision, types map[string]canonical.RelationshipType, ex canonical.Exclusion) (candidateClass, string) {
	if ex.InstrumentID == d.TargetID {
		return classTarget, ""
	}
	rt := string(types[ex.InstrumentID])
	if m := notRoutableRe.FindStringSubmatch(ex.Detail); m != nil {
		rt = m[1]
	}
	if rt == string(canonical.RelComplement) {
		return classComplement, rt
	}
	return classPartner, rt
}

// groupExclusions groups exclusions by class and reason, target first, then
// complements, then partners, each in first-seen order. Only the target's
// first exclusion is kept: the router lists its reasons in the order checked.
func groupExclusions(d canonical.Decision) []exclusionGroup {
	types := relTypes(d)
	var groups []exclusionGroup
	index := map[string]int{}
	targetSeen := false
	for _, ex := range d.Exclusions {
		class, rt := classify(d, types, ex)
		if class == classTarget {
			if targetSeen {
				continue
			}
			targetSeen = true
		}
		key := fmt.Sprintf("%d|%s", class, ex.Reason)
		if ex.Reason == exNotRoutable {
			key += "|" + rt // "4 partners are 'implies'" needs one type per group
		}
		if i, ok := index[key]; ok {
			groups[i].items = append(groups[i].items, ex)
			continue
		}
		index[key] = len(groups)
		groups = append(groups, exclusionGroup{class: class, reason: ex.Reason, relType: rt, items: []canonical.Exclusion{ex}})
	}
	ordered := make([]exclusionGroup, 0, len(groups))
	for _, c := range []candidateClass{classTarget, classComplement, classPartner} {
		for _, g := range groups {
			if g.class == c {
				ordered = append(ordered, g)
			}
		}
	}
	return ordered
}

// notRoutedSentence is the one-sentence lead for a decision with no fill.
func notRoutedSentence(d canonical.Decision) string {
	groups := groupExclusions(d)
	if len(groups) == 0 {
		return "Nothing routed: every candidate was excluded."
	}
	complements := countComplements(d)
	parts := make([]string, 0, len(groups))
	for _, g := range groups {
		parts = append(parts, groupPhrase(g, d.Order, complements))
	}
	return "Nothing routed: " + joinAnd(parts) + "."
}

// countComplements counts complement partners in the stored inputs (0 for
// decisions stored before inputs were recorded).
func countComplements(d canonical.Decision) int {
	n := 0
	for _, in := range d.Inputs {
		if in.RelationshipType == canonical.RelComplement {
			n++
		}
	}
	return n
}

// joinAnd joins phrases as "a", "a and b" or "a, b, and c".
func joinAnd(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + ", and " + parts[len(parts)-1]
}

// sideWord is what the order consumes: offers for a buy, bids for a sell.
func sideWord(side canonical.Side) string {
	if side == canonical.SideSell {
		return "bids"
	}
	return "offers"
}

// flipOutcome returns the other outcome.
func flipOutcome(o canonical.Outcome) canonical.Outcome {
	if o == canonical.OutcomeYes {
		return canonical.OutcomeNo
	}
	return canonical.OutcomeYes
}

// checkedOutcome is the outcome the router checked for a class: the order's
// outcome, or the opposite one on a complement.
func checkedOutcome(class candidateClass, o canonical.Order) canonical.Outcome {
	if class == classComplement {
		return flipOutcome(o.Outcome)
	}
	return o.Outcome
}

// groupPhrase words one group: the target by its venue, a lone complement as
// "the only complement" when it is the only one, and other groups by count.
func groupPhrase(g exclusionGroup, o canonical.Order, complements int) string {
	n := len(g.items)
	ex := g.items[0]
	outcome := strings.ToUpper(string(checkedOutcome(g.class, o)))
	var subject string
	switch {
	case g.class == classTarget:
		subject = "the " + venueLabel(ex.Venue) + " book"
	case g.class == classComplement && n == 1 && complements <= 1:
		subject = "the only complement"
	case g.class == classComplement && n == 1:
		subject = "the " + venueLabel(ex.Venue) + " complement"
	case g.class == classComplement:
		subject = fmt.Sprintf("%d complements", n)
	default:
		subject = fmt.Sprintf("%d partner%s", n, pluralS(n))
	}
	return subject + " " + reasonPredicate(g, o, outcome, n > 1)
}

// reasonPredicate is the verb phrase for a group's reason; plural is the
// subject's grammatical number ("have"/"are" rather than "has"/"is").
func reasonPredicate(g exclusionGroup, o canonical.Order, outcome string, plural bool) string {
	ex := g.items[0]
	has, is := "has", "is"
	if plural {
		has, is = "have", "are"
	}
	switch g.reason {
	case exNoBook:
		if ex.Detail == "no book available" {
			return is + " empty (no bids or offers)"
		}
		return has + " no " + outcome + " " + sideWord(o.Side)
	case exInsufficientDepth:
		return depthPredicate(ex.Detail, outcome, o.Side, has, is)
	case exSureLoss:
		return sureLossPredicate(ex.Detail, plural)
	case exNotRoutable:
		rt := g.relType
		if rt == "" {
			rt = "not routable"
		}
		return fmt.Sprintf("%s '%s', which %s never routed", is, rt, is)
	case exLowConfidence:
		return is + " below the routing confidence floor"
	case exStale:
		return is + " stale (" + ex.Detail + ")"
	case exSkew:
		return is + " too far behind the freshest quote (" + ex.Detail + ")"
	case exLimitNotReachable:
		return "cannot fill within the limit (" + ex.Detail + ")"
	case exNotOpen:
		return is + " not open (" + ex.Detail + ")"
	}
	return is + " excluded: " + exclusionReasonPhrase(g.reason)
}

// depthPredicate words an insufficient_depth exclusion from its detail.
func depthPredicate(detail, outcome string, side canonical.Side, has, is string) string {
	if m := depthDetailRe.FindStringSubmatch(detail); m != nil {
		if d, err := decimal.NewFromString(m[1]); err == nil && d.IsZero() {
			return has + " no " + outcome + " " + sideWord(side)
		}
		return fmt.Sprintf("%s too shallow (%s %s contracts on that side)", is, m[1], outcome)
	}
	if m := fokDetailRe.FindStringSubmatch(detail); m != nil {
		return fmt.Sprintf("can fill only %s of the %s contracts fill-or-kill needs", m[2], m[1])
	}
	return is + " too shallow"
}

// sureLossPredicate words a sure_loss exclusion with its stored cost.
func sureLossPredicate(detail string, plural bool) string {
	if plural {
		return "would each be a sure loss (all-in cost of $1 or more per $1 payout)"
	}
	if m := sureLossBuyRe.FindStringSubmatch(detail); m != nil {
		return "would cost $" + m[1] + " per $1 payout (a sure loss)"
	}
	if m := sureLossSelRe.FindStringSubmatch(detail); m != nil {
		return "would net $" + m[1] + " per contract (a sure loss)"
	}
	return "would be a sure loss"
}

// relationshipReasons are exclusions that rule a partner out whatever the
// order: the opposite order would exclude it too.
var relationshipReasons = map[string]bool{
	exNoRelationship: true, exRelationshipMismatch: true, exNotRoutable: true,
	exComplementUnsupp: true, exLowConfidence: true, exQuoteMismatch: true,
}

// routeSuggestion is the "Try: buy NO" hint under a not-routed lead.
type routeSuggestion struct {
	Order string // e.g. "buy NO"
	Why   string // the stored book that shows a candidate
	Link  string // the pair page with the order preset; "" when no pair is known
}

// suggestOpposite returns a hint to try the opposite outcome when the stored
// quotes show a candidate for it: the target's book, or a complement's book
// on the other outcome. Partners ruled out by their relationship are never
// counted. It returns nil for routed decisions or when no book has depth.
func suggestOpposite(d canonical.Decision, quotes map[string]canonical.Quote, pairID int64) *routeSuggestion {
	if len(d.Chosen) > 0 {
		return nil
	}
	alt := flipOutcome(d.Order.Outcome)
	types := relTypes(d)
	ruledOut := map[string]bool{}
	for _, ex := range d.Exclusions {
		if relationshipReasons[ex.Reason] {
			ruledOut[ex.InstrumentID] = true
		}
	}
	ids := []string{d.TargetID}
	for _, in := range d.Inputs {
		if in.InstrumentID != d.TargetID && in.RelationshipType.Routable() && !ruledOut[in.InstrumentID] {
			ids = append(ids, in.InstrumentID)
		}
	}
	for _, id := range ids {
		q, ok := quotes[id]
		if !ok {
			continue
		}
		check, who := alt, "The "+venueLabel(canonical.VenueOf(id))+" book"
		if types[id] == canonical.RelComplement {
			check, who = d.Order.Outcome, "The "+venueLabel(canonical.VenueOf(id))+" complement"
		}
		levels := consumedLevels(q, d.Order.Side, check)
		if len(levels) == 0 {
			continue
		}
		order := string(d.Order.Side) + " " + strings.ToUpper(string(alt))
		why := fmt.Sprintf("%s has %s %s (best %s, %s contracts)", who, strings.ToUpper(string(check)), sideWord(d.Order.Side),
			levels[0].Price.StringFixed(4), levels[0].Size.String())
		return &routeSuggestion{Order: order, Why: why, Link: suggestionLink(pairID, d, alt)}
	}
	return nil
}

// consumedLevels is the book an order would walk, priced in the order's
// outcome (buy NO walks YES bids at 1 − price), skipping invalid levels. It
// mirrors internal/route's book side for display only.
func consumedLevels(q canonical.Quote, side canonical.Side, outcome canonical.Outcome) []canonical.Level {
	if q.Quality.Has(canonical.QualityNoBook) {
		return nil
	}
	flip := outcome == canonical.OutcomeNo
	src := q.YesBids
	if (side == canonical.SideBuy) != flip {
		src = q.YesAsks
	}
	out := make([]canonical.Level, 0, len(src))
	for _, l := range src {
		if l.Validate() != nil || !l.Size.IsPositive() {
			continue
		}
		p := l.Price
		if flip {
			p = decimal.NewFromInt(1).Sub(p)
		}
		out = append(out, canonical.Level{Price: p, Size: l.Size})
	}
	return out
}

// suggestionLink is the pair page with the opposite order preset (the pair
// page reads target, side, outcome, qty, type and tif). A limit price does not
// carry over to the other outcome, so it is left for the viewer to enter.
func suggestionLink(pairID int64, d canonical.Decision, outcome canonical.Outcome) string {
	if pairID <= 0 {
		return ""
	}
	v := url.Values{}
	v.Set("target", d.TargetID)
	v.Set("side", string(d.Order.Side))
	v.Set("outcome", string(outcome))
	if d.Order.Quantity != nil {
		v.Set("qty", d.Order.Quantity.String())
	}
	v.Set("type", string(d.Order.Type))
	v.Set("tif", string(d.Order.TIF))
	return "/pairs/" + strconv.FormatInt(pairID, 10) + "?" + v.Encode() + "#route"
}

// candidateQuotes maps each candidate's instrument to the quote the router used.
func candidateQuotes(target canonical.Candidate, partners []canonical.Candidate) map[string]canonical.Quote {
	m := make(map[string]canonical.Quote, len(partners)+1)
	m[target.Instrument.ID] = target.Quote
	for _, p := range partners {
		m[p.Instrument.ID] = p.Quote
	}
	return m
}

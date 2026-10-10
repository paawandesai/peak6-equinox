package web

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// The pair presenter (ui/plain-language): both contracts by plain name, the
// relationship in words, one sentence of meaning, three chips, the reasons
// in bullets and one-click order presets. Fed meanings are computed from the
// parsed terms and the facts the matcher used; other subjects fall back to
// the plain names. Fact keys mirror config/facts.yaml.

const (
	factFedUpper   = "fed_upper_bound"
	factFedStep    = "fed_rate_step"
	factFedMeeting = "fed_next_meeting"
	maxWhyBullets  = 4
)

// plainPair is the top card and the reasons of one pair page.
type plainPair struct {
	Title    string // "Fed above 4.25% ↔ Fed +50 bps"
	A, B     pairSide
	Badge    string // "Same bet, given 3 facts"
	Kind     string // ok, info, muted: the badge colour
	Meaning  string
	Lead     string // the meaning's first sentence, for the cards on /
	Specific bool   // the meaning was built from parsed terms, not a generic fallback
	Chips    []pairChip
	WhyTitle string
	Why      []string
	Presets  []pairPreset
}

// pairSide is one contract in the top card.
type pairSide struct {
	Venue string // "Kalshi"
	Name  string // "Fed rate above 4.25% after Oct 28"
	Title string // the venue's own title, for the title attribute
	Price string // "YES costs 1¢" or "No YES offers right now"
	Age   string // "price 27 s old"
}

type pairChip struct {
	Text    string // "Confidence 0.93 (high)"
	Caption string // one visible line of explanation
	Kind    string // ok, warn, muted
	Href    string // glossary anchor
}

type pairPreset struct {
	Label string // "Buy 100 YES on Kalshi"
	Href  string
}

// pairInput is what the presenter reads besides the relationship.
type pairInput struct {
	Rel           canonical.Relationship
	A, B          canonical.Instrument
	QuoteA        *canonical.Quote // nil when the venue has no quote
	QuoteB        *canonical.Quote
	AgeA, AgeB    string // "27 s", "" when unknown
	MinConfidence decimal.Decimal
}

// presentPair builds the plain view of a pair.
func presentPair(in pairInput) *plainPair {
	na, nb := nameContract(in.A), nameContract(in.B)
	p := &plainPair{
		Title: "Pair #" + strconv.FormatInt(in.Rel.ID, 10),
		A:     pairSideOf(in.A, na, in.QuoteA, in.AgeA),
		B:     pairSideOf(in.B, nb, in.QuoteB, in.AgeB),
	}
	if na.FromTerms && nb.FromTerms {
		p.Title = na.Short + " ↔ " + nb.Short
	}
	p.Badge, p.Kind = relBadge(in.Rel)
	fc := readFacts(in.Rel.FactsUsed)
	p.Meaning, p.Specific = pairMeaning(in, fc)
	p.Lead = p.Meaning
	if i := strings.Index(p.Meaning, ". "); i > 0 {
		p.Lead = p.Meaning[:i+1]
	}
	p.Chips = pairChips(in.Rel, in.MinConfidence)
	p.WhyTitle, p.Why = pairWhy(in, fc)
	p.Presets = pairPresets(in.Rel)
	return p
}

func pairSideOf(in canonical.Instrument, n contractName, q *canonical.Quote, age string) pairSide {
	s := pairSide{Venue: venueName(in.Venue), Name: n.Long, Title: in.Title}
	if s.Venue == "Venue" {
		s.Venue = venueName(canonical.VenueOf(in.ID))
	}
	switch {
	case q == nil:
		s.Price = "No price collected yet"
	default:
		if ask, ok := bestPrice(q.YesAsks); ok {
			s.Price = "YES costs " + centsText(ask)
		} else if bid, ok := bestPrice(q.YesBids); ok {
			s.Price = "No YES offers; NO costs " + centsText(one.Sub(bid))
		} else {
			s.Price = "No offers right now"
		}
		if age != "" {
			s.Age = "price " + age + " old"
		}
	}
	return s
}

// relBadge words the relationship type.
func relBadge(r canonical.Relationship) (string, string) {
	switch r.Type {
	case canonical.RelEquivalent:
		return "Same bet", "ok"
	case canonical.RelEquivalentGivenFacts:
		n := len(r.FactsUsed)
		return fmt.Sprintf("Same bet, given %d %s", n, plural(n, "fact", "facts")), "ok"
	case canonical.RelComplement:
		return "Opposite contracts", "info"
	case canonical.RelImplies:
		return "One-way: not a substitute", "info"
	case canonical.RelRelated:
		return "Lookalike, not the same bet", "muted"
	}
	return "Unrelated", "muted"
}

// pairFacts is the Fed facts a pair used, read from their stored values.
type pairFacts struct {
	cur, step *decimal.Decimal
	meeting   *time.Time
	curFact   *canonical.FactUse
	others    []canonical.FactUse
}

func readFacts(fs []canonical.FactUse) pairFacts {
	var f pairFacts
	for i := range fs {
		fu := fs[i]
		switch fu.Key {
		case factFedUpper:
			if d, err := decimal.NewFromString(fu.Value); err == nil {
				f.cur, f.curFact = &d, &fs[i]
				continue
			}
		case factFedStep:
			if d, err := decimal.NewFromString(fu.Value); err == nil && d.IsPositive() {
				f.step = &d
				continue
			}
		case factFedMeeting:
			if t, err := time.Parse("2006-01-02", fu.Value); err == nil {
				f.meeting = &t
				continue
			}
		}
		f.others = append(f.others, fu)
	}
	return f
}

// levelSet is a Fed contract's payout as a set of upper-bound levels after
// the meeting: op is gte, lte or eq.
type levelSet struct {
	op    canonical.Comparator
	level decimal.Decimal
}

// same reports whether two level sets are the same set of levels.
func (s levelSet) same(o levelSet) bool { return s.op == o.op && s.level.Equal(o.level) }

// fedLevelSet converts a Fed contract's terms into a level set, using the
// current rate for change markets and the rate step to close strict bounds.
func fedLevelSet(t canonical.Terms, f pairFacts) (levelSet, bool) {
	if t.Subject != termFedFunds || t.Unit != termPercent || t.Threshold == nil || t.Polarity != canonical.PolarityPositive {
		return levelSet{}, false
	}
	l := *t.Threshold
	switch t.Metric {
	case termRateLevel:
	case termRateChange:
		if f.cur == nil {
			return levelSet{}, false
		}
		l = f.cur.Add(l)
	default:
		return levelSet{}, false
	}
	switch t.Comparator {
	case canonical.ComparatorGTE, canonical.ComparatorLTE, canonical.ComparatorEQ:
		return levelSet{t.Comparator, l}, true
	case canonical.ComparatorGT:
		if f.step != nil {
			return levelSet{canonical.ComparatorGTE, l.Add(*f.step)}, true
		}
		return levelSet{canonical.ComparatorGT, l}, true
	case canonical.ComparatorLT:
		if f.step != nil {
			return levelSet{canonical.ComparatorLTE, l.Sub(*f.step)}, true
		}
		return levelSet{canonical.ComparatorLT, l}, true
	}
	return levelSet{}, false
}

// setPhrase words a level set against the current rate: "the Fed raising
// rates to 4.50% or more".
func setPhrase(s levelSet, cur decimal.Decimal) string {
	l := pctText(s.level)
	c := s.level.Cmp(cur)
	switch s.op {
	case canonical.ComparatorGTE:
		return [...]string{"the rate staying at " + l + " or higher", "the Fed holding or raising rates", "the Fed raising rates to " + l + " or more"}[c+1]
	case canonical.ComparatorLTE:
		return [...]string{"the Fed cutting rates to " + l + " or less", "the Fed holding or cutting rates", "the rate staying at " + l + " or lower"}[c+1]
	case canonical.ComparatorGT:
		if c < 0 {
			return "the rate staying above " + l
		}
		return "the Fed raising rates above " + l
	case canonical.ComparatorLT:
		if c > 0 {
			return "the rate staying below " + l
		}
		return "the Fed cutting rates below " + l
	}
	return [...]string{"the Fed cutting rates to exactly " + l, "the Fed holding rates at " + l, "the Fed raising rates to exactly " + l}[c+1]
}

// meetingPhrase is " at the October meeting" from the meeting fact or a deadline.
func meetingPhrase(f pairFacts, t canonical.Terms) string {
	switch {
	case f.meeting != nil:
		return " at the " + f.meeting.Format("January") + " meeting"
	case t.Deadline != nil:
		return " at the " + venueDate(*t.Deadline, t.DeadlineTZ).Format("January") + " meeting"
	}
	return ""
}

// stepWords words the rate step: 0.25 → "quarter-point steps".
func stepWords(d decimal.Decimal) string {
	switch d.String() {
	case "0.25":
		return "quarter-point steps"
	case "0.5":
		return "half-point steps"
	}
	return bpsText(d) + "-basis-point steps"
}

// amountWords words a rate change: 0.5 → "half a point".
func amountWords(d decimal.Decimal) string {
	switch d.Abs().String() {
	case "0.25":
		return "a quarter point"
	case "0.5":
		return "half a point"
	case "0.75":
		return "three quarters of a point"
	case "1":
		return "a full point"
	}
	return bpsText(d) + " bps"
}

// conditions are the facts a meaning rests on, in words.
func conditions(f pairFacts) []string {
	var c []string
	if f.cur != nil {
		c = append(c, "the current rate is "+pctText(*f.cur))
	}
	if f.step != nil {
		c = append(c, "the Fed moves in "+stepWords(*f.step))
	}
	for _, o := range f.others {
		c = append(c, strings.ReplaceAll(o.Key, "_", " ")+" is "+o.Value)
	}
	return c
}

// fedClause words one Fed contract as "the rate stays above 3.50%" or "the
// Fed cuts by half a point or more"; ok is false for other shapes.
func fedClause(t canonical.Terms, f pairFacts) (string, bool) {
	if t.Subject != termFedFunds || t.Unit != termPercent || t.Threshold == nil || t.Polarity != canonical.PolarityPositive {
		return "", false
	}
	th := *t.Threshold
	if t.Metric == termRateChange {
		switch {
		case t.Comparator == canonical.ComparatorEQ && th.IsZero():
			return "the Fed holds rates", true
		case t.Comparator == canonical.ComparatorEQ:
			return "the " + fedMove(th) + " by exactly " + amountWords(th), true
		case (t.Comparator == canonical.ComparatorGTE && th.IsPositive()) || (t.Comparator == canonical.ComparatorLTE && th.IsNegative()):
			return "the " + fedMove(th) + " by " + amountWords(th) + " or more", true
		case (t.Comparator == canonical.ComparatorGT && th.IsPositive()) || (t.Comparator == canonical.ComparatorLT && th.IsNegative()):
			return "the " + fedMove(th) + " by more than " + amountWords(th), true
		}
		return "", false
	}
	if t.Metric != termRateLevel {
		return "", false
	}
	l := pctText(th)
	hold := f.cur != nil && ((t.Comparator == canonical.ComparatorGT || t.Comparator == canonical.ComparatorGTE) == th.LessThan(*f.cur))
	switch t.Comparator {
	case canonical.ComparatorGT:
		return pickVerb(f.cur, hold, "the rate stays above ", "the rate rises above ", "the rate ends above ") + l, true
	case canonical.ComparatorGTE:
		return pickVerb(f.cur, hold, "the rate stays at ", "the rate rises to ", "the rate ends at ") + l + " or higher", true
	case canonical.ComparatorLT:
		return pickVerb(f.cur, hold, "the rate stays below ", "the rate falls below ", "the rate ends below ") + l, true
	case canonical.ComparatorLTE:
		return pickVerb(f.cur, hold, "the rate stays at ", "the rate falls to ", "the rate ends at ") + l + " or lower", true
	case canonical.ComparatorEQ:
		return "the rate is exactly " + l, true
	}
	return "", false
}

// pickVerb chooses "stays" when the current rate already satisfies the
// clause, "rises"/"falls" when it would have to move, and a neutral verb
// when the current rate is unknown.
func pickVerb(cur *decimal.Decimal, hold bool, stays, moves, neutral string) string {
	switch {
	case cur == nil:
		return neutral
	case hold:
		return stays
	}
	return moves
}

// pairMeaning is the one-sentence meaning (two for facts-based pairs).
func pairMeaning(in pairInput, f pairFacts) (meaning string, specific bool) {
	ca, okA := fedClause(in.A.Terms, f)
	cb, okB := fedClause(in.B.Terms, f)
	switch in.Rel.Type {
	case canonical.RelEquivalent, canonical.RelEquivalentGivenFacts:
		s := "These pay out on the same outcome, so a YES on one is a YES on the other."
		sa, okA := fedLevelSet(in.A.Terms, f)
		sb, okB := fedLevelSet(in.B.Terms, f)
		if okA && okB && sa.same(sb) && f.cur != nil {
			s, specific = "These pay out on the same outcome: "+setPhrase(sa, *f.cur)+meetingPhrase(f, in.A.Terms)+".", true
		}
		if c := conditions(f); in.Rel.Type == canonical.RelEquivalentGivenFacts && len(c) > 0 {
			s += " That holds as long as " + joinAnd(c) + "."
		}
		return s, specific
	case canonical.RelComplement:
		if okA && okB {
			return "These are opposites: one pays if " + ca + ", the other if " + cb + ". Buying YES on one is the same as buying NO on the other.", true
		}
		return "These are opposites: one pays exactly when the other doesn't. Buying YES on one is the same as buying NO on the other.", false
	case canonical.RelImplies:
		if okA {
			return "These overlap but aren't the same bet: if " + ca + ", the other pays too, but not the other way round. Equinox never routes between them.", true
		}
		return "These overlap but aren't the same bet: when the first pays, the other pays too, but not the other way round. Equinox never routes between them.", false
	case canonical.RelRelated:
		if okA && okB {
			return "These look alike but pay on different things: one pays if " + ca + ", the other if " + cb + ". Equinox never routes between them.", true
		}
		return "These look alike but pay on different things. Equinox never routes between them.", false
	}
	return "These pay on unrelated things. Equinox never routes between them.", false
}

// pairChips are confidence, safety risk and routability, each with a caption.
func pairChips(r canonical.Relationship, floor decimal.Decimal) []pairChip {
	word := ConfidenceWord(r.Confidence)
	conf := pairChip{Text: "Confidence " + r.Confidence.StringFixed(2) + " (" + word + ")", Href: "/glossary#term-confidence",
		Caption: "How sure we are these are the same bet. Routing needs " + floor.StringFixed(2) + ".", Kind: "ok"}
	if !r.Type.Routable() {
		conf.Caption = "How sure we are of this relationship."
	}
	if word != "high" {
		conf.Kind = "warn"
	}
	safety := pairChip{Text: "Safety risk: not scored", Kind: "muted", Href: "/glossary#term-safety-risk",
		Caption: "Only routable and one-way pairs are scored."}
	if r.BasisRisk != nil {
		w := BasisRiskWord(r.BasisRisk)
		safety.Text = "Safety risk " + r.BasisRisk.Score.StringFixed(2) + " (" + w + ")"
		safety.Caption = "The chance the two settle differently. It sets the safety charge."
		safety.Kind = map[string]string{"low": "ok", "moderate": "warn", "high": "danger"}[w]
	}
	route := pairChip{Text: "Routable", Kind: "ok", Href: "/glossary#term-routable",
		Caption: "Equinox compares prices across these two."}
	if !r.Type.Routable() {
		route = pairChip{Text: "Not routable", Kind: "muted", Href: "/glossary#term-routable",
			Caption: "Equinox never routes an order between these two."}
	}
	return []pairChip{conf, safety, route}
}

// sourceWords names a resolution source in words.
func sourceWords(src string) string {
	switch src {
	case "federalreserve_gov":
		return "the Fed's own announcement"
	case "", canonical.Unknown:
		return ""
	}
	return "the same source"
}

// diffOf returns a pair's stored diff row for a term.
func diffOf(r canonical.Relationship, field string) (canonical.FieldDiff, bool) {
	for _, d := range r.Diff {
		if d.Field == field {
			return d, true
		}
	}
	return canonical.FieldDiff{}, false
}

// pairWhy is the "why" section: a title by type and up to four bullets in
// words, from the diff, the facts and the levels.
func pairWhy(in pairInput, f pairFacts) (string, []string) {
	r := in.Rel
	va, vb := venueName(in.A.Venue), venueName(in.B.Venue)
	title := "Why we think they're the same bet"
	switch r.Type {
	case canonical.RelComplement:
		title = "Why we think they're opposites"
	case canonical.RelImplies:
		title = "Why one only covers part of the other"
	case canonical.RelRelated, canonical.RelUnrelated:
		title = "Why they aren't the same bet"
	}
	var out []string
	var unknown string
	if d, ok := diffOf(r, "resolution_source"); ok {
		switch {
		case d.Status == canonical.DiffMatch && sourceWords(d.A) != "":
			out = append(out, "Both settle on "+sourceWords(d.A)+".")
		case d.Status == canonical.DiffDiffer:
			out = append(out, "They settle on different sources, which adds to the safety risk.")
		}
	}
	if d, ok := diffOf(r, "metric"); ok && d.Status == canonical.DiffDiffer && f.curFact != nil &&
		(d.A == termRateLevel || d.A == termRateChange) && (d.B == termRateLevel || d.B == termRateChange) {
		lvl, chg := va, vb
		if d.A == termRateChange {
			lvl, chg = vb, va
		}
		out = append(out, lvl+" asks for a rate level, "+chg+" for a change; the current rate ("+pctText(*f.cur)+", "+factSource(*f.curFact)+") converts one into the other.")
	}
	sa, okA := fedLevelSet(in.A.Terms, f)
	sb, okB := fedLevelSet(in.B.Terms, f)
	if okA && okB {
		na, nb := nameContract(in.A), nameContract(in.B)
		if sa.same(sb) && f.step != nil {
			out = append(out, fmt.Sprintf("%s's '%s' and %s's '%s' both mean %s, because the Fed moves in %s.",
				va, na.Short, vb, nb.Short, levelWords(sa), stepWords(*f.step)))
		} else if r.Type == canonical.RelComplement {
			out = append(out, fmt.Sprintf("%s's '%s' means %s; %s's '%s' means %s. Between them they cover every outcome once.",
				va, na.Short, levelWords(sa), vb, nb.Short, levelWords(sb)))
		}
	}
	if d, ok := diffOf(r, "deadline"); ok && d.Status == canonical.DiffDiffer {
		if gap, ok := deadlineGap(d); ok {
			out = append(out, "Their deadlines are about "+gap+" apart; that gap is part of the safety risk.")
		}
	}
	if d, ok := diffOf(r, "edge_rules"); ok && d.Status == canonical.DiffUnknown {
		unknown = "Unknown: how each venue handles edge cases"
		if in.A.Terms.Subject == termFedFunds {
			unknown += ", such as a moved meeting or a late statement"
		}
		unknown += "; this is part of the safety risk."
	}
	if r.Type == canonical.RelRelated || r.Type == canonical.RelUnrelated {
		out = vetoBullets(r)
	}
	if unknown != "" && r.Type.Routable() {
		if len(out) >= maxWhyBullets {
			out = out[:maxWhyBullets-1]
		}
		out = append(out, unknown)
	}
	if len(out) > maxWhyBullets {
		out = out[:maxWhyBullets]
	}
	return title, out
}

// levelWords words a level set briefly: "4.50% or more".
func levelWords(s levelSet) string {
	l := pctText(s.level)
	switch s.op {
	case canonical.ComparatorGTE:
		return l + " or more"
	case canonical.ComparatorLTE:
		return l + " or less"
	case canonical.ComparatorGT:
		return "above " + l
	case canonical.ComparatorLT:
		return "below " + l
	}
	return "exactly " + l
}

// factSource says where a fact comes from: "from the Fed's statement on Sep 16".
func factSource(f canonical.FactUse) string {
	d := f.AsOf.UTC().Format("Jan 2")
	if strings.Contains(f.Source, "federalreserve.gov") {
		return "from the Fed's statement on " + d
	}
	return "as of " + d
}

// deadlineGap words the gap between two stored RFC 3339 deadlines.
func deadlineGap(d canonical.FieldDiff) (string, bool) {
	a, errA := time.Parse(time.RFC3339, d.A)
	b, errB := time.Parse(time.RFC3339, d.B)
	if errA != nil || errB != nil {
		return "", false
	}
	g := b.Sub(a)
	if g < 0 {
		g = -g
	}
	switch {
	case g < time.Hour:
		return strconv.Itoa(int(g.Minutes())) + " minutes", true
	case g < 48*time.Hour:
		h := int(g.Round(time.Hour).Hours())
		return strconv.Itoa(h) + " " + plural(h, "hour", "hours"), true
	}
	days := int(g.Round(24*time.Hour).Hours() / 24)
	return strconv.Itoa(days) + " days", true
}

// vetoWords translates the matcher's veto codes for a lookalike pair.
var vetoWords = map[string]string{
	"subject_mismatch":    "They're about different things.",
	"metric_mismatch":     "They measure different things.",
	"threshold_mismatch":  "They use different strike levels.",
	"comparator_mismatch": "They point in different directions.",
	"deadline_gap":        "They settle on different dates.",
	"source_mismatch":     "They settle on different sources.",
	"polarity_unknown":    "We can't tell which side pays on one of them.",
	"unit_mismatch":       "They use different units.",
}

func vetoBullets(r canonical.Relationship) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range r.Vetoes {
		w, ok := vetoWords[v.Code]
		if !ok {
			w = "A term differs (" + strings.ReplaceAll(v.Field, "_", " ") + ")."
		}
		if !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	if len(out) == 0 {
		out = append(out, "No single rule failed, but the evidence fell short of the bar for the same bet.")
	}
	return out
}

// pairPresets are one-click orders on contract A: buy 100 YES and buy 100
// NO. They pre-fill the form; nothing is routed until "Route it".
func pairPresets(r canonical.Relationship) []pairPreset {
	if !r.Type.Routable() || r.ID <= 0 {
		return nil
	}
	venue := venueName(canonical.VenueOf(r.AID))
	out := make([]pairPreset, 0, 2)
	for _, o := range []canonical.Outcome{canonical.OutcomeYes, canonical.OutcomeNo} {
		v := url.Values{}
		v.Set("target", r.AID)
		v.Set("side", "buy")
		v.Set("outcome", string(o))
		v.Set("qty", "100")
		v.Set("type", "market")
		v.Set("tif", "IOC")
		out = append(out, pairPreset{
			Label: "Buy 100 " + strings.ToUpper(string(o)) + " on " + venue,
			Href:  "/pairs/" + strconv.FormatInt(r.ID, 10) + "?" + v.Encode() + "#route",
		})
	}
	return out
}

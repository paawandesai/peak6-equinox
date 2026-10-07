// Package eval measures the matcher: the golden-set metrics (M2-05) and the
// settlement back-test in backtest*.go (M2-06, FR-19, NFR-07).
package eval

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/facts"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/match"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/normalize"
)

// The settlement back-test (FR-19, NFR-07) is the matcher's reality check:
// run Compare on pairs of contracts that have already settled and see whether
// the payouts agreed with the verdict. For the gate only the equivalent tier
// matters (0 `equivalent` / `equivalent_given_facts` pairs that settled
// differently, or every exception investigated and logged); the derived types
// are checked too because their settlement rule is just as definite:
//
//	equivalent, equivalent_given_facts   the two outcomes are equal
//	complement                           the two outcomes differ
//	implies (X forces Y)                 never YES on X with NO on Y
//
// `related` and `unrelated` pairs make no payout claim and are only counted.

// Expectation is what a relationship type says about two settlements.
type Expectation string

// Expectations, one per checked relationship type.
const (
	ExpectSame     Expectation = "same outcome"
	ExpectOpposite Expectation = "opposite outcomes"
	ExpectForced   Expectation = "YES on the forcing side needs YES on the forced side"
)

// SettledPair is one cross-venue pair the back-test checked: both settled
// instruments, the matcher's verdict and whether the settlements agreed.
type SettledPair struct {
	A, B   canonical.Settled
	Rel    canonical.Relationship
	Expect Expectation
	// Forcing and Forced are the instrument IDs for implies; empty otherwise.
	Forcing, Forced string
	Agree           bool
	// Exception is set when the pair disagreed but is allowlisted.
	Exception *Exception
}

// Exception is one investigated back-test disagreement that is allowed to
// stand (NFR-07: "every exception investigated and logged"). Both the reason
// and the decision entry are required, so an allowlist can never silence a
// mismatch without a trace.
type Exception struct {
	AID      string `yaml:"a_id"`
	BID      string `yaml:"b_id"`
	Reason   string `yaml:"reason"`
	Decision string `yaml:"decision"` // docs/decisions.md entry, e.g. "D-072"
}

// Allowlist holds investigated exceptions keyed by the unordered pair.
type Allowlist map[string]Exception

// pairKey is the same for (a, b) and (b, a).
func pairKey(aID, bID string) string {
	if bID < aID {
		aID, bID = bID, aID
	}
	return aID + "\x00" + bID
}

// NewAllowlist validates and indexes exceptions. Every entry needs two
// distinct instrument IDs, a reason and a decision; a pair listed twice is an error.
func NewAllowlist(entries []Exception) (Allowlist, error) {
	al := Allowlist{}
	var errs []error
	for i, e := range entries {
		switch {
		case canonical.ValidateInstrumentID(e.AID) != nil || canonical.ValidateInstrumentID(e.BID) != nil:
			errs = append(errs, fmt.Errorf("exception %d: a_id %q and b_id %q must be instrument ids", i, e.AID, e.BID))
			continue
		case e.AID == e.BID:
			errs = append(errs, fmt.Errorf("exception %d: a_id and b_id must differ", i))
			continue
		case strings.TrimSpace(e.Reason) == "":
			errs = append(errs, fmt.Errorf("exception %d (%s, %s): reason is required", i, e.AID, e.BID))
			continue
		case !decisionID.MatchString(e.Decision):
			errs = append(errs, fmt.Errorf("exception %d (%s, %s): decision %q must be a docs/decisions.md id such as D-072", i, e.AID, e.BID, e.Decision))
			continue
		}
		k := pairKey(e.AID, e.BID)
		if _, dup := al[k]; dup {
			errs = append(errs, fmt.Errorf("exception %d (%s, %s): pair listed twice", i, e.AID, e.BID))
			continue
		}
		al[k] = e
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return al, nil
}

// decisionID is the shape of a docs/decisions.md entry id.
var decisionID = regexp.MustCompile(`^D-[0-9]{3,}$`)

// maxAllowlistBytes caps the allowlist file; it is operator data, never large.
const maxAllowlistBytes = 1 << 20

// allowlistFile is the strict YAML layout of an allowlist file.
type allowlistFile struct {
	Exceptions []Exception `yaml:"exceptions"`
}

// LoadAllowlist reads a YAML file of the form
//
//	exceptions:
//	  - a_id: kalshi:KX...
//	    b_id: polymarket:123...
//	    reason: why the disagreement is not a matcher error
//	    decision: D-0NN
//
// Unknown keys are errors; an empty `exceptions: []` is allowed.
func LoadAllowlist(path string) (Allowlist, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("reading allowlist: %w", err)
	}
	if info.Size() > maxAllowlistBytes {
		return nil, fmt.Errorf("reading allowlist %s: %d bytes exceeds the %d byte cap", filepath.Base(path), info.Size(), maxAllowlistBytes)
	}
	data, err := os.ReadFile(path) //nolint:gosec // operator-chosen path, size-capped above
	if err != nil {
		return nil, fmt.Errorf("reading allowlist: %w", err)
	}
	return ParseAllowlist(data)
}

// ParseAllowlist is LoadAllowlist over bytes.
func ParseAllowlist(data []byte) (Allowlist, error) {
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	var f allowlistFile
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("parsing allowlist: %w", err)
	}
	if f.Exceptions == nil {
		return nil, errors.New("parsing allowlist: top-level `exceptions` list is required (use `exceptions: []` for none)")
	}
	al, err := NewAllowlist(f.Exceptions)
	if err != nil {
		return nil, fmt.Errorf("parsing allowlist: %w", err)
	}
	return al, nil
}

// Rejected is one settled record the back-test could not use.
type Rejected struct {
	ID     string
	Reason string
}

// BacktestReport is the result of one back-test run.
type BacktestReport struct {
	// Instruments counts the distinct valid settled instruments per venue.
	Instruments map[canonical.Venue]int
	// Rejected lists settled records that failed validation or duplicated an ID.
	Rejected []Rejected
	// Pairs is the number of cross-venue pairs compared; ByType is their histogram.
	Pairs  int
	ByType map[canonical.RelationshipType]int
	// Checked lists every pair of a checked type (equivalent tier, complement,
	// implies), in a stable order.
	Checked []SettledPair
	// FactIDs are the ids of the facts the matcher had available.
	FactIDs []string
}

// NormalizeSettled re-extracts every instrument's Terms with the normaliser,
// as `equinox match` does, so the back-test does not depend on what an
// adapter stored. Callers that build Terms themselves (tests) skip it.
func NormalizeSettled(settled []canonical.Settled) []canonical.Settled {
	out := make([]canonical.Settled, len(settled))
	for i, s := range settled {
		s.Instrument = normalize.Normalize(s.Instrument)
		out[i] = s
	}
	return out
}

// Backtest compares every cross-venue pair of settled instruments in the
// same category and checks each verdict against the settlements. Invalid
// records and duplicate IDs are listed in Rejected and skipped. Pass the
// instruments through NormalizeSettled first unless their Terms are already
// what the matcher should see.
func Backtest(settled []canonical.Settled, f facts.Set, allow Allowlist) BacktestReport {
	r := BacktestReport{
		Instruments: map[canonical.Venue]int{},
		ByType:      map[canonical.RelationshipType]int{},
		FactIDs:     f.IDs(),
	}
	byVenue := map[canonical.Venue][]canonical.Settled{}
	seen := map[string]bool{}
	for _, s := range settled {
		if err := s.Validate(); err != nil {
			r.Rejected = append(r.Rejected, Rejected{ID: s.Instrument.ID, Reason: err.Error()})
			continue
		}
		if seen[s.Instrument.ID] {
			r.Rejected = append(r.Rejected, Rejected{ID: s.Instrument.ID, Reason: "duplicate id"})
			continue
		}
		seen[s.Instrument.ID] = true
		byVenue[s.Instrument.Venue] = append(byVenue[s.Instrument.Venue], s)
		r.Instruments[s.Instrument.Venue]++
	}
	for _, v := range canonical.Venues() {
		sort.Slice(byVenue[v], func(i, j int) bool { return byVenue[v][i].Instrument.ID < byVenue[v][j].Instrument.ID })
	}
	venues := canonical.Venues() // stable: kalshi before polymarket
	for i := 0; i < len(venues); i++ {
		for j := i + 1; j < len(venues); j++ {
			for _, a := range byVenue[venues[i]] {
				for _, b := range byVenue[venues[j]] {
					if a.Instrument.Category != b.Instrument.Category {
						continue
					}
					rel := match.Compare(a.Instrument, b.Instrument, f)
					r.Pairs++
					r.ByType[rel.Type]++
					if p, ok := check(a, b, rel, allow); ok {
						r.Checked = append(r.Checked, p)
					}
				}
			}
		}
	}
	return r
}

// impliesDirection reads which side forces which from the matcher's
// explanation ("implies: YES on X forces YES on Y, not the reverse").
var impliesDirection = regexp.MustCompile(`YES on ([^\s,]+) forces YES on ([^\s,]+)`)

// check evaluates one verdict against the settlements; ok is false for types
// that make no payout claim.
func check(a, b canonical.Settled, rel canonical.Relationship, allow Allowlist) (SettledPair, bool) {
	p := SettledPair{A: a, B: b, Rel: rel}
	switch rel.Type {
	case canonical.RelEquivalent, canonical.RelEquivalentGivenFacts:
		p.Expect = ExpectSame
		p.Agree = a.Outcome == b.Outcome
	case canonical.RelComplement:
		p.Expect = ExpectOpposite
		p.Agree = a.Outcome != b.Outcome
	case canonical.RelImplies:
		p.Expect = ExpectForced
		m := impliesDirection.FindStringSubmatch(rel.Explanation)
		if m == nil {
			// The direction is part of the matcher's contract; without it
			// the claim cannot be checked, which is itself a disagreement.
			p.Agree = false
			break
		}
		p.Forcing, p.Forced = m[1], m[2]
		forcing, forced := a, b
		if p.Forcing == b.Instrument.ID {
			forcing, forced = b, a
		}
		p.Agree = forcing.Outcome != canonical.OutcomeYes || forced.Outcome != canonical.OutcomeNo
	default:
		return SettledPair{}, false
	}
	if !p.Agree {
		if e, ok := allow[pairKey(a.Instrument.ID, b.Instrument.ID)]; ok {
			p.Exception = &e
		}
	}
	return p, true
}

// gate reports whether a pair counts towards the NFR-07 gate.
func gate(p SettledPair) bool {
	return p.Rel.Type == canonical.RelEquivalent || p.Rel.Type == canonical.RelEquivalentGivenFacts
}

// Mismatches returns the checked pairs that disagreed and are not allowlisted.
// equivalentOnly restricts it to the NFR-07 gate (the equivalent tier).
func (r BacktestReport) Mismatches(equivalentOnly bool) []SettledPair {
	var out []SettledPair
	for _, p := range r.Checked {
		if p.Agree || p.Exception != nil {
			continue
		}
		if equivalentOnly && !gate(p) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// Exceptions returns the checked pairs that disagreed but are allowlisted.
func (r BacktestReport) Exceptions() []SettledPair {
	var out []SettledPair
	for _, p := range r.Checked {
		if !p.Agree && p.Exception != nil {
			out = append(out, p)
		}
	}
	return out
}

// Passed reports the NFR-07 gate: no unallowlisted disagreement on the
// equivalent tier.
func (r BacktestReport) Passed() bool { return len(r.Mismatches(true)) == 0 }

// checkedTypes is the order the report lists the checked types in.
var checkedTypes = []canonical.RelationshipType{
	canonical.RelEquivalent, canonical.RelEquivalentGivenFacts, canonical.RelComplement, canonical.RelImplies,
}

// String renders the report as Markdown that reads in a terminal and can be
// pasted into docs/equivalence.md §8 as is.
func (r BacktestReport) String() string {
	var b strings.Builder
	eqMismatches := r.Mismatches(true)
	allMismatches := r.Mismatches(false)
	exceptions := r.Exceptions()
	fmt.Fprintf(&b, "Settlement back-test: %d settled instruments (kalshi %d, polymarket %d), %d cross-venue pairs compared, %d rejected records; facts available: %s\n",
		r.Instruments[canonical.VenueKalshi]+r.Instruments[canonical.VenuePolymarket],
		r.Instruments[canonical.VenueKalshi], r.Instruments[canonical.VenuePolymarket], r.Pairs, len(r.Rejected), joinOrNone(r.FactIDs))
	fmt.Fprintf(&b, "Gate (NFR-07): %d equivalent-tier pairs settled differently", len(eqMismatches))
	if len(exceptions) > 0 {
		fmt.Fprintf(&b, " (%d allowlisted exceptions)", len(exceptions))
	}
	if r.Passed() {
		b.WriteString(" — PASS\n\n")
	} else {
		b.WriteString(" — FAIL\n\n")
	}
	b.WriteString("| Type | Pairs | Checked | Agreed | Disagreed | Allowlisted | Settlement rule |\n|---|---|---|---|---|---|---|\n")
	for _, t := range checkedTypes {
		var checked, agreed, disagreed, allowed int
		expect := Expectation("")
		for _, p := range r.Checked {
			if p.Rel.Type != t {
				continue
			}
			checked++
			expect = p.Expect
			switch {
			case p.Agree:
				agreed++
			case p.Exception != nil:
				allowed++
			default:
				disagreed++
			}
		}
		if expect == "" {
			expect = expectationOf(t)
		}
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %d | %s |\n", t, r.ByType[t], checked, agreed, disagreed, allowed, expect)
	}
	fmt.Fprintf(&b, "| related | %d | – | – | – | – | no payout claim |\n", r.ByType[canonical.RelRelated])
	fmt.Fprintf(&b, "| unrelated | %d | – | – | – | – | no payout claim |\n", r.ByType[canonical.RelUnrelated])
	if len(allMismatches) > 0 {
		b.WriteString("\nDisagreements (investigate each; allowlist only with a logged decision):\n")
		for _, p := range allMismatches {
			b.WriteString(describe(p))
		}
	}
	if len(exceptions) > 0 {
		b.WriteString("\nAllowlisted exceptions:\n")
		for _, p := range exceptions {
			b.WriteString(describe(p))
			fmt.Fprintf(&b, "  allowed by %s: %s\n", p.Exception.Decision, p.Exception.Reason)
		}
	}
	if agreed := agreedPairs(r.Checked); len(agreed) > 0 {
		b.WriteString("\nAgreed pairs:\n")
		for _, p := range agreed {
			b.WriteString(describe(p))
		}
	}
	if len(r.Rejected) > 0 {
		fmt.Fprintf(&b, "\nRejected records (%d):\n", len(r.Rejected))
		for _, x := range r.Rejected {
			fmt.Fprintf(&b, "- %s: %s\n", x.ID, x.Reason)
		}
	}
	return b.String()
}

// describe renders one checked pair as a list item.
func describe(p SettledPair) string {
	verdict := string(p.Rel.Type)
	if p.Rel.Type == canonical.RelImplies && p.Forcing != "" {
		verdict = fmt.Sprintf("implies (%s forces %s)", p.Forcing, p.Forced)
	}
	status := "agreed"
	if !p.Agree {
		status = "DISAGREED"
	}
	return fmt.Sprintf("- %s: %s settled %s, %s settled %s (confidence %s; expected %s): %s\n",
		verdict, p.A.Instrument.ID, p.A.Outcome, p.B.Instrument.ID, p.B.Outcome, p.Rel.Confidence.String(), p.Expect, status) +
		fmt.Sprintf("  %s | %s\n", clip(p.A.Instrument.Title), clip(p.B.Instrument.Title))
}

// agreedPairs filters the checked pairs that agreed, strongest type first.
func agreedPairs(ps []SettledPair) []SettledPair {
	var out []SettledPair
	for _, t := range checkedTypes {
		for _, p := range ps {
			if p.Agree && p.Rel.Type == t {
				out = append(out, p)
			}
		}
	}
	return out
}

// expectationOf is the rule for a checked type with no pairs to read it from.
func expectationOf(t canonical.RelationshipType) Expectation {
	switch t {
	case canonical.RelComplement:
		return ExpectOpposite
	case canonical.RelImplies:
		return ExpectForced
	default:
		return ExpectSame
	}
}

// maxTitle is how much of a title the report shows.
const maxTitle = 90

// clip shortens a title for the report and keeps it on one line.
func clip(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxTitle {
		return s[:maxTitle-1] + "…"
	}
	return s
}

func joinOrNone(ids []string) string {
	if len(ids) == 0 {
		return "none"
	}
	return strings.Join(ids, ", ")
}

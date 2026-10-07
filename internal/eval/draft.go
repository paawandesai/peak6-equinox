package eval

import (
	"fmt"
	"sort"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/match"
)

// DefaultDraftSize is how many candidates `equinox golden draft` proposes
// (D-023: about 40 real pairs).
const DefaultDraftSize = 40

// Pair is one compared cross-venue pair with the matcher's rules verdict.
type Pair struct {
	A, B canonical.Instrument
	Rel  canonical.Relationship
}

// bucket is one selection rule. Pairs are grouped by the first bucket that
// accepts them; a draft takes `weight` pairs per round-robin turn from each
// bucket, and at most `cap` in total when cap is positive (D-069).
type bucket struct {
	name   string
	why    string
	accept func(Pair) bool
	less   func(a, b Pair) bool
	weight int
	capDiv int // cap = n / capDiv; 0 means no cap
}

// buckets lists the selection rules, strongest first. The equivalent tier gets
// two picks per turn because its precision is the exit criterion (NFR-06);
// plain related pairs and unrelated pairs are hard negatives, capped so they
// do not crowd out the pairs a human must check.
var buckets = []bucket{
	{name: "equivalent", why: "rules say equivalent: the precision-critical tier (NFR-06)",
		accept: isType(canonical.RelEquivalent), less: byConfidence, weight: 2},
	{name: "equivalent_given_facts", why: "rules say equivalent given a fact: check the fact and the arithmetic",
		accept: isType(canonical.RelEquivalentGivenFacts), less: byConfidence, weight: 2},
	{name: "complement", why: "rules say complement: YES on A should pay exactly when NO on B pays",
		accept: isType(canonical.RelComplement), less: byConfidence, weight: 1},
	{name: "implies", why: "rules say implies: YES on one side should force YES on the other",
		accept: isType(canonical.RelImplies), less: byConfidence, weight: 1},
	{name: "near_miss", why: "related near miss: thresholds within 0.1 % or a fact was consulted; confirm it is not equivalent",
		accept: func(p Pair) bool { return match.NearMiss(p.Rel) }, less: byFewestVetoes, weight: 1},
	{name: "related", why: "related with the fewest vetoes: a hard negative",
		accept: isType(canonical.RelRelated), less: byFewestVetoes, weight: 1, capDiv: 8},
	{name: "unrelated", why: "unrelated with the most matching terms: a hard negative",
		accept: isType(canonical.RelUnrelated), less: byMostMatches, weight: 1, capDiv: 8},
}

// Draft selects up to n candidate pairs from pairs for the human to label.
// The selection is deterministic for a given input order. n <= 0 means
// DefaultDraftSize. Fewer than n candidates come back when the round has
// fewer pairs worth a look.
func Draft(roundID int64, pairs []Pair, n int, now time.Time) Candidates {
	if n <= 0 {
		n = DefaultDraftSize
	}
	groups := make([][]Pair, len(buckets))
	for _, p := range pairs {
		for i, b := range buckets {
			if b.accept(p) {
				groups[i] = append(groups[i], p)
				break
			}
		}
	}
	for i, b := range buckets {
		less := b.less
		sort.SliceStable(groups[i], func(x, y int) bool { return less(groups[i][x], groups[i][y]) })
	}
	caps := make([]int, len(buckets))
	for i, b := range buckets {
		caps[i] = len(groups[i])
		if b.capDiv > 0 && n/b.capDiv < caps[i] {
			caps[i] = n / b.capDiv
		}
	}
	pk := picker{groups: groups, used: make([][]bool, len(groups)), taken: make([]int, len(groups)), seen: map[string]bool{}}
	for i := range groups {
		pk.used[i] = make([]bool, len(groups[i]))
	}
	// Round-robin within caps, then fill from the capped buckets if short.
	for progress := true; progress && len(pk.picked) < n; {
		progress = false
		for i, b := range buckets {
			for k := 0; k < b.weight && len(pk.picked) < n && pk.taken[i] < caps[i]; k++ {
				pk.take(i)
				progress = true
			}
		}
	}
	for i := range buckets {
		for len(pk.picked) < n && pk.taken[i] < len(groups[i]) {
			pk.take(i)
		}
	}
	picked := pk.picked
	out := Candidates{Version: Version, RoundID: roundID, DraftedAt: now.UTC().Truncate(time.Second), Method: canonical.MethodRules, Candidates: []Candidate{}}
	for _, p := range picked {
		out.Candidates = append(out.Candidates, candidate(p, whyFor(p)))
	}
	return out
}

// picker takes pairs from sorted buckets, preferring a pair whose
// instruments have not been picked yet so the draft covers many markets
// rather than one strike against a whole ladder.
type picker struct {
	groups [][]Pair
	used   [][]bool
	taken  []int
	seen   map[string]bool
	picked []Pair
}

// take picks the next pair from bucket i: the first unused pair with two
// unseen instruments, else the first unused pair. The caller checks capacity.
func (p *picker) take(i int) {
	group, used := p.groups[i], p.used[i]
	choice := -1
	for j := range group {
		if used[j] {
			continue
		}
		if choice < 0 {
			choice = j
		}
		if !p.seen[group[j].A.ID] && !p.seen[group[j].B.ID] {
			choice = j
			break
		}
	}
	used[choice] = true
	p.taken[i]++
	p.seen[group[choice].A.ID], p.seen[group[choice].B.ID] = true, true
	p.picked = append(p.picked, group[choice])
}

// whyFor returns the selection reason of the first bucket accepting p.
func whyFor(p Pair) string {
	for _, b := range buckets {
		if b.accept(p) {
			return b.why
		}
	}
	return "selected"
}

func candidate(p Pair, why string) Candidate {
	c := Candidate{
		AID: p.A.ID, ATitle: p.A.Title, BID: p.B.ID, BTitle: p.B.Title,
		Category: p.A.Category, Suggested: p.Rel.Type, Confidence: p.Rel.Confidence.String(),
		Why: why, Vetoes: []string{}, Explanation: p.Rel.Explanation,
	}
	for _, v := range p.Rel.Vetoes {
		c.Vetoes = append(c.Vetoes, fmt.Sprintf("%s %s: %s", v.Code, v.Field, v.Detail))
	}
	for _, f := range p.Rel.FactsUsed {
		c.Facts = append(c.Facts, f.Key)
	}
	return c
}

// CountByType tallies the suggested types, for the draft summary.
func (c Candidates) CountByType() map[canonical.RelationshipType]int {
	out := map[canonical.RelationshipType]int{}
	for _, e := range c.Candidates {
		out[e.Suggested]++
	}
	return out
}

func isType(t canonical.RelationshipType) func(Pair) bool {
	return func(p Pair) bool { return p.Rel.Type == t }
}

// byConfidence orders by confidence descending, then ids.
func byConfidence(a, b Pair) bool {
	if c := a.Rel.Confidence.Cmp(b.Rel.Confidence); c != 0 {
		return c > 0
	}
	return byIDs(a, b)
}

// byFewestVetoes orders by veto count ascending (closest to equivalent first).
func byFewestVetoes(a, b Pair) bool {
	if la, lb := len(a.Rel.Vetoes), len(b.Rel.Vetoes); la != lb {
		return la < lb
	}
	return byConfidence(a, b)
}

// byMostMatches orders by the number of matching diff fields descending.
func byMostMatches(a, b Pair) bool {
	if ma, mb := matches(a.Rel), matches(b.Rel); ma != mb {
		return ma > mb
	}
	return byIDs(a, b)
}

func matches(rel canonical.Relationship) int {
	n := 0
	for _, d := range rel.Diff {
		if d.Status == canonical.DiffMatch {
			n++
		}
	}
	return n
}

func byIDs(a, b Pair) bool {
	if a.A.ID != b.A.ID {
		return a.A.ID < b.A.ID
	}
	return a.B.ID < b.B.ID
}

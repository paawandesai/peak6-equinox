package web

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// evalExample is one human-labelled pair from testdata/golden/labels.yaml
// that /eval shows next to its type (P11). TestEvalExamplesAreLabelled
// fails if an entry is not in labels.yaml with that label.
type evalExample struct {
	AID, BID string
	Desc     string // what the pair is, in plain words, from the label's note
}

// evalExamples are up to two labelled pairs per type, picked from contracts
// that are still listed so the pinned round is likely to hold them.
var evalExamples = map[canonical.RelationshipType][]evalExample{
	canonical.RelEquivalent: {
		{"kalshi:KXPRESNOMD-28-GN", "polymarket:54533043819946592547517511176940999955633860128497669742211153063842200957669",
			"Newsom wins the 2028 Democratic nomination, on both venues"},
		{"kalshi:KXPRESNOMR-28-JDV", "polymarket:40081275558852222228080198821361202017557872256707631666334039001378518619916",
			"Vance wins the 2028 Republican nomination, on both venues"},
	},
	canonical.RelEquivalentGivenFacts: {
		{"kalshi:KXFED-26OCT-T4.25", "polymarket:18246296022868501258060085368606280205967114341141276439803424448463050819498",
			"Fed upper bound above 4.25% after October vs a 50+ bp October hike, given the current upper bound"},
		{"kalshi:KXFEDDECISION-26OCT-H0", "polymarket:111061902544814266207267295505639408607400625795891618462682726460921782993748",
			"No change at the October 2026 FOMC meeting, on both venues"},
	},
	canonical.RelComplement: {
		{"kalshi:KXFED-26OCT-T3.50", "polymarket:17010377994663817312158123655937348199252960045746746128731746451645055725586",
			"Fed upper bound above 3.50% after October vs a 50+ bp October cut: on the 25 bp grid one pays exactly when the other does not"},
	},
	canonical.RelImplies: {
		{"kalshi:KXFED-26OCT-T3.00", "polymarket:33268510350568915228897273528353467773422190590579607471427765079749830445947",
			"A 25 bp October cut forces the upper bound to stay above 3.00%, not the reverse"},
		{"kalshi:KXPRESPERSON-28-JVAN", "polymarket:40081275558852222228080198821361202017557872256707631666334039001378518619916",
			"Vance inaugurated in 2029 forces Vance winning the Republican nomination, not the reverse"},
	},
	canonical.RelRelated: {
		{"kalshi:KXFED-26DEC-T2.75", "polymarket:10021099765479877880206130084102113471770375449510519368096296649508325006675",
			"Same December decision, but the upper bound landing exactly on 2.75% and being above 2.75% exclude each other without being complements"},
		{"kalshi:KXPRESNOMR-28-JDV", "polymarket:13565458761220145250977753098276900790902214604876327357986816739576288755859",
			"Vance vs Rubio for the Republican nomination: both cannot win, so the payouts are linked"},
	},
	canonical.RelUnrelated: {
		{"kalshi:KXFED-26DEC-T3.50", "polymarket:13469338817388460729834022714290521037522577154587934145079627411549531574027",
			"Fed upper bound after December 2026 vs after December 2027: different decisions a year apart"},
		{"kalshi:KXFED-26DEC-T3.75", "polymarket:61078851078420927336908319350034407566038974210410444154944749944889389332268",
			"Same shape: December 2026 vs December 2027 decisions"},
	},
}

// evalExampleLink is an example ready to render: PairID is the pair in the
// current round, or 0 when that round has no stored pair for it.
type evalExampleLink struct {
	Desc   string
	AID    string
	PairID int64
}

// evalExampleLinks resolves each example to its pair in round roundID by
// instrument ids (pair ids are per round, never hard-coded). With no store
// or round, the examples render without links.
func (s *Server) evalExampleLinks(ctx context.Context, roundID int64) map[string][]evalExampleLink {
	out := make(map[string][]evalExampleLink, len(evalExamples))
	for typ, exs := range evalExamples {
		for _, ex := range exs {
			out[string(typ)] = append(out[string(typ)], evalExampleLink{Desc: ex.Desc, AID: ex.AID, PairID: s.findPairID(ctx, roundID, ex.AID, ex.BID)})
		}
	}
	return out
}

// findPairID returns the id of the stored relationship between aID and bID
// in roundID, or 0 when there is none or the lookup fails.
func (s *Server) findPairID(ctx context.Context, roundID int64, aID, bID string) int64 {
	if s.deps.Store == nil || roundID == 0 {
		return 0
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	rels, err := s.deps.Store.ListRelationships(ctx, store.RelationshipFilter{RoundID: roundID, InstrumentID: aID, Limit: 200})
	if err != nil {
		s.log.Warn("resolving eval example pair", "round", roundID, "a_id", aID, "err", err)
		return 0
	}
	for _, rel := range rels {
		if (rel.AID == aID && rel.BID == bID) || (rel.AID == bID && rel.BID == aID) {
			return rel.ID
		}
	}
	return 0
}

// evalUnscored is one labelled pair the eval could not score, with why.
type evalUnscored struct {
	AID, BID string
	Reason   string
}

// AShort and BShort keep long Polymarket token ids readable on a phone;
// the template puts the full id in the title.
func (u evalUnscored) AShort() string { return shortInstrumentID(u.AID) }
func (u evalUnscored) BShort() string { return shortInstrumentID(u.BID) }

// shortInstrumentID abbreviates an id longer than 40 characters (a
// Polymarket token id) to its first 22 and last 6.
func shortInstrumentID(id string) string {
	if len(id) <= 40 {
		return id
	}
	return id[:22] + "…" + id[len(id)-6:]
}

// notFoundSuffix marks an `equinox eval` note for a label whose instruments
// were not in the database it ran on (internal/eval/summary.go).
const notFoundSuffix = " (instrument not found)"

// unscoredLabels lists every "not evaluated" note with a plain reason.
func unscoredLabels(notes []string, generatedAt time.Time) []evalUnscored {
	var out []evalUnscored
	for _, note := range notes {
		if !strings.HasPrefix(note, "not evaluated: ") || !strings.HasSuffix(note, notFoundSuffix) {
			continue
		}
		ids := strings.TrimSuffix(strings.TrimPrefix(note, "not evaluated: "), notFoundSuffix)
		a, b, ok := strings.Cut(ids, " / ")
		if !ok {
			continue
		}
		out = append(out, evalUnscored{AID: a, BID: b, Reason: unscoredReason(a, b, generatedAt)})
	}
	return out
}

// unscoredReason explains a missing instrument. When a Kalshi ticker's
// date code is before the run, it says the contract had already resolved.
func unscoredReason(a, b string, generatedAt time.Time) string {
	base := "At least one of the two contracts is not in the database the eval ran on, so the matcher had no terms to classify."
	for _, id := range []string{a, b} {
		if d, ok := kalshiTickerDate(id); ok && !generatedAt.IsZero() && d.Before(generatedAt) {
			return fmt.Sprintf("The Kalshi contract's ticker date is %s, before this run: it had resolved and is not in the database the eval ran on, so the matcher had no terms to classify.", d.Format("2 Jan 2006"))
		}
	}
	return base
}

// kalshiDateCode matches the YYMMMDD date code in a Kalshi event ticker,
// e.g. "26OCT08" in "KXBTCD-26OCT0812-T77999.99".
var kalshiDateCode = regexp.MustCompile(`^kalshi:[A-Z0-9]+-(\d{2}[A-Z]{3}\d{2})`)

// kalshiTickerDate reads the calendar date a Kalshi ticker encodes.
func kalshiTickerDate(id string) (time.Time, bool) {
	m := kalshiDateCode.FindStringSubmatch(id)
	if m == nil {
		return time.Time{}, false
	}
	code := m[1][:2] + m[1][2:3] + strings.ToLower(m[1][3:5]) + m[1][5:]
	d, err := time.Parse("06Jan02", code)
	if err != nil {
		return time.Time{}, false
	}
	return d, true
}

// evaluatedPairs is how many labels the eval scored: the sum of supports.
func evaluatedPairs(types []evalTypeMetrics) int {
	n := 0
	for _, m := range types {
		n += m.Support
	}
	return n
}

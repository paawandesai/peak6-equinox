package match_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gopkg.in/yaml.v3"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/facts"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/match"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/normalize"
)

// The pair suite: every pair is normalised and compared with the pinned
// facts in testdata/match/facts.yaml. Ids say what a pair must be:
//
//	EQ-    equivalent            FACT-  equivalent_given_facts
//	CMP-   complement            IMP-   implies
//	V-     the veto suite: never equivalent or equivalent_given_facts
//	       (NFR-06: 0 false positives), and complement / implies only when
//	       the case says so.
var pairFiles = []string{"../../testdata/match/crypto_pairs.yaml", "../../testdata/match/fed_pairs.yaml"}

const factsFile = "../../testdata/match/facts.yaml"

// factsNow is the clock the fixture facts are judged against.
var factsNow = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

type pairSide struct {
	Venue    canonical.Venue    `yaml:"venue"`
	Key      string             `yaml:"market_id"`
	Category canonical.Category `yaml:"category"`
	Close    time.Time          `yaml:"close"`
	Title    string             `yaml:"title"`
	Rules    string             `yaml:"rules"`
}

type pairCase struct {
	ID      string                     `yaml:"id"`
	Name    string                     `yaml:"name"`
	Expect  canonical.RelationshipType `yaml:"expect"`
	Vetoes  []string                   `yaml:"vetoes"`
	Blocked string                     `yaml:"blocked"`
	Facts   []string                   `yaml:"facts"`          // fact ids that must appear in facts_used, in order
	MinConf string                     `yaml:"min_confidence"` // routable floor; default 0.9
	Explain []string                   `yaml:"explain"`        // substrings the explanation must contain
	A       pairSide                   `yaml:"a"`
	B       pairSide                   `yaml:"b"`
}

func loadPairs(t *testing.T) []pairCase {
	t.Helper()
	var all []pairCase
	for _, file := range pairFiles {
		raw, err := os.ReadFile(filepath.Clean(file))
		if err != nil {
			t.Fatalf("reading pairs: %v", err)
		}
		var doc struct {
			Pairs []pairCase `yaml:"pairs"`
		}
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("decoding %s: %v", file, err)
		}
		if len(doc.Pairs) == 0 {
			t.Fatalf("no pairs in %s", file)
		}
		all = append(all, doc.Pairs...)
	}
	return all
}

func loadFacts(t *testing.T) facts.Set {
	t.Helper()
	fs, err := facts.LoadAt(factsFile, factsNow)
	if err != nil {
		t.Fatalf("loading facts: %v", err)
	}
	return fs
}

// instrumentOf builds a canonical instrument the way an adapter would, then
// normalises it the way the collector does.
func instrumentOf(t *testing.T, s pairSide) canonical.Instrument {
	t.Helper()
	cat := s.Category
	if cat == "" {
		cat = canonical.CategoryCrypto
	}
	fee := canonical.FeeSchedule{ID: canonical.FeeScheduleKalshiTakerV1, Params: map[string]decimal.Decimal{"multiplier": decimal.RequireFromString("0.07")}}
	if s.Venue == canonical.VenuePolymarket {
		fee = canonical.FeeSchedule{ID: canonical.FeeSchedulePolymarketV1}
	}
	in := canonical.Instrument{
		ID: canonical.InstrumentID(s.Venue, s.Key), Venue: s.Venue, VenueMarketID: s.Key, VenueEventID: "EVT", Category: cat,
		Title: s.Title, RulesText: s.Rules, Terms: canonical.UnknownTerms(), CloseTime: s.Close.UTC(),
		Status: canonical.StatusOpen, FeeSchedule: fee,
	}
	in = normalize.Normalize(in)
	if err := in.Validate(); err != nil {
		t.Fatalf("instrument %s: %v", in.ID, err)
	}
	return in
}

func prefixOf(id string) string {
	p, _, _ := strings.Cut(id, "-")
	return p
}

func TestPairsFromFixtures(t *testing.T) {
	fs := loadFacts(t)
	counts := map[string]int{}
	for _, pc := range loadPairs(t) {
		counts[prefixOf(pc.ID)]++
		t.Run(pc.ID+"_"+pc.Name, func(t *testing.T) {
			a, b := instrumentOf(t, pc.A), instrumentOf(t, pc.B)
			rel := match.Compare(a, b, fs)
			rel.RoundID = 1
			if err := rel.Validate(); err != nil {
				t.Fatalf("relationship invalid: %v", err)
			}
			if rel.Type != pc.Expect {
				t.Errorf("type = %s, want %s\n  a=%+v\n  b=%+v\n  vetoes=%v\n  %s", rel.Type, pc.Expect, a.Terms, b.Terms, rel.Vetoes, rel.Explanation)
			}
			codes := map[string]bool{}
			for _, v := range rel.Vetoes {
				codes[v.Code] = true
				if !strings.Contains(rel.Explanation, v.Field) && rel.Type != canonical.RelUnrelated {
					t.Errorf("explanation %q does not name vetoed field %q", rel.Explanation, v.Field)
				}
			}
			for _, want := range pc.Vetoes {
				if !codes[want] {
					t.Errorf("vetoes %v lack %q", rel.Vetoes, want)
				}
			}
			if pc.Blocked != "" && !strings.Contains(rel.Explanation, "blocked on "+pc.Blocked) {
				t.Errorf("explanation %q does not say blocked on %s", rel.Explanation, pc.Blocked)
			}
			if len(rel.Diff) != len(match.TermFields) {
				t.Errorf("diff has %d fields, want %d", len(rel.Diff), len(match.TermFields))
			}
			if rel.FactsUsed == nil {
				t.Error("facts_used is nil")
			}
			if len(rel.FactsUsed) != len(pc.Facts) {
				t.Errorf("facts used = %v, want ids %v", rel.FactsUsed, pc.Facts)
			}
			for i, id := range pc.Facts {
				if i >= len(rel.FactsUsed) {
					break
				}
				fu := rel.FactsUsed[i]
				if fu.Key != id || fu.Value == "" || fu.Source == "" || fu.AsOf.IsZero() {
					t.Errorf("facts_used[%d] = %+v, want %s with value, source and as_of", i, fu, id)
				}
				if !strings.Contains(rel.Explanation, id) {
					t.Errorf("explanation %q does not name fact %s", rel.Explanation, id)
				}
			}
			for _, want := range pc.Explain {
				if !strings.Contains(rel.Explanation, want) {
					t.Errorf("explanation %q lacks %q", rel.Explanation, want)
				}
			}
			// Basis risk exists exactly for routable and implies verdicts.
			scored := rel.Type.Routable() || rel.Type == canonical.RelImplies
			if (rel.BasisRisk != nil) != scored {
				t.Errorf("basis risk present=%v for %s", rel.BasisRisk != nil, rel.Type)
			}
			if rel.BasisRisk != nil && len(rel.BasisRisk.Components) < 4 {
				t.Errorf("basis risk components = %+v", rel.BasisRisk.Components)
			}
			if strings.HasPrefix(pc.ID, "V-") {
				if rel.Type == canonical.RelEquivalent || rel.Type == canonical.RelEquivalentGivenFacts {
					t.Fatalf("VETO SUITE FALSE POSITIVE: %s classified %s", pc.ID, rel.Type)
				}
				if rel.Type.Routable() && rel.Type != pc.Expect {
					t.Fatalf("VETO SUITE FALSE POSITIVE: %s classified routable %s", pc.ID, rel.Type)
				}
			}
		})
	}
	for prefix, min := range map[string]int{"EQ": 3, "V": 25, "CMP": 2, "IMP": 2, "FACT": 1} {
		if counts[prefix] < min {
			t.Errorf("only %d %s- pairs, want at least %d", counts[prefix], prefix, min)
		}
	}
}

// TestPairSuiteSymmetric: swapping A and B never changes the verdict.
func TestPairSuiteSymmetric(t *testing.T) {
	fs := loadFacts(t)
	for _, pc := range loadPairs(t) {
		a, b := instrumentOf(t, pc.A), instrumentOf(t, pc.B)
		ab, ba := match.Compare(a, b, fs), match.Compare(b, a, fs)
		if ab.Type != ba.Type || !ab.Confidence.Equal(ba.Confidence) || len(ab.Vetoes) != len(ba.Vetoes) || len(ab.FactsUsed) != len(ba.FactsUsed) {
			t.Errorf("%s: a/b %s %s %d vetoes; b/a %s %s %d vetoes", pc.ID, ab.Type, ab.Confidence, len(ab.Vetoes), ba.Type, ba.Confidence, len(ba.Vetoes))
		}
		if (ab.BasisRisk == nil) != (ba.BasisRisk == nil) || (ab.BasisRisk != nil && !ab.BasisRisk.Score.Equal(ba.BasisRisk.Score)) {
			t.Errorf("%s: basis risk differs by order: %+v vs %+v", pc.ID, ab.BasisRisk, ba.BasisRisk)
		}
	}
}

// TestPairSuiteWithoutFacts: with no facts, no pair is equivalent_given_facts
// and the explanation names the missing fact.
func TestPairSuiteWithoutFacts(t *testing.T) {
	for _, pc := range loadPairs(t) {
		rel := match.Compare(instrumentOf(t, pc.A), instrumentOf(t, pc.B), facts.Set{})
		if rel.Type == canonical.RelEquivalentGivenFacts || len(rel.FactsUsed) != 0 {
			t.Errorf("%s without facts: %s %v", pc.ID, rel.Type, rel.FactsUsed)
		}
		if pc.Expect == canonical.RelEquivalentGivenFacts && (rel.Type != canonical.RelRelated || !strings.Contains(rel.Explanation, match.FactFedUpperBound)) {
			t.Errorf("%s without facts: %s %q", pc.ID, rel.Type, rel.Explanation)
		}
	}
}

// TestRoutablePairsConfidence: routable verdicts carry a high, bounded
// confidence, name every deciding field and have no vetoes beyond the
// proposition.
func TestRoutablePairsConfidence(t *testing.T) {
	fs := loadFacts(t)
	floor := decimal.RequireFromString("0.9")
	for _, pc := range loadPairs(t) {
		rel := match.Compare(instrumentOf(t, pc.A), instrumentOf(t, pc.B), fs)
		if !rel.Type.Routable() || rel.Type != pc.Expect {
			continue // reported by TestPairsFromFixtures
		}
		low := floor
		if pc.MinConf != "" {
			low = decimal.RequireFromString(pc.MinConf)
		}
		if rel.Confidence.LessThan(low) || rel.Confidence.GreaterThan(decimal.NewFromInt(1)) {
			t.Errorf("%s: confidence %s outside [%s, 1]", pc.ID, rel.Confidence, low)
		}
		for _, f := range []string{"deadlines", "polarity"} {
			if rel.Type == canonical.RelEquivalent && !strings.Contains(rel.Explanation, f) {
				t.Errorf("%s: explanation %q lacks %q", pc.ID, rel.Explanation, f)
			}
		}
		if rel.Type == canonical.RelEquivalent && len(rel.Vetoes) != 0 {
			t.Errorf("%s: equivalent with vetoes %v", pc.ID, rel.Vetoes)
		}
		for _, v := range rel.Vetoes {
			switch v.Field {
			case "metric", "comparator", "threshold", "threshold_high", "polarity":
			default:
				t.Errorf("%s: routable %s with a veto on %s", pc.ID, rel.Type, v.Field)
			}
		}
	}
}

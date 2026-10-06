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
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/match"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/normalize"
)

const pairsFile = "../../testdata/match/crypto_pairs.yaml"

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
	A       pairSide                   `yaml:"a"`
	B       pairSide                   `yaml:"b"`
}

func loadPairs(t *testing.T) []pairCase {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(pairsFile))
	if err != nil {
		t.Fatalf("reading pairs: %v", err)
	}
	var doc struct {
		Pairs []pairCase `yaml:"pairs"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decoding pairs: %v", err)
	}
	if len(doc.Pairs) == 0 {
		t.Fatal("no pairs")
	}
	return doc.Pairs
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

func TestCryptoPairsFromFixture(t *testing.T) {
	var equivalents, vetoCases int
	for _, pc := range loadPairs(t) {
		t.Run(pc.ID+"_"+pc.Name, func(t *testing.T) {
			a, b := instrumentOf(t, pc.A), instrumentOf(t, pc.B)
			rel := match.Compare(a, b, nil)
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
			if strings.HasPrefix(pc.ID, "V-") && rel.Type == canonical.RelEquivalent {
				t.Fatalf("VETO SUITE FALSE POSITIVE: %s classified equivalent", pc.ID)
			}
		})
		switch {
		case strings.HasPrefix(pc.ID, "EQ-"):
			equivalents++
		case strings.HasPrefix(pc.ID, "V-"):
			vetoCases++
		}
	}
	if equivalents < 3 {
		t.Errorf("only %d true-equivalent pairs, want at least 3", equivalents)
	}
	if vetoCases < 20 {
		t.Errorf("only %d veto-suite pairs, want at least 20", vetoCases)
	}
}

// TestVetoSuiteSymmetric: swapping A and B never changes the verdict.
func TestVetoSuiteSymmetric(t *testing.T) {
	for _, pc := range loadPairs(t) {
		a, b := instrumentOf(t, pc.A), instrumentOf(t, pc.B)
		ab, ba := match.Compare(a, b, nil), match.Compare(b, a, nil)
		if ab.Type != ba.Type || !ab.Confidence.Equal(ba.Confidence) || len(ab.Vetoes) != len(ba.Vetoes) {
			t.Errorf("%s: a/b %s %s %d vetoes; b/a %s %s %d vetoes", pc.ID, ab.Type, ab.Confidence, len(ab.Vetoes), ba.Type, ba.Confidence, len(ba.Vetoes))
		}
	}
}

// TestEquivalentPairsConfidence: equivalent verdicts carry a high, bounded
// confidence and name every deciding field.
func TestEquivalentPairsConfidence(t *testing.T) {
	floor := decimal.RequireFromString("0.9")
	for _, pc := range loadPairs(t) {
		if !strings.HasPrefix(pc.ID, "EQ-") {
			continue
		}
		rel := match.Compare(instrumentOf(t, pc.A), instrumentOf(t, pc.B), nil)
		if rel.Type != canonical.RelEquivalent {
			continue // reported by TestCryptoPairsFromFixture
		}
		if rel.Confidence.LessThan(floor) || rel.Confidence.GreaterThan(decimal.NewFromInt(1)) {
			t.Errorf("%s: confidence %s outside [0.9, 1]", pc.ID, rel.Confidence)
		}
		for _, f := range []string{"deadlines", "per ", "polarity"} {
			if !strings.Contains(rel.Explanation, f) {
				t.Errorf("%s: explanation %q lacks %q", pc.ID, rel.Explanation, f)
			}
		}
		if len(rel.Vetoes) != 0 {
			t.Errorf("%s: equivalent with vetoes %v", pc.ID, rel.Vetoes)
		}
	}
}

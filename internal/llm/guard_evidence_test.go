package llm_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gopkg.in/yaml.v3"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/facts"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/llm"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/match"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/normalize"
)

// TestCompareGuardKeepsEvidenceValid covers Compare -> llm.Guard -> Validate with
// non-nil matcher evidence: when the LLM lowers the confidence (confirm or
// downgrade), the evidence total must follow it, or UpsertRelationship rejects
// the row (final review of !97, round 1).
func TestCompareGuardKeepsEvidenceValid(t *testing.T) {
	fs := loadFacts(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	checked := 0
	for _, c := range loadPairs(t) {
		rel := match.Compare(instrumentOf(t, c.A), instrumentOf(t, c.B), fs)
		if rel.Evidence == nil || rel.Type == canonical.RelUnrelated || rel.Confidence.LessThan(decimal.RequireFromString("0.2")) {
			continue
		}
		rel.RoundID = 1 // set by the collector in production
		rulesTotal := rel.Evidence.Total
		low := rel.Confidence.Sub(decimal.RequireFromString("0.1"))
		for _, v := range []canonical.LLMVerdict{
			{Relationship: rel.Type, Confidence: low, Evidence: []string{"same terms"}},
			{Relationship: canonical.RelRelated, Confidence: low, Evidence: []string{"terms differ"}},
		} {
			if !rel.Type.CanBecome(v.Relationship) {
				continue
			}
			res := llm.Guard(context.Background(), log, rel, v)
			if err := res.Relationship.Validate(); err != nil {
				t.Fatalf("%s: guarded relationship (%s) fails Validate: %v", c.ID, res.Outcome, err)
			}
			if res.Relationship.Evidence == nil || !res.Relationship.Evidence.Total.Equal(res.Relationship.Confidence) {
				t.Fatalf("%s: evidence total does not follow the guarded confidence", c.ID)
			}
			if !rel.Evidence.Total.Equal(rulesTotal) {
				t.Fatalf("%s: Guard mutated the rules evidence in place", c.ID)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no fixture pair produced matcher evidence; the test covers nothing")
	}
}

// Fixture helpers copied from internal/match/pairs_test.go (the matcher package may not import llm).
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

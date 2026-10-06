package llm

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/shopspring/decimal"
	"gopkg.in/yaml.v3"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// CasesPath is the fixed prompt-injection and jailbreak suite, relative to
// the repo root. Both the AB-01 security test and the live eval run it.
const CasesPath = "testdata/llm/injection_cases.yaml"

// InjectionCase is one fixed attack. VenueText is injected into Field of
// instrument B; MockResponse is what a compromised model would answer when it
// sees that text; Expected is the guard outcome the suite requires.
type InjectionCase struct {
	ID     string `yaml:"id"`
	Attack string `yaml:"attack"`
	// Field is "title" or "rules"; the default is "rules".
	Field string `yaml:"field"`
	// Rules is the rules-based relationship type for the pair; default "related".
	Rules        canonical.RelationshipType `yaml:"rules"`
	VenueText    string                     `yaml:"venue_text"`
	MockResponse string                     `yaml:"mock_response"`
	Expected     Outcome                    `yaml:"expected"`
	// ExpectedType, when set, is the relationship type the guard must produce.
	ExpectedType canonical.RelationshipType `yaml:"expected_type"`
	Note         string                     `yaml:"note"`
}

// LoadInjectionCases reads and validates the case file: unique IDs, known
// fields, outcomes and relationship types, and non-empty attack text.
func LoadInjectionCases(path string) ([]InjectionCase, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is a repo-relative test fixture
	if err != nil {
		return nil, fmt.Errorf("reading injection cases: %w", err)
	}
	var cases []InjectionCase
	if err := yaml.Unmarshal(data, &cases); err != nil {
		return nil, fmt.Errorf("parsing injection cases: %w", err)
	}
	seen := map[string]bool{}
	var errs []error
	for i := range cases {
		c := &cases[i]
		if c.Field == "" {
			c.Field = "rules"
		}
		if c.Rules == "" {
			c.Rules = canonical.RelRelated
		}
		switch {
		case c.ID == "":
			errs = append(errs, fmt.Errorf("case %d has no id", i))
		case seen[c.ID]:
			errs = append(errs, fmt.Errorf("case id %q is duplicated", c.ID))
		}
		seen[c.ID] = true
		if c.Attack == "" || c.VenueText == "" || c.MockResponse == "" {
			errs = append(errs, fmt.Errorf("case %q needs attack, venue_text and mock_response", c.ID))
		}
		if c.Field != "title" && c.Field != "rules" {
			errs = append(errs, fmt.Errorf("case %q field %q must be title or rules", c.ID, c.Field))
		}
		if !c.Rules.Valid() {
			errs = append(errs, fmt.Errorf("case %q rules type %q is not valid", c.ID, c.Rules))
		}
		switch c.Expected {
		case OutcomeConfirmed, OutcomeDowngraded, OutcomeRejectedUpgrade, OutcomeError:
		default:
			errs = append(errs, fmt.Errorf("case %q expected %q is not an outcome", c.ID, c.Expected))
		}
		if c.ExpectedType != "" && !c.ExpectedType.Valid() {
			errs = append(errs, fmt.Errorf("case %q expected_type %q is not valid", c.ID, c.ExpectedType))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return cases, nil
}

// Instruments returns the pair for the case: a clean Kalshi instrument A and
// a Polymarket instrument B carrying the attack text in the chosen field.
func (c InjectionCase) Instruments() (a, b canonical.Instrument) {
	closeAt := time.Date(2026, 10, 31, 16, 0, 0, 0, time.UTC)
	a = canonical.Instrument{
		ID:            "kalshi:KXBTCD-26OCT31-T120000",
		Venue:         canonical.VenueKalshi,
		VenueMarketID: "KXBTCD-26OCT31-T120000",
		VenueEventID:  "KXBTCD-26OCT31",
		Category:      canonical.CategoryCrypto,
		Title:         "Bitcoin price above $120,000 on Oct 31, 2026 at 12pm EDT?",
		RulesText:     "Resolves YES if the CF Benchmarks BRTI price is above $120,000.00 at 12:00 PM EDT on October 31, 2026.",
		Terms:         canonical.UnknownTerms(),
		CloseTime:     closeAt,
		Status:        canonical.StatusOpen,
		FeeSchedule:   canonical.FeeSchedule{ID: canonical.FeeScheduleKalshiTakerV1},
	}
	b = canonical.Instrument{
		ID:            "polymarket:71321045679252212594626385532706912750332728571942532289631379312455583992563",
		Venue:         canonical.VenuePolymarket,
		VenueMarketID: "0x1a2b3c",
		VenueEventID:  "bitcoin-above-120k-on-october-31",
		Category:      canonical.CategoryCrypto,
		Title:         "Will Bitcoin be above $120,000 on October 31?",
		RulesText:     "This market will resolve to Yes if the Binance BTC/USDT 1-minute candle close at 12:00 PM ET on October 31, 2026 is above $120,000.",
		Terms:         canonical.UnknownTerms(),
		CloseTime:     closeAt,
		Status:        canonical.StatusOpen,
		FeeSchedule:   canonical.FeeSchedule{ID: canonical.FeeSchedulePolymarketV1},
	}
	if c.Field == "title" {
		b.Title = b.Title + " " + c.VenueText
	} else {
		b.RulesText = b.RulesText + "\n\n" + c.VenueText
	}
	return a, b
}

// RulesRelationship is the rules verdict for the case's pair.
func (c InjectionCase) RulesRelationship() canonical.Relationship {
	a, b := c.Instruments()
	return canonical.Relationship{
		RoundID:     1,
		AID:         a.ID,
		BID:         b.ID,
		Type:        c.Rules,
		Confidence:  decimal.RequireFromString("0.8"),
		Method:      canonical.MethodRules,
		Diff:        []canonical.FieldDiff{{Field: "resolution_source", A: "CF Benchmarks BRTI", B: "Binance BTC/USDT", Status: canonical.DiffDiffer}},
		Explanation: "Rules verdict " + string(c.Rules) + ": same subject, threshold and deadline; resolution sources differ.",
	}
}

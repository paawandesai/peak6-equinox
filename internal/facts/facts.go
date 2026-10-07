// Package facts loads and validates the external facts the matcher may apply
// (FR-10, D-021). Facts live in config/facts.yaml (path from FACTS_FILE) so
// they are reviewable data, never code: each has an id, a value, a source URL
// and the date it is true as of. A relationship that depends on a fact
// records it in Relationship.FactsUsed, so every verdict can show the fact it
// rests on (equivalent_given_facts in docs/domain.md).
//
// Loading fails fast: a missing file, malformed YAML, an unknown key, a
// duplicate id, a bad date or a future as_of all return an error. Nothing is
// guessed or defaulted.
//
// This package imports only canonical, decimal, yaml and the standard
// library. internal/match consumes a Set through its Facts interface.
package facts

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"
	"gopkg.in/yaml.v3"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// FedUpperBound is the id of the Fed demo fact: the upper bound of the
// federal funds target range, in percent, in force before the next FOMC
// meeting. The Oct 2026 pair applies it: Polymarket "increase 25 bps after the
// October 2026 meeting" is Kalshi "upper bound above <FedUpperBound> after
// Oct 28, 2026" given this fact (docs/domain.md, equivalent_given_facts).
const FedUpperBound = "fed_upper_bound"

// Limits on a facts file and its entries.
const (
	// MaxFileBytes caps the facts file; a real file is a few kilobytes.
	MaxFileBytes = 1 << 20
	// MaxValueLen caps a fact value; values are short literals such as "4.00".
	MaxValueLen = 128
	// MaxSourceLen caps a source URL.
	MaxSourceLen = 2048
	// MaxNotesLen caps the free-text notes.
	MaxNotesLen = 2000
	// DateLayout is the only accepted as_of form: a calendar date, UTC.
	DateLayout = "2006-01-02"
)

// idPattern is the accepted form of a fact id: a snake_case domain term that
// doubles as a stable key in relationship traces and JSON.
var idPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// Fact is one external fact: what is true, where it comes from and since when.
type Fact struct {
	// ID is the snake_case key the matcher looks the fact up by, e.g. "fed_upper_bound".
	ID string
	// Value is the fact as a literal string in its documented unit, e.g. "4.00".
	// Decimal parses it when the fact is numeric.
	Value string
	// Source is the absolute http(s) URL the value was taken from.
	Source string
	// AsOf is the UTC midnight of the calendar date the value is true as of.
	AsOf time.Time
	// Notes is optional free text for reviewers: unit, meeting, caveats.
	Notes string
}

// Use shapes the fact for a relationship trace (canonical.FactUse, FR-10).
func (f Fact) Use() canonical.FactUse {
	return canonical.FactUse{Key: f.ID, Value: f.Value, Source: f.Source, AsOf: f.AsOf}
}

// Decimal parses Value as a decimal number. It fails for non-numeric facts.
func (f Fact) Decimal() (decimal.Decimal, error) {
	d, err := decimal.NewFromString(f.Value)
	if err != nil {
		return decimal.Decimal{}, fmt.Errorf("fact %s: value %q is not a decimal", f.ID, f.Value)
	}
	return d, nil
}

// Set is an immutable collection of validated facts, keyed by id. The zero
// value is an empty set: every lookup misses. Set satisfies match.Facts.
type Set struct {
	byID map[string]Fact
}

// Lookup returns the fact stored under id shaped for a trace, if any.
func (s Set) Lookup(id string) (canonical.FactUse, bool) {
	f, ok := s.byID[id]
	if !ok {
		return canonical.FactUse{}, false
	}
	return f.Use(), true
}

// Get returns the full fact stored under id, if any.
func (s Set) Get(id string) (Fact, bool) {
	f, ok := s.byID[id]
	return f, ok
}

// All returns every fact, sorted by id.
func (s Set) All() []Fact {
	out := make([]Fact, 0, len(s.byID))
	for _, f := range s.byID {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// IDs returns every fact id, sorted.
func (s Set) IDs() []string {
	out := make([]string, 0, len(s.byID))
	for id := range s.byID {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Len is the number of facts in the set.
func (s Set) Len() int { return len(s.byID) }

// FactsUsed shapes the facts with the given ids for Relationship.FactsUsed, in
// the order given. Ids the set does not hold are skipped: a matcher rule only
// names ids it has already looked up, so a miss here is not an error. The
// result is never nil, so it serialises as an empty JSON array.
func (s Set) FactsUsed(ids ...string) []canonical.FactUse {
	out := make([]canonical.FactUse, 0, len(ids))
	for _, id := range ids {
		if u, ok := s.Lookup(id); ok {
			out = append(out, u)
		}
	}
	return out
}

// New builds a Set from facts, applying the same validation as a file. It is
// for tests and callers that assemble facts in code.
func New(now time.Time, fs ...Fact) (Set, error) {
	byID := make(map[string]Fact, len(fs))
	for i, f := range fs {
		if err := f.validate(now); err != nil {
			return Set{}, fmt.Errorf("fact %d: %w", i+1, err)
		}
		if _, dup := byID[f.ID]; dup {
			return Set{}, fmt.Errorf("fact %d: duplicate id %q", i+1, f.ID)
		}
		byID[f.ID] = f
	}
	return Set{byID: byID}, nil
}

// Load reads and validates the facts file at path (FACTS_FILE), judging
// future as_of dates against the current time.
func Load(path string) (Set, error) {
	return LoadAt(path, time.Now())
}

// LoadAt is Load with an explicit clock. A missing file is an error wrapping
// os.ErrNotExist.
func LoadAt(path string, now time.Time) (Set, error) {
	data, err := readCapped(path)
	if err != nil {
		return Set{}, fmt.Errorf("loading facts %s: %w", path, err)
	}
	s, err := Parse(data, now)
	if err != nil {
		return Set{}, fmt.Errorf("loading facts %s: %w", path, err)
	}
	return s, nil
}

func readCapped(path string) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // path is FACTS_FILE, operator configuration
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading: %w", err)
	}
	if len(data) > MaxFileBytes {
		return nil, fmt.Errorf("file exceeds %d bytes", MaxFileBytes)
	}
	return data, nil
}

// factsFile is the on-disk shape of config/facts.yaml.
type factsFile struct {
	Facts []factEntry `yaml:"facts"`
}

// factEntry is one YAML list item. Every field decodes as a string so the
// file's literal text is what gets validated: a bare 4.00 stays "4.00" and a
// bare date stays "2026-09-16".
type factEntry struct {
	ID     string `yaml:"id"`
	Value  string `yaml:"value"`
	Source string `yaml:"source"`
	AsOf   string `yaml:"as_of"`
	Notes  string `yaml:"notes"`
}

// Parse decodes and validates facts-file contents. Unknown keys are errors.
// The top-level facts key is required; an empty list is allowed and yields an
// empty Set (the matcher then never applies a fact).
func Parse(data []byte, now time.Time) (Set, error) {
	if len(data) > MaxFileBytes {
		return Set{}, fmt.Errorf("file exceeds %d bytes", MaxFileBytes)
	}
	if !utf8.Valid(data) {
		return Set{}, errors.New("file is not valid UTF-8")
	}
	var ff factsFile
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&ff); err != nil {
		if errors.Is(err, io.EOF) {
			return Set{}, errors.New("file is empty")
		}
		return Set{}, fmt.Errorf("parsing yaml: %w", err)
	}
	if ff.Facts == nil {
		return Set{}, errors.New("missing top-level facts list")
	}
	fs := make([]Fact, 0, len(ff.Facts))
	for i, e := range ff.Facts {
		f, err := e.toFact()
		if err != nil {
			return Set{}, fmt.Errorf("fact %d: %w", i+1, err)
		}
		fs = append(fs, f)
	}
	return New(now, fs...)
}

func (e factEntry) toFact() (Fact, error) {
	f := Fact{
		ID:     strings.TrimSpace(e.ID),
		Value:  strings.TrimSpace(e.Value),
		Source: strings.TrimSpace(e.Source),
		Notes:  strings.TrimSpace(e.Notes),
	}
	asOf := strings.TrimSpace(e.AsOf)
	if asOf == "" {
		return Fact{}, fmt.Errorf("%s: as_of is required", describe(f.ID))
	}
	t, err := time.ParseInLocation(DateLayout, asOf, time.UTC)
	if err != nil {
		return Fact{}, fmt.Errorf("%s: as_of %q is not a date in the form %s", describe(f.ID), asOf, DateLayout)
	}
	f.AsOf = t
	return f, nil
}

// validate checks one fact's fields. now bounds as_of: a fact cannot be true
// as of a day that has not happened yet (D-066: reject, not warn).
func (f Fact) validate(now time.Time) error {
	what := describe(f.ID)
	if !idPattern.MatchString(f.ID) {
		return fmt.Errorf("%s: id must match %s", what, idPattern)
	}
	if f.Value == "" {
		return fmt.Errorf("%s: value is required", what)
	}
	if len(f.Value) > MaxValueLen {
		return fmt.Errorf("%s: value exceeds %d bytes", what, MaxValueLen)
	}
	if err := checkSource(f.Source); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if f.AsOf.IsZero() {
		return fmt.Errorf("%s: as_of is required", what)
	}
	if f.AsOf.Location() != time.UTC || !f.AsOf.Equal(f.AsOf.Truncate(24*time.Hour)) {
		return fmt.Errorf("%s: as_of must be a UTC calendar date", what)
	}
	today := now.UTC().Truncate(24 * time.Hour)
	if f.AsOf.After(today) {
		return fmt.Errorf("%s: as_of %s is in the future (today is %s)", what,
			f.AsOf.Format(DateLayout), today.Format(DateLayout))
	}
	if len(f.Notes) > MaxNotesLen {
		return fmt.Errorf("%s: notes exceed %d bytes", what, MaxNotesLen)
	}
	return nil
}

func checkSource(s string) error {
	if s == "" {
		return errors.New("source is required")
	}
	if len(s) > MaxSourceLen {
		return fmt.Errorf("source exceeds %d bytes", MaxSourceLen)
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("source %q must be an absolute http(s) URL", s)
	}
	return nil
}

// describe names a fact in errors without echoing arbitrary text: ids are
// validated before anything else about them is printed.
func describe(id string) string {
	if idPattern.MatchString(id) {
		return "fact " + id
	}
	return "fact with invalid id"
}

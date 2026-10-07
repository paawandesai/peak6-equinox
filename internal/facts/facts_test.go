package facts_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gopkg.in/yaml.v3"
	"pgregory.net/rapid"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/facts"
)

// now is the clock every test judges as_of against: 2026-10-06 12:00 UTC.
var now = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

const goodYAML = `# comment
facts:
  - id: fed_upper_bound
    value: "4.00"
    source: https://www.federalreserve.gov/newsevents/pressreleases/monetary20260916a.htm
    as_of: 2026-09-16
    notes: Percent.
  - id: btc_halving_block
    value: "1050000"
    source: https://example.com/halving
    as_of: 2024-04-20
`

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "facts.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadHappyPath(t *testing.T) {
	t.Parallel()
	s, err := facts.LoadAt(writeFile(t, goodYAML), now)
	if err != nil {
		t.Fatalf("LoadAt: %v", err)
	}
	if s.Len() != 2 {
		t.Fatalf("Len = %d, want 2", s.Len())
	}
	f, ok := s.Get("fed_upper_bound")
	if !ok {
		t.Fatal("fed_upper_bound missing")
	}
	want := facts.Fact{
		ID:     "fed_upper_bound",
		Value:  "4.00",
		Source: "https://www.federalreserve.gov/newsevents/pressreleases/monetary20260916a.htm",
		AsOf:   time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC),
		Notes:  "Percent.",
	}
	if f != want {
		t.Errorf("Get = %+v, want %+v", f, want)
	}
	if f.AsOf.Location() != time.UTC {
		t.Errorf("AsOf location = %v, want UTC", f.AsOf.Location())
	}
	d, err := f.Decimal()
	if err != nil || !d.Equal(decimal.RequireFromString("4.00")) {
		t.Errorf("Decimal = %v, %v", d, err)
	}
	if got := s.IDs(); strings.Join(got, ",") != "btc_halving_block,fed_upper_bound" {
		t.Errorf("IDs = %v (want sorted)", got)
	}
	all := s.All()
	if len(all) != 2 || all[0].ID != "btc_halving_block" || all[1].ID != "fed_upper_bound" {
		t.Errorf("All = %+v (want sorted by id)", all)
	}
	// Optional notes default to empty.
	if b, _ := s.Get("btc_halving_block"); b.Notes != "" {
		t.Errorf("notes = %q, want empty", b.Notes)
	}
}

func TestLookupAndFactsUsedShapeFactUse(t *testing.T) {
	t.Parallel()
	s, err := facts.Parse([]byte(goodYAML), now)
	if err != nil {
		t.Fatal(err)
	}
	u, ok := s.Lookup("fed_upper_bound")
	if !ok {
		t.Fatal("Lookup miss")
	}
	wantUse := canonical.FactUse{
		Key:    "fed_upper_bound",
		Value:  "4.00",
		Source: "https://www.federalreserve.gov/newsevents/pressreleases/monetary20260916a.htm",
		AsOf:   time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC),
	}
	if u != wantUse {
		t.Errorf("Lookup = %+v, want %+v", u, wantUse)
	}
	if _, ok := s.Lookup("nope"); ok {
		t.Error("Lookup of unknown id hit")
	}
	if _, ok := s.Get("nope"); ok {
		t.Error("Get of unknown id hit")
	}

	used := s.FactsUsed("fed_upper_bound", "nope", "btc_halving_block")
	if len(used) != 2 || used[0].Key != "fed_upper_bound" || used[1].Key != "btc_halving_block" {
		t.Errorf("FactsUsed = %+v", used)
	}
	if used[0] != wantUse {
		t.Errorf("FactsUsed[0] = %+v, want %+v", used[0], wantUse)
	}
	if empty := s.FactsUsed(); empty == nil || len(empty) != 0 {
		t.Errorf("FactsUsed() = %#v, want empty non-nil", empty)
	}
	f, _ := s.Get("fed_upper_bound")
	if f.Use() != wantUse {
		t.Errorf("Use = %+v", f.Use())
	}
}

func TestZeroSetIsEmpty(t *testing.T) {
	t.Parallel()
	var s facts.Set
	if s.Len() != 0 || len(s.All()) != 0 || len(s.IDs()) != 0 {
		t.Error("zero Set is not empty")
	}
	if _, ok := s.Lookup("fed_upper_bound"); ok {
		t.Error("zero Set Lookup hit")
	}
	if got := s.FactsUsed("fed_upper_bound"); got == nil || len(got) != 0 {
		t.Errorf("FactsUsed = %#v", got)
	}
}

func TestLoadMissingFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "missing.yaml")
	_, err := facts.LoadAt(path, now)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("error does not wrap ErrNotExist: %v", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error does not name the path: %v", err)
	}
	if _, err := facts.Load(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Load: %v", err)
	}
}

func TestLoadOversizedFile(t *testing.T) {
	t.Parallel()
	big := "facts: []\n# " + strings.Repeat("x", facts.MaxFileBytes)
	_, err := facts.LoadAt(writeFile(t, big), now)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("err = %v, want size error", err)
	}
}

func TestParseRejectsInvalidFiles(t *testing.T) {
	t.Parallel()
	entry := func(id, value, source, asOf string) string {
		return fmt.Sprintf("facts:\n  - id: %s\n    value: %s\n    source: %s\n    as_of: %s\n", id, value, source, asOf)
	}
	good := func(id string) string {
		return entry(id, `"1"`, "https://example.com/x", "2026-01-02")
	}
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"empty file", "", "file is empty"},
		{"malformed yaml", "facts: [\n  - id: x\n", "parsing yaml"},
		{"not a mapping", "- just\n- a list\n", "parsing yaml"},
		{"missing facts key", "{}\n", "missing top-level facts list"},
		{"facts is null", "facts:\n", "missing top-level facts list"},
		{"unknown top-level key", "facts: []\nextra: 1\n", "field extra not found"},
		{"unknown fact key", good("x") + "    unit: percent\n", "field unit not found"},
		{"duplicate ids", good("fed_upper_bound") + strings.TrimPrefix(good("fed_upper_bound"), "facts:\n"), `fact 2: duplicate id "fed_upper_bound"`},
		{"future as_of", entry("x", `"1"`, "https://example.com", "2026-10-07"), "as_of 2026-10-07 is in the future (today is 2026-10-06)"},
		{"missing as_of", "facts:\n  - id: x\n    value: \"1\"\n    source: https://example.com\n", "as_of is required"},
		{"as_of with time", entry("x", `"1"`, "https://example.com", `"2026-01-02T00:00:00Z"`), "is not a date in the form 2006-01-02"},
		{"as_of not a date", entry("x", `"1"`, "https://example.com", "yesterday"), "is not a date in the form 2006-01-02"},
		{"as_of invalid day", entry("x", `"1"`, "https://example.com", "2026-02-30"), "is not a date"},
		{"missing id", entry(`""`, `"1"`, "https://example.com", "2026-01-02"), "fact with invalid id: id must match"},
		{"uppercase id", entry("FedUpperBound", `"1"`, "https://example.com", "2026-01-02"), "fact with invalid id: id must match"},
		{"id with dash", entry("fed-upper-bound", `"1"`, "https://example.com", "2026-01-02"), "id must match"},
		{"id too long", entry(strings.Repeat("a", 65), `"1"`, "https://example.com", "2026-01-02"), "id must match"},
		{"missing value", entry("x", `""`, "https://example.com", "2026-01-02"), "fact x: value is required"},
		{"value too long", entry("x", strings.Repeat("9", facts.MaxValueLen+1), "https://example.com", "2026-01-02"), "value exceeds"},
		{"missing source", entry("x", `"1"`, `""`, "2026-01-02"), "fact x: source is required"},
		{"relative source", entry("x", `"1"`, "federalreserve.gov/x", "2026-01-02"), "must be an absolute http(s) URL"},
		{"non-http source", entry("x", `"1"`, "ftp://example.com/x", "2026-01-02"), "must be an absolute http(s) URL"},
		{"source without host", entry("x", `"1"`, "https:///x", "2026-01-02"), "must be an absolute http(s) URL"},
		{"source too long", entry("x", `"1"`, "https://example.com/"+strings.Repeat("a", facts.MaxSourceLen), "2026-01-02"), "source exceeds"},
		{"notes too long", good("x") + "    notes: " + strings.Repeat("n", facts.MaxNotesLen+1) + "\n", "notes exceed"},
		{"invalid utf-8", "facts:\n  - id: \xff\n", "not valid UTF-8"},
		{"fact is a scalar", "facts:\n  - 42\n", "parsing yaml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, err := facts.Parse([]byte(tc.yaml), now)
			if err == nil {
				t.Fatalf("Parse accepted %q: %+v", tc.yaml, s.All())
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %q, want it to contain %q", err, tc.want)
			}
			if s.Len() != 0 {
				t.Errorf("Set not empty on error: %d", s.Len())
			}
		})
	}
}

func TestParseAsOfTodayIsAllowed(t *testing.T) {
	t.Parallel()
	y := "facts:\n  - id: x\n    value: \"1\"\n    source: https://example.com\n    as_of: 2026-10-06\n"
	// Late in the UTC day the date is still today.
	late := time.Date(2026, 10, 6, 23, 59, 59, 0, time.UTC)
	if _, err := facts.Parse([]byte(y), late); err != nil {
		t.Errorf("as_of today rejected: %v", err)
	}
	// A clock in another zone is compared on its UTC date.
	ny := time.FixedZone("NY", -4*3600)
	early := time.Date(2026, 10, 5, 21, 0, 0, 0, ny) // 2026-10-06 01:00 UTC
	if _, err := facts.Parse([]byte(y), early); err != nil {
		t.Errorf("as_of today (UTC) rejected for a non-UTC clock: %v", err)
	}
	if _, err := facts.Parse([]byte(y), now.Add(-24*time.Hour)); err == nil {
		t.Error("as_of tomorrow accepted")
	}
}

func TestParseEmptyListIsAnEmptySet(t *testing.T) {
	t.Parallel()
	s, err := facts.Parse([]byte("facts: []\n"), now)
	if err != nil {
		t.Fatal(err)
	}
	if s.Len() != 0 {
		t.Errorf("Len = %d", s.Len())
	}
}

func TestParseTrimsAndKeepsLiteralText(t *testing.T) {
	t.Parallel()
	y := "facts:\n  - id: \" rate \"\n    value: 4.50\n    source: \" https://example.com/a \"\n    as_of: \" 2026-01-02 \"\n    notes: \"  n  \"\n"
	s, err := facts.Parse([]byte(y), now)
	if err != nil {
		t.Fatal(err)
	}
	f, ok := s.Get("rate")
	if !ok || f.Value != "4.50" || f.Source != "https://example.com/a" || f.Notes != "n" {
		t.Errorf("fact = %+v", f)
	}
}

func TestNewValidatesLikeParse(t *testing.T) {
	t.Parallel()
	ok := facts.Fact{ID: "a", Value: "1", Source: "https://example.com", AsOf: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}
	if _, err := facts.New(now, ok); err != nil {
		t.Errorf("New: %v", err)
	}
	if _, err := facts.New(now, ok, ok); err == nil || !strings.Contains(err.Error(), "duplicate id") {
		t.Errorf("duplicate: %v", err)
	}
	zero := ok
	zero.AsOf = time.Time{}
	if _, err := facts.New(now, zero); err == nil || !strings.Contains(err.Error(), "as_of is required") {
		t.Errorf("zero as_of: %v", err)
	}
	withTime := ok
	withTime.AsOf = time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	if _, err := facts.New(now, withTime); err == nil || !strings.Contains(err.Error(), "UTC calendar date") {
		t.Errorf("as_of with time: %v", err)
	}
	local := ok
	local.AsOf = time.Date(2026, 1, 2, 0, 0, 0, 0, time.FixedZone("X", 3600))
	if _, err := facts.New(now, local); err == nil || !strings.Contains(err.Error(), "UTC calendar date") {
		t.Errorf("non-UTC as_of: %v", err)
	}
	text := ok
	text.Value = "four"
	s, err := facts.New(now, text)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := s.Get("a")
	if _, err := f.Decimal(); err == nil || !strings.Contains(err.Error(), "is not a decimal") {
		t.Errorf("Decimal of text: %v", err)
	}
}

// TestShippedFactsFile loads config/facts.yaml as the app will, and checks the
// Fed demo fact (D-066) is present and well-formed.
func TestShippedFactsFile(t *testing.T) {
	t.Parallel()
	s, err := facts.Load(filepath.Join("..", "..", "config", "facts.yaml"))
	if err != nil {
		t.Fatalf("shipped facts file: %v", err)
	}
	f, ok := s.Get(facts.FedUpperBound)
	if !ok {
		t.Fatalf("shipped file lacks %s; ids: %v", facts.FedUpperBound, s.IDs())
	}
	d, err := f.Decimal()
	if err != nil {
		t.Fatalf("fed_upper_bound value: %v", err)
	}
	if d.LessThanOrEqual(decimal.Zero) || d.GreaterThan(decimal.NewFromInt(25)) {
		t.Errorf("fed_upper_bound = %s, not a plausible percent", d)
	}
	if !strings.HasPrefix(f.Source, "https://www.federalreserve.gov/") {
		t.Errorf("fed_upper_bound source = %q, want federalreserve.gov", f.Source)
	}
	if f.Notes == "" {
		t.Error("fed_upper_bound has no notes (unit and meeting are documented there)")
	}
}

// TestParseRoundTrip (property): any valid set of facts survives a YAML
// marshal → Parse round trip unchanged.
func TestParseRoundTrip(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(0, 6).Draw(rt, "n")
		ids := map[string]bool{}
		type entry struct {
			ID     string `yaml:"id"`
			Value  string `yaml:"value"`
			Source string `yaml:"source"`
			AsOf   string `yaml:"as_of"`
			Notes  string `yaml:"notes,omitempty"`
		}
		var entries []entry
		want := map[string]facts.Fact{}
		for i := 0; i < n; i++ {
			id := rapid.StringMatching(`^[a-z][a-z0-9_]{0,20}$`).Draw(rt, "id")
			if ids[id] {
				continue
			}
			ids[id] = true
			value := rapid.StringMatching(`^[0-9]{1,6}(\.[0-9]{1,4})?$`).Draw(rt, "value")
			source := "https://example.com/" + rapid.StringMatching(`^[a-z0-9/._-]{0,30}$`).Draw(rt, "path")
			days := rapid.IntRange(0, 3650).Draw(rt, "days")
			asOf := now.UTC().Truncate(24*time.Hour).AddDate(0, 0, -days)
			notes := rapid.StringMatching(`^[A-Za-z0-9 .,%()-]{0,60}$`).Draw(rt, "notes")
			notes = strings.TrimSpace(notes)
			entries = append(entries, entry{ID: id, Value: value, Source: source, AsOf: asOf.Format(facts.DateLayout), Notes: notes})
			want[id] = facts.Fact{ID: id, Value: value, Source: source, AsOf: asOf, Notes: notes}
		}
		data, err := yaml.Marshal(map[string]any{"facts": entries})
		if err != nil {
			rt.Fatal(err)
		}
		if entries == nil {
			data = []byte("facts: []\n")
		}
		s, err := facts.Parse(data, now)
		if err != nil {
			rt.Fatalf("Parse(%s): %v", data, err)
		}
		if s.Len() != len(want) {
			rt.Fatalf("Len = %d, want %d", s.Len(), len(want))
		}
		for id, w := range want {
			got, ok := s.Get(id)
			if !ok || got != w {
				rt.Errorf("Get(%s) = %+v, %v; want %+v", id, got, ok, w)
			}
			u, _ := s.Lookup(id)
			if u != w.Use() {
				rt.Errorf("Lookup(%s) = %+v", id, u)
			}
		}
		prev := ""
		for _, f := range s.All() {
			if f.ID <= prev {
				rt.Errorf("All not sorted: %q after %q", f.ID, prev)
			}
			prev = f.ID
		}
	})
}

// FuzzParse checks the parser never panics and never returns a non-empty Set
// together with an error.
func FuzzParse(f *testing.F) {
	f.Add([]byte(goodYAML))
	f.Add([]byte("facts: []\n"))
	f.Add([]byte("facts:\n  - id: x\n    value: 1\n    source: https://e.com\n    as_of: 2026-10-07\n"))
	f.Add([]byte("facts: [\n"))
	f.Add([]byte(""))
	f.Fuzz(func(t *testing.T, data []byte) {
		s, err := facts.Parse(data, now)
		if err != nil && s.Len() != 0 {
			t.Errorf("error with non-empty set: %v", err)
		}
		if err == nil {
			for _, fact := range s.All() {
				if fact.ID == "" || fact.Value == "" || fact.Source == "" || fact.AsOf.IsZero() || fact.AsOf.After(now) {
					t.Errorf("accepted invalid fact %+v", fact)
				}
			}
		}
	})
}

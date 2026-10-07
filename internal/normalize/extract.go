// Package normalize extracts canonical.Terms from an instrument's title and
// rules text with deterministic rules, one extractor per category (FR-06).
//
// Contract: unknown is a value. A term the text does not state, states
// ambiguously, or states inconsistently is recorded as canonical.Unknown (nil
// for Threshold and Deadline), never guessed. The matcher (internal/match)
// treats any unknown on a vetoed field as "cannot be equivalent".
//
// Precedence: the rules text is the binding document, so every field is read
// from the rules first and from the title only when the rules do not state it
// (D-060). Venue text is hostile input (AB-07): the extractors use bounded
// regular expressions, never panic, and never execute or interpret the text.
//
// Each category file (crypto.go, fed.go, politics.go) installs its extractor
// from an init function with Register, so adding a category never edits this
// file. A category with no extractor gets UnknownTerms.
package normalize

import (
	"fmt"
	"sort"
	"sync"

	// Deadlines are stated in venue-local zones (ET, UTC). Embedding the zone
	// database makes extraction independent of the runtime image's tzdata.
	_ "time/tzdata"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// Quality flags added by this package. canonical.QualityDeadlineDefaulted is
// reused for a deadline taken from the close time.
const (
	// QualityDeadlineYearInferred: the text gave a month and day but no year;
	// the year nearest the instrument's close time was used.
	QualityDeadlineYearInferred canonical.QualityFlag = "deadline_year_inferred"
	// QualityTermsAmbiguous: a field was stated more than one way in the same
	// text and was recorded as unknown.
	QualityTermsAmbiguous canonical.QualityFlag = "terms_ambiguous"
)

// Extractor turns one instrument's title and rules into Terms plus the
// quality issues found on the way. It must never panic on hostile text.
type Extractor func(canonical.Instrument) (canonical.Terms, canonical.Quality)

// registry maps a category to its extractor. It is filled by Register from
// the category files' init functions and read-only afterwards; the mutex
// keeps Register safe to call from tests too.
var (
	registryMu sync.RWMutex
	registry   = map[canonical.Category]Extractor{}
)

// Register installs fn as the extractor for category. It is meant to be
// called from an init function, once per category: registering nil or the
// same category twice is a programming error and panics at start-up, before
// any venue data is handled.
func Register(category canonical.Category, fn Extractor) {
	if fn == nil {
		panic(fmt.Sprintf("normalize: Register(%q) with a nil extractor", category))
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := registry[category]; dup {
		panic(fmt.Sprintf("normalize: Register(%q) called twice", category))
	}
	registry[category] = fn
}

// Registered returns the categories that have an extractor, sorted.
func Registered() []canonical.Category {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]canonical.Category, 0, len(registry))
	for c := range registry {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func extractorFor(category canonical.Category) (Extractor, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	fn, ok := registry[category]
	return fn, ok
}

// Normalize returns a copy of in with Terms extracted from its title and rules
// text and any extraction quality issues appended to Quality. The collector
// calls this on every instrument it stores.
func Normalize(in canonical.Instrument) canonical.Instrument {
	terms, issues := extract(in)
	in.Terms = terms
	in.Quality = append(canonical.Quality(nil), in.Quality...) // never write into the caller's slice
	for _, issue := range issues {
		in.Quality.Add(issue.Flag, issue.Field, issue.Detail)
	}
	return in
}

// Extract returns the Terms Normalize would set, without the quality issues.
// It is the architecture.md interface; prefer Normalize when the flags matter.
func Extract(in canonical.Instrument) canonical.Terms {
	terms, _ := extract(in)
	return terms
}

func extract(in canonical.Instrument) (canonical.Terms, canonical.Quality) {
	if fn, ok := extractorFor(in.Category); ok {
		return fn(in)
	}
	return canonical.UnknownTerms(), nil
}

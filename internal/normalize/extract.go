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
// M1 supports the crypto category only; fed and politics follow in M2 and
// return UnknownTerms until then.
package normalize

import (
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
	if in.Category == canonical.CategoryCrypto {
		return extractCrypto(in)
	}
	return canonical.UnknownTerms(), nil
}

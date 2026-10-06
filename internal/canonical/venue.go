package canonical

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Unknown is the value recorded for any term that could not be parsed.
const Unknown = "unknown"

// Venue identifies a prediction-market exchange.
type Venue string

// Supported venues.
const (
	VenueKalshi     Venue = "kalshi"
	VenuePolymarket Venue = "polymarket"
)

// Venues lists every supported venue in a stable order.
func Venues() []Venue { return []Venue{VenueKalshi, VenuePolymarket} }

// Valid reports whether v is a supported venue.
func (v Venue) Valid() bool { return v == VenueKalshi || v == VenuePolymarket }

// Category is the market family a venue universe groups instruments into.
type Category string

// Supported categories (docs/plan.md).
const (
	CategoryCrypto   Category = "crypto"
	CategoryFed      Category = "fed"
	CategoryPolitics Category = "politics"
)

// Categories lists every supported category in a stable order.
func Categories() []Category { return []Category{CategoryCrypto, CategoryFed, CategoryPolitics} }

// Valid reports whether c is a supported category.
func (c Category) Valid() bool {
	return c == CategoryCrypto || c == CategoryFed || c == CategoryPolitics
}

// instrumentIDPattern is the accepted form of an instrument ID:
// "<venue>:<venue market key>", where the key is a Kalshi ticker or a
// Polymarket token ID (AB-08: IDs are validated against expected formats).
var instrumentIDPattern = regexp.MustCompile(`^(kalshi|polymarket):[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// InstrumentID builds the canonical ID for a venue market key, e.g.
// "kalshi:KXBTCD-26OCT0617-T120000" or "polymarket:<token_id>".
func InstrumentID(v Venue, key string) string { return string(v) + ":" + key }

// ValidateInstrumentID checks id has the form "<venue>:<key>" with a known
// venue and a key of 1-128 characters from [A-Za-z0-9._-].
func ValidateInstrumentID(id string) error {
	if !instrumentIDPattern.MatchString(id) {
		return fmt.Errorf("instrument id %q: want <venue>:<key> with key [A-Za-z0-9._-]{1,128}", truncate(id, 64))
	}
	return nil
}

// VenueOf returns the venue prefix of an instrument ID, or "" if it has none.
func VenueOf(id string) Venue {
	v, _, ok := strings.Cut(id, ":")
	if !ok {
		return ""
	}
	return Venue(v)
}

// checkUTC returns an error unless t is in time.UTC. A zero time passes only
// when allowZero is set.
func checkUTC(field string, t time.Time, allowZero bool) error {
	if t.IsZero() {
		if allowZero {
			return nil
		}
		return fmt.Errorf("%s is required", field)
	}
	if t.Location() != time.UTC {
		return fmt.Errorf("%s must be UTC; got location %q", field, t.Location())
	}
	return nil
}

// truncate shortens s to at most n bytes (on a rune boundary) plus an
// ellipsis, so hostile input cannot bloat errors, logs or stored records.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := 0
	for i := range s {
		if i > n {
			break
		}
		cut = i
	}
	return s[:cut] + "…"
}

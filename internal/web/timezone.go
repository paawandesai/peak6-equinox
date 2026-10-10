package web

import (
	"net/http"
	"time"
)

// Supported timezones for the timezone selector (M3-08, M4-77).
const (
	TzET    = "ET"
	TzCT    = "CT"
	TzUTC   = "UTC"
	TzLocal = "Local"
)

// timezoneCookie is the cookie name for the user's timezone preference.
const timezoneCookie = "tz"

// loadLocation is the timezone loader used by formatTime. It can be overridden
// in tests to inject failures and verify the UTC fallback path.
var loadLocation = time.LoadLocation

// timezoneFromRequest reads the user's timezone preference from the tz cookie.
// Returns ET (America/New_York) as the default when the cookie is missing or invalid.
func timezoneFromRequest(r *http.Request) string {
	cookie, err := r.Cookie(timezoneCookie)
	if err != nil || cookie.Value == "" {
		return TzET
	}
	switch cookie.Value {
	case TzET, TzCT, TzUTC, TzLocal:
		return cookie.Value
	default:
		return TzET
	}
}

// formatTime formats a time.Time in the given timezone. tz must be one of
// TzET, TzCT, TzUTC, or TzLocal (M3-08, M4-77). Returns "unknown" for zero times.
// Errors loading a timezone location fall back to UTC. Unknown tz defaults to ET.
// For TzLocal, displays UTC with a note (JS rewrites it client-side when enabled).
func formatTime(t time.Time, tz string) string {
	if t.IsZero() {
		return "unknown"
	}

	var loc *time.Location
	var err error

	switch tz {
	case TzET:
		loc, err = loadLocation("America/New_York")
	case TzCT:
		loc, err = loadLocation("America/Chicago")
	case TzUTC, TzLocal:
		// Local falls back to UTC when JS is disabled (M4-77)
		loc = time.UTC
	default:
		// Unknown timezone defaults to ET (M3-08)
		loc, err = loadLocation("America/New_York")
	}

	// If time.LoadLocation fails (e.g., timezone database unavailable), fall back to UTC.
	if err != nil {
		loc = time.UTC
	}

	return t.In(loc).Format("2006-01-02 15:04:05 MST")
}

package web

import (
	"net/http"
	"time"
)

// Supported timezones for the timezone selector (M3-08).
const (
	TzET  = "ET"
	TzCT  = "CT"
	TzUTC = "UTC"
)

// timezoneCookie is the cookie name for the user's timezone preference.
const timezoneCookie = "tz"

// timezoneFromRequest reads the user's timezone preference from the tz cookie.
// Returns ET (America/New_York) as the default when the cookie is missing or invalid.
func timezoneFromRequest(r *http.Request) string {
	cookie, err := r.Cookie(timezoneCookie)
	if err != nil || cookie.Value == "" {
		return TzET
	}
	switch cookie.Value {
	case TzET, TzCT, TzUTC:
		return cookie.Value
	default:
		return TzET
	}
}

// formatTime formats a time.Time in the given timezone. tz must be one of
// TzET, TzCT, or TzUTC (M3-08). Returns "unknown" for zero times.
// Errors loading a timezone location fall back to UTC. Unknown tz defaults to ET.
func formatTime(t time.Time, tz string) string {
	if t.IsZero() {
		return "unknown"
	}

	var loc *time.Location
	var err error

	switch tz {
	case TzET:
		loc, err = time.LoadLocation("America/New_York")
	case TzCT:
		loc, err = time.LoadLocation("America/Chicago")
	case TzUTC:
		loc = time.UTC
	default:
		// Unknown timezone defaults to ET (M3-08)
		loc, err = time.LoadLocation("America/New_York")
	}

	// If time.LoadLocation fails (e.g., timezone database unavailable), fall back to UTC.
	if err != nil {
		loc = time.UTC
	}

	return t.In(loc).Format("2006-01-02 15:04:05 MST")
}

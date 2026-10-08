package web

import (
	"net/http"
)

// handleTimezone answers POST /timezone: sets the user's timezone preference
// cookie and redirects back to the referrer (M3-08).
func (s *Server) handleTimezone(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}

	tz := r.FormValue("tz")
	// Validate timezone: must be one of the supported values
	if tz != TzET && tz != TzCT && tz != TzUTC {
		http.Error(w, "invalid timezone", http.StatusBadRequest)
		return
	}

	// Set the timezone cookie (expires in 1 year)
	//nolint:gosec // G124: All required security attributes (Secure, HttpOnly, SameSite) are set
	http.SetCookie(w, &http.Cookie{
		Name:     timezoneCookie,
		Value:    tz,
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60, // 1 year in seconds
		HttpOnly: true,
		Secure:   r.TLS != nil, // Secure only on HTTPS
		SameSite: http.SameSiteLaxMode,
	})

	// Redirect back to the referrer, or home if no referrer or if referer is not same-origin.
	// Only accept relative URLs to prevent open redirects (AB-08).
	referer := r.Header.Get("Referer")
	target := "/"
	if referer != "" && len(referer) > 0 && referer[0] == '/' && (len(referer) == 1 || referer[1] != '/') {
		// Relative URL starting with single / (not //)
		target = referer
	}
	//nolint:gosec // G710: target is validated to be a relative URL only
	http.Redirect(w, r, target, http.StatusSeeOther)
}

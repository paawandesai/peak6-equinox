package web

import (
	"net/http"
	"net/url"
)

// handleTimezone answers POST /timezone: sets the user's timezone preference
// cookie and redirects back to the referrer (M3-08).
func (s *Server) handleTimezone(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}

	tz := r.FormValue("tz")
	// Validate timezone: must be one of the supported values (M4-77 added Local)
	if tz != TzET && tz != TzCT && tz != TzUTC && tz != TzLocal {
		http.Error(w, "invalid timezone", http.StatusBadRequest)
		return
	}

	// Set the timezone cookie (expires in 1 year)
	// Secure flag is set when serving over HTTPS (X-Forwarded-Proto behind Cloud Run proxy)
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	//nolint:gosec // G124: All required security attributes (Secure, HttpOnly, SameSite) are set
	http.SetCookie(w, &http.Cookie{
		Name:     timezoneCookie,
		Value:    tz,
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60, // 1 year in seconds
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})

	// Redirect back to the referrer, or home if no referrer or if referer is not same-origin.
	// Parse the referer URL and only accept same-host paths to prevent open redirects (AB-08, Cursor round 1).
	referer := r.Header.Get("Referer")
	target := "/"
	if referer != "" {
		refURL, err := url.Parse(referer)
		if err == nil && refURL.Host == r.Host {
			// Same-host: use the path and query, but guard against // and \ to prevent protocol-relative URLs
			path := refURL.Path
			if path != "" && path[0] == '/' && (len(path) == 1 || (path[1] != '/' && path[1] != '\\')) {
				target = path
				if refURL.RawQuery != "" {
					target += "?" + refURL.RawQuery
				}
			}
		}
	}
	//nolint:gosec // G710: target is validated to be same-host path only
	http.Redirect(w, r, target, http.StatusSeeOther)
}

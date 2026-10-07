package web

import "net/http"

// cdnHost is the only third-party origin pages may load scripts and styles
// from. Both assets are pinned by version with an SRI hash (public.go), so a
// swapped CDN file fails to load rather than running (AB-07, AB-09).
const cdnHost = "https://cdn.jsdelivr.net"

// contentSecurityPolicy is the CSP every response carries (NFR-12). There is
// no 'unsafe-inline', so templates must not contain inline scripts, event
// handler attributes or style attributes; templates_test.go enforces that.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self' " + cdnHost + "; " +
	"style-src 'self' " + cdnHost + "; " +
	"object-src 'none'; " +
	"frame-ancestors 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'"

// securityHeaders sets the CSP and related security headers on every response.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

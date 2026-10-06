package web

import "net/http"

// securityHeaders sets the CSP and related security headers.
// Identity stub until M1-09 implements it.
func securityHeaders(next http.Handler) http.Handler {
	return next
}

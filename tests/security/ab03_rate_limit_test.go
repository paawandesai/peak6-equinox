package security

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/web"
)

// TestAB03_PublicRateLimit: public routes enforce a per-IP rate limit (default
// 60 req/min) to prevent flooding (AB-03). When exceeded, the server returns
// HTTP 429 with a Retry-After header and a JSON body.
func TestAB03_PublicRateLimit(t *testing.T) {
	clock := &testClock{t: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	s := web.New(web.Config{PublicRateLimitPerMin: 60, AppEnv: "ci"}, web.Deps{Now: clock.now})
	handler := s.Handler()

	ip := "203.0.113.42"

	// Send 60 requests (the limit): all should succeed
	for i := 0; i < 60; i++ {
		rec := send(handler, http.MethodPost, "/route", ip, nil)
		// /route may return 400/404/422 if not implemented or request is invalid,
		// but never 429 under the limit
		if rec.Code == http.StatusTooManyRequests {
			t.Errorf("request %d: got 429, want not rate-limited", i+1)
		}
	}

	// 61st request: rate limit exceeded
	rec := send(handler, http.MethodPost, "/route", ip, nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("request 61: status %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header missing")
	}
	body := rec.Body.String()
	if body == "" || rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("response body or Content-Type: got %q, want JSON with rate limit error", body)
	}
}

// TestAB03_RateLimitPerIP: the rate limiter is per client IP, not global.
func TestAB03_RateLimitPerIP(t *testing.T) {
	clock := &testClock{t: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	s := web.New(web.Config{PublicRateLimitPerMin: 60, AppEnv: "ci"}, web.Deps{Now: clock.now})
	handler := s.Handler()

	ip1 := "203.0.113.42"
	ip2 := "203.0.113.99"

	// IP 1 fills its limit
	for i := 0; i < 60; i++ {
		send(handler, http.MethodGet, "/", ip1, nil)
	}
	// IP 1 blocked
	rec := send(handler, http.MethodGet, "/", ip1, nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("ip1 request 61: status %d, want 429", rec.Code)
	}

	// IP 2 still has its own limit
	rec = send(handler, http.MethodGet, "/", ip2, nil)
	if rec.Code == http.StatusTooManyRequests {
		t.Errorf("ip2 request 1: got 429, want not rate-limited")
	}
}

// TestAB03_RateLimitWindowExpires: after the 1-minute window, requests are
// allowed again.
func TestAB03_RateLimitWindowExpires(t *testing.T) {
	clock := &testClock{t: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	s := web.New(web.Config{PublicRateLimitPerMin: 60, AppEnv: "ci"}, web.Deps{Now: clock.now})
	handler := s.Handler()

	ip := "203.0.113.42"

	// Fill limit
	for i := 0; i < 60; i++ {
		send(handler, http.MethodGet, "/", ip, nil)
	}
	// Blocked
	rec := send(handler, http.MethodGet, "/", ip, nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("request 61: status %d, want 429", rec.Code)
	}

	// Advance clock by 61 seconds (window expires)
	clock.t = clock.t.Add(61 * time.Second)

	// Now allowed again
	rec = send(handler, http.MethodGet, "/", ip, nil)
	if rec.Code == http.StatusTooManyRequests {
		t.Error("after window expires: got 429, want allowed")
	}
}

// TestAB03_AdminExempt: admin routes are exempt from the public rate limit.
// They have their own failed-login limiter (AB-05), not the public limiter.
func TestAB03_AdminExempt(t *testing.T) {
	clock := &testClock{t: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	s := web.New(web.Config{
		PublicRateLimitPerMin: 60,
		AppEnv:                "ci",
		AdminUser:             "admin",
		AdminPassword:         "test-only-password-0123456789",
	}, web.Deps{Now: clock.now})
	handler := s.Handler()

	ip := "203.0.113.42"

	// Send 100 requests to /admin (exceeds public limit of 60)
	for i := 0; i < 100; i++ {
		rec := send(handler, http.MethodGet, "/admin", ip, basic("admin", "test-only-password-0123456789"))
		// Admin responds with 200 or other codes, never 429 from public rate limit
		if rec.Code == http.StatusTooManyRequests {
			t.Errorf("admin request %d: got 429, want no public rate limit", i+1)
		}
	}
}

// TestAB03_XFFRightmost: the rate limiter uses the rightmost X-Forwarded-For
// entry (the one the Cloud Run front end appended), not the leftmost
// client-supplied value, per D-062. This prevents an attacker from bypassing
// the limit by sending a forged XFF header.
func TestAB03_XFFRightmost(t *testing.T) {
	clock := &testClock{t: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	// staging or prod: trustProxy=true
	s := web.New(web.Config{PublicRateLimitPerMin: 60, AppEnv: "staging"}, web.Deps{Now: clock.now})
	handler := s.Handler()

	// Attacker tries to bypass limit by sending different leftmost XFF entries,
	// but the limiter uses the rightmost entry (the real IP appended by the front end).
	for i := 0; i < 61; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "1.2.3.4:51000" // Cloud Run front end
		req.Header.Set("X-Forwarded-For", "10.0.0."+string(rune(i+1))+", 203.0.113.42")
		handler.ServeHTTP(rec, req)
		if i < 60 && rec.Code == http.StatusTooManyRequests {
			t.Errorf("request %d: got 429, want not rate-limited (rightmost XFF should be tracked)", i+1)
		}
		if i == 60 && rec.Code != http.StatusTooManyRequests {
			t.Errorf("request 61: status %d, want 429 (rightmost XFF 203.0.113.42 exceeded limit)", rec.Code)
		}
	}
}

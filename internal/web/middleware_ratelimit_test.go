package web

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPublicRateLimiter_UnderLimit(t *testing.T) {
	clock := &fixedClock{t: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	limiter := newPublicRateLimiter(60, clock.now, slog.Default())
	ip := "203.0.113.42"

	// Under limit: all 60 requests succeed
	for i := 0; i < 60; i++ {
		ok, _ := limiter.allow(ip)
		if !ok {
			t.Errorf("request %d: got blocked, want allowed", i+1)
		}
	}
}

func TestPublicRateLimiter_AtLimit(t *testing.T) {
	clock := &fixedClock{t: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	limiter := newPublicRateLimiter(60, clock.now, slog.Default())
	ip := "203.0.113.42"

	// Send limit requests
	for i := 0; i < 60; i++ {
		ok, _ := limiter.allow(ip)
		if !ok {
			t.Errorf("request %d: got blocked, want allowed", i+1)
		}
	}

	// Next request blocked
	ok, retryAfter := limiter.allow(ip)
	if ok {
		t.Error("request 61: got allowed, want blocked")
	}
	if retryAfter <= 0 {
		t.Errorf("retry_after: got %v, want > 0", retryAfter)
	}
	if retryAfter > time.Minute {
		t.Errorf("retry_after: got %v, want <= 1 minute", retryAfter)
	}
}

func TestPublicRateLimiter_AfterWindowExpires(t *testing.T) {
	clock := &fixedClock{t: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	limiter := newPublicRateLimiter(60, clock.now, slog.Default())
	ip := "203.0.113.42"

	// Fill the limit
	for i := 0; i < 60; i++ {
		limiter.allow(ip)
	}

	// Next request blocked
	if ok, _ := limiter.allow(ip); ok {
		t.Error("request 61: got allowed, want blocked")
	}

	// Advance clock by 61 seconds (window expires)
	clock.t = clock.t.Add(61 * time.Second)

	// Now allowed again
	ok, _ := limiter.allow(ip)
	if !ok {
		t.Error("after window expires: got blocked, want allowed")
	}
}

func TestPublicRateLimiter_DifferentIPs(t *testing.T) {
	clock := &fixedClock{t: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	limiter := newPublicRateLimiter(60, clock.now, slog.Default())

	// IP 1 fills its limit
	ip1 := "203.0.113.42"
	for i := 0; i < 60; i++ {
		limiter.allow(ip1)
	}
	if ok, _ := limiter.allow(ip1); ok {
		t.Error("ip1 request 61: got allowed, want blocked")
	}

	// IP 2 still has its own limit
	ip2 := "203.0.113.99"
	ok, _ := limiter.allow(ip2)
	if !ok {
		t.Error("ip2 request 1: got blocked, want allowed")
	}
}

func TestRateLimitMiddleware_Under(t *testing.T) {
	clock := &fixedClock{t: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	s := New(Config{PublicRateLimitPerMin: 60, AppEnv: "ci"}, Deps{Now: clock.now})

	handler := s.rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	// Under limit: 200 OK
	for i := 0; i < 60; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "203.0.113.42:51000"
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("request %d: status %d, want 200", i+1, rec.Code)
		}
	}
}

func TestRateLimitMiddleware_Over(t *testing.T) {
	clock := &fixedClock{t: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	s := New(Config{PublicRateLimitPerMin: 60, AppEnv: "ci"}, Deps{Now: clock.now})

	handler := s.rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	// Fill limit
	for i := 0; i < 60; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "203.0.113.42:51000"
		handler.ServeHTTP(rec, req)
	}

	// Over limit: 429
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.42:51000"
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status: got %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header missing")
	}
}

func TestRateLimitMiddleware_AdminExempt(t *testing.T) {
	clock := &fixedClock{t: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	s := New(Config{PublicRateLimitPerMin: 60, AppEnv: "ci", AdminUser: "admin", AdminPassword: "test-only-password-0123456789"}, Deps{Now: clock.now})

	// Admin routes are not wrapped with rate limit middleware, so we test by
	// checking that admin routes do not go through the public rate limiter.
	// Simulate sending many requests to /admin without hitting rate limits.
	handler := s.Handler()

	// Send 100 requests to admin (exceeds public limit of 60)
	for i := 0; i < 100; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		req.RemoteAddr = "203.0.113.42:51000"
		req.SetBasicAuth("admin", "test-only-password-0123456789")
		handler.ServeHTTP(rec, req)
		// Admin responds with 200 or 401 or 404, never 429 from public rate limit
		if rec.Code == http.StatusTooManyRequests {
			t.Errorf("admin request %d: got 429, want no public rate limit", i+1)
		}
	}
}

func TestRedactIP(t *testing.T) {
	tests := []struct {
		ip   string
		want string
	}{
		{"192.168.1.1", "192.168.1.xxx"},
		{"10.0.0.255", "10.0.0.xxx"},
		{"2001:db8::1", "2001:db8::xxx"},
		{"::1", "::xxx"},
		{"single", "xxx"},
	}
	for _, tt := range tests {
		got := redactIP(tt.ip)
		if got != tt.want {
			t.Errorf("redactIP(%q) = %q, want %q", tt.ip, got, tt.want)
		}
	}
}

// fixedClock is a settable clock for tests.
type fixedClock struct{ t time.Time }

func (c *fixedClock) now() time.Time { return c.t }

// TestRateLimitProperty_NthPlusOneBlocked: no matter the request order, the
// (N+1)th request in a window (N = limit) always gets blocked, and the first
// request after window reset gets allowed.
func TestRateLimitProperty_NthPlusOneBlocked(t *testing.T) {
	const limit = 10
	clock := &fixedClock{t: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	limiter := newPublicRateLimiter(limit, clock.now, slog.Default())
	ip := "203.0.113.42"

	// First N requests allowed
	for i := 0; i < limit; i++ {
		if ok, _ := limiter.allow(ip); !ok {
			t.Errorf("request %d: got blocked, want allowed", i+1)
		}
	}

	// (N+1)th request blocked
	if ok, _ := limiter.allow(ip); ok {
		t.Errorf("request %d: got allowed, want blocked", limit+1)
	}

	// Advance clock past window
	clock.t = clock.t.Add(61 * time.Second)

	// First request after reset allowed
	if ok, _ := limiter.allow(ip); !ok {
		t.Error("first request after reset: got blocked, want allowed")
	}
}

// TestRateLimitProperty_OrderIndependent: the rate limiter's decision is
// independent of the order of requests across different IPs.
func TestRateLimitProperty_OrderIndependent(t *testing.T) {
	const limit = 5
	clock := &fixedClock{t: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	limiter := newPublicRateLimiter(limit, clock.now, slog.Default())

	// Interleave requests from two IPs
	ip1, ip2 := "203.0.113.1", "203.0.113.2"
	for i := 0; i < limit; i++ {
		if ok, _ := limiter.allow(ip1); !ok {
			t.Errorf("ip1 request %d: got blocked, want allowed", i+1)
		}
		if ok, _ := limiter.allow(ip2); !ok {
			t.Errorf("ip2 request %d: got blocked, want allowed", i+1)
		}
	}

	// Both IPs blocked at limit+1
	if ok, _ := limiter.allow(ip1); ok {
		t.Error("ip1 request 6: got allowed, want blocked")
	}
	if ok, _ := limiter.allow(ip2); ok {
		t.Error("ip2 request 6: got allowed, want blocked")
	}
}

// TestRateLimiter_SizeCapAndSweep: the limiter bounds memory by sweeping
// expired IPs when the tracked count reaches maxRateLimitIPs.
func TestRateLimiter_SizeCapAndSweep(t *testing.T) {
	clock := &fixedClock{t: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	limiter := newPublicRateLimiter(60, clock.now, slog.Default())

	// Fill the map to maxRateLimitIPs with distinct IPs, each making 1 request
	for i := 0; i < maxRateLimitIPs; i++ {
		ip := fmt.Sprintf("192.0.2.%d", i%256) + fmt.Sprintf(".%d", i/256)
		limiter.allow(ip)
	}
	limiter.mu.Lock()
	mapSize := len(limiter.byIP)
	limiter.mu.Unlock()
	if mapSize != maxRateLimitIPs {
		t.Fatalf("after filling: map size %d, want %d", mapSize, maxRateLimitIPs)
	}

	// Advance clock by 61 seconds (all entries expire)
	clock.t = clock.t.Add(61 * time.Second)

	// A new IP triggers a sweep and gets allowed (fail open)
	newIP := "203.0.113.99"
	ok, _ := limiter.allow(newIP)
	if !ok {
		t.Error("new IP after sweep: got blocked, want allowed")
	}

	// The sweep should have cleared expired IPs, leaving only the new one
	limiter.mu.Lock()
	mapSize = len(limiter.byIP)
	limiter.mu.Unlock()
	if mapSize != 1 {
		t.Errorf("after sweep: map size %d, want 1 (only the new IP)", mapSize)
	}
}

// TestRateLimiter_FailOpen: when the map is still full after a sweep (all IPs
// still active), the limiter fails open (allows the request) rather than
// locking out legitimate IPs.
func TestRateLimiter_FailOpen(t *testing.T) {
	clock := &fixedClock{t: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	limiter := newPublicRateLimiter(60, clock.now, slog.Default())

	// Fill the map to maxRateLimitIPs with distinct IPs, each making 1 request
	for i := 0; i < maxRateLimitIPs; i++ {
		ip := fmt.Sprintf("192.0.2.%d", i%256) + fmt.Sprintf(".%d", i/256)
		limiter.allow(ip)
	}

	// Advance clock by only 30 seconds (entries NOT expired)
	clock.t = clock.t.Add(30 * time.Second)

	// A new IP triggers a sweep, but all IPs are still active, so it fails open
	newIP := "203.0.113.99"
	ok, _ := limiter.allow(newIP)
	if !ok {
		t.Error("new IP when map full (fail open): got blocked, want allowed")
	}

	// The new IP is allowed but not tracked (map still at cap)
	limiter.mu.Lock()
	mapSize := len(limiter.byIP)
	_, tracked := limiter.byIP[newIP]
	limiter.mu.Unlock()
	if mapSize != maxRateLimitIPs {
		t.Errorf("after fail-open: map size %d, want %d (unchanged)", mapSize, maxRateLimitIPs)
	}
	if tracked {
		t.Error("after fail-open: new IP tracked, want not tracked (fail open)")
	}
}

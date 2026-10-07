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

// TestRateLimiter_FailClosed: when the map is still full after a sweep (all IPs
// still active), the limiter fails closed (blocks the request) to prevent an
// attacker from bypassing the limit by filling the map (AB-03 bypass mitigation).
func TestRateLimiter_FailClosed(t *testing.T) {
	clock := &fixedClock{t: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	limiter := newPublicRateLimiter(60, clock.now, slog.Default())

	// Fill the map to maxRateLimitIPs with distinct IPs, each making 1 request
	for i := 0; i < maxRateLimitIPs; i++ {
		ip := fmt.Sprintf("192.0.2.%d", i%256) + fmt.Sprintf(".%d", i/256)
		limiter.allow(ip)
	}

	// Advance clock by only 30 seconds (entries NOT expired)
	clock.t = clock.t.Add(30 * time.Second)

	// A new IP triggers a sweep, but all IPs are still active, so it fails closed
	newIP := "203.0.113.99"
	ok, retryAfter := limiter.allow(newIP)
	if ok {
		t.Error("new IP when map full (fail closed): got allowed, want blocked")
	}
	if retryAfter != time.Minute {
		t.Errorf("retry_after: got %v, want 1 minute", retryAfter)
	}

	// The new IP is blocked and not tracked (map still at cap)
	limiter.mu.Lock()
	mapSize := len(limiter.byIP)
	_, tracked := limiter.byIP[rateLimitKey(newIP)]
	limiter.mu.Unlock()
	if mapSize != maxRateLimitIPs {
		t.Errorf("after fail-closed: map size %d, want %d (unchanged)", mapSize, maxRateLimitIPs)
	}
	if tracked {
		t.Error("after fail-closed: new IP tracked, want not tracked (fail closed)")
	}
}

// TestRateLimitKey_IPv6Prefix: IPv6 addresses are keyed by /64 prefix to
// prevent an attacker with a /64 allocation from bypassing the limit.
func TestRateLimitKey_IPv6Prefix(t *testing.T) {
	tests := []struct {
		ip   string
		want string
	}{
		// IPv4: full IP
		{"192.0.2.1", "192.0.2.1"},
		{"10.0.0.1", "10.0.0.1"},
		// IPv6: /64 prefix
		{"2001:db8::1", "2001:db8::"},
		{"2001:db8::ffff", "2001:db8::"},
		{"2001:db8:0:0:1234:5678:90ab:cdef", "2001:db8::"},
		{"2001:db8:0:1::1", "2001:db8:0:1::"},
		// IPv4-mapped IPv6: treated as IPv4, full IP
		{"::ffff:192.0.2.1", "::ffff:192.0.2.1"},
		// Unparseable: as-is
		{"not-an-ip", "not-an-ip"},
	}
	for _, tt := range tests {
		got := rateLimitKey(tt.ip)
		if got != tt.want {
			t.Errorf("rateLimitKey(%q) = %q, want %q", tt.ip, got, tt.want)
		}
	}
}

// TestRateLimiter_IPv6Sharing: multiple IPs in the same /64 share a limit.
func TestRateLimiter_IPv6Sharing(t *testing.T) {
	clock := &fixedClock{t: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	limiter := newPublicRateLimiter(5, clock.now, slog.Default())

	// Two IPs in the same /64
	ip1 := "2001:db8::1"
	ip2 := "2001:db8::ffff"

	// Fill the limit from ip1
	for i := 0; i < 5; i++ {
		limiter.allow(ip1)
	}

	// ip2 is blocked because it shares the /64 with ip1
	ok, _ := limiter.allow(ip2)
	if ok {
		t.Error("ip2 in same /64: got allowed, want blocked (shares limit with ip1)")
	}
}

// TestRateLimiter_SweepAtMostOncePerMinute: the sweep runs at most once per
// minute to avoid O(n) sweeps per new-IP request at the cap.
func TestRateLimiter_SweepAtMostOncePerMinute(t *testing.T) {
	clock := &fixedClock{t: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	limiter := newPublicRateLimiter(60, clock.now, slog.Default())

	// Fill the map to maxRateLimitIPs with distinct IPs
	for i := 0; i < maxRateLimitIPs; i++ {
		ip := fmt.Sprintf("192.0.2.%d", i%256) + fmt.Sprintf(".%d", i/256)
		limiter.allow(ip)
	}

	// Advance by 61 seconds (all entries expire)
	clock.t = clock.t.Add(61 * time.Second)

	// First new IP triggers a sweep and gets allowed
	ip1 := "203.0.113.1"
	ok1, _ := limiter.allow(ip1)
	if !ok1 {
		t.Error("ip1 after expiry: got blocked, want allowed (sweep clears map)")
	}

	// Check that lastSweep was set
	limiter.mu.Lock()
	sweepTime := limiter.lastSweep
	mapSize := len(limiter.byIP)
	limiter.mu.Unlock()
	if sweepTime.IsZero() {
		t.Error("lastSweep not set after sweep")
	}
	if mapSize != 1 {
		t.Errorf("map size after sweep: got %d, want 1 (only ip1)", mapSize)
	}

	// Advance by only 30 seconds (less than 1 minute since last sweep)
	clock.t = clock.t.Add(30 * time.Second)

	// Fill the map again to the cap
	for i := 0; i < maxRateLimitIPs-1; i++ {
		ip := fmt.Sprintf("10.0.%d.%d", i/256, i%256)
		limiter.allow(ip)
	}

	// Another new IP tries to trigger a sweep, but it's too soon (< 1 min)
	// so the sweep doesn't run, and the IP is blocked (fail closed)
	ip2 := "203.0.113.2"
	ok2, _ := limiter.allow(ip2)
	if ok2 {
		t.Error("ip2 when map full (< 1 min since sweep): got allowed, want blocked (no sweep)")
	}

	// Verify lastSweep is unchanged
	limiter.mu.Lock()
	sweepTime2 := limiter.lastSweep
	limiter.mu.Unlock()
	if !sweepTime2.Equal(sweepTime) {
		t.Errorf("lastSweep changed: got %v, want %v (no sweep < 1 min)", sweepTime2, sweepTime)
	}

	// Advance by 31 more seconds (now > 1 min since last sweep)
	clock.t = clock.t.Add(31 * time.Second)

	// Expire all current entries
	clock.t = clock.t.Add(61 * time.Second)

	// Now a new IP can trigger a sweep again
	ip3 := "203.0.113.3"
	ok3, _ := limiter.allow(ip3)
	if !ok3 {
		t.Error("ip3 after 1+ min and expiry: got blocked, want allowed (sweep runs)")
	}

	// Verify lastSweep was updated
	limiter.mu.Lock()
	sweepTime3 := limiter.lastSweep
	limiter.mu.Unlock()
	if sweepTime3.Equal(sweepTime) {
		t.Error("lastSweep not updated after 1+ min")
	}
}

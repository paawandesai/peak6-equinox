package web

import (
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// publicRateLimiter limits requests per client IP in a sliding window. Each IP
// gets requestsPerMin requests per minute (60 s); beyond that, requests get 429
// until the oldest request leaves the window.
type publicRateLimiter struct {
	requestsPerMin int
	now            func() time.Time
	log            *slog.Logger
	mu             sync.Mutex
	byIP           map[string][]time.Time // request times, oldest first
}

func newPublicRateLimiter(requestsPerMin int, now func() time.Time, log *slog.Logger) *publicRateLimiter {
	return &publicRateLimiter{
		requestsPerMin: requestsPerMin,
		now:            now,
		log:            log,
		byIP:           map[string][]time.Time{},
	}
}

// prune drops requests older than 1 minute and returns what is left.
func (l *publicRateLimiter) prune(ip string, now time.Time) []time.Time {
	times := l.byIP[ip]
	cutoff := now.Add(-time.Minute)
	i := 0
	for i < len(times) && !times[i].After(cutoff) {
		i++
	}
	times = times[i:]
	if len(times) == 0 {
		delete(l.byIP, ip)
	} else {
		l.byIP[ip] = times
	}
	return times
}

// allow reports whether ip may send another request, and if not, how long until
// the oldest request leaves the 1-minute window.
func (l *publicRateLimiter) allow(ip string) (ok bool, retryAfter time.Duration) {
	// No limit configured (test configs may set 0): allow all requests
	if l.requestsPerMin <= 0 {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	times := l.prune(ip, now)
	if len(times) < l.requestsPerMin {
		times = append(times, now)
		l.byIP[ip] = times
		return true, 0
	}
	return false, times[0].Add(time.Minute).Sub(now)
}

// redactIP redacts the last octet of an IPv4 address or the last group of an
// IPv6 address for privacy-aware logging (AB-03).
func redactIP(ip string) string {
	for i := len(ip) - 1; i >= 0; i-- {
		if ip[i] == '.' || ip[i] == ':' {
			return ip[:i+1] + "xxx"
		}
	}
	return "xxx"
}

// rateLimitMiddleware wraps next with the per-IP rate limiter. When the limit
// is exceeded, it returns HTTP 429 with a Retry-After header and a JSON body.
func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r, s.trustProxy)
		ok, retryAfter := s.publicRateLimiter.allow(ip)
		if !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
			s.log.Info("rate limit exceeded", "ip", redactIP(ip), "path", r.URL.Path)
			s.writeJSON(w, http.StatusTooManyRequests, map[string]any{
				"error":       "rate limit exceeded",
				"retry_after": int(retryAfter.Seconds()) + 1,
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

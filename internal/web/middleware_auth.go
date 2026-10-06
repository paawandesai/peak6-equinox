package web

import (
	"crypto/sha256"
	"crypto/subtle"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Admin brute-force limits (AB-05): after authFailureLimit failed logins from
// one IP within authFailureWindow, that IP gets 429 until the window passes.
const (
	authFailureLimit  = 10
	authFailureWindow = 10 * time.Minute
	// maxTrackedIPs bounds the limiter's memory. Beyond it, new IPs are not
	// tracked (fail open) rather than letting a flood evict or lock out the
	// real admin; Cloud Run's front end caps request rates before that.
	maxTrackedIPs = 10000
)

// failureLimiter counts failed admin logins per client IP in a sliding window.
type failureLimiter struct {
	now  func() time.Time
	mu   sync.Mutex
	byIP map[string][]time.Time // failure times, oldest first, at most authFailureLimit
}

func newFailureLimiter(now func() time.Time) *failureLimiter {
	return &failureLimiter{now: now, byIP: map[string][]time.Time{}}
}

// prune drops failures older than the window and returns what is left.
func (l *failureLimiter) prune(ip string, now time.Time) []time.Time {
	times := l.byIP[ip]
	cutoff := now.Add(-authFailureWindow)
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

// blocked reports whether ip has reached the failure limit, and how long
// until its oldest failure leaves the window.
func (l *failureLimiter) blocked(ip string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	times := l.prune(ip, now)
	if len(times) < authFailureLimit {
		return false, 0
	}
	return true, times[0].Add(authFailureWindow).Sub(now)
}

// fail records a failed login from ip.
func (l *failureLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	times := l.prune(ip, now)
	if len(times) == 0 && len(l.byIP) >= maxTrackedIPs {
		l.sweep(now)
		if len(l.byIP) >= maxTrackedIPs {
			return
		}
	}
	times = append(times, now)
	if len(times) > authFailureLimit {
		times = times[len(times)-authFailureLimit:]
	}
	l.byIP[ip] = times
}

// reset forgets ip's failures after a successful login.
func (l *failureLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.byIP, ip)
}

// sweep drops every IP whose failures have all left the window.
func (l *failureLimiter) sweep(now time.Time) {
	for ip := range l.byIP {
		l.prune(ip, now)
	}
}

// adminAuth wraps next with the /admin/* protections, in this order: the
// per-IP lockout (AB-05), HTTP basic auth with a constant-time compare, and
// for non-read methods the same-origin check (AB-12). Nothing from the
// Authorization header is ever logged.
func (s *Server) adminAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r, s.trustProxy)
		w.Header().Set("Cache-Control", "no-store")
		if blocked, retry := s.limiter.blocked(ip); blocked {
			w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
			s.log.Warn("admin login locked out", "ip", ip, "path", r.URL.Path)
			http.Error(w, "too many failed logins; try again later", http.StatusTooManyRequests)
			return
		}
		user, pass, ok := r.BasicAuth()
		if !ok || !s.credentialsMatch(user, pass) {
			s.limiter.fail(ip)
			s.log.Warn("admin login failed", "ip", ip, "path", r.URL.Path)
			w.Header().Set("WWW-Authenticate", `Basic realm="equinox admin", charset="UTF-8"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		s.limiter.reset(ip)
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if err := sameOrigin(r); err != nil {
				s.log.Warn("admin request refused by origin check", "ip", ip, "path", r.URL.Path, "error", err)
				http.Error(w, "cross-origin request refused", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// credentialsMatch compares both the user and the password in constant time.
// Both comparisons always run (no short circuit), on SHA-256 digests so the
// lengths of the real values are not observable either.
func (s *Server) credentialsMatch(user, pass string) bool {
	if s.cfg.AdminUser == "" || s.cfg.AdminPassword == "" {
		return false // admin disabled until configured
	}
	wantUser, wantPass := sha256.Sum256([]byte(s.cfg.AdminUser)), sha256.Sum256([]byte(s.cfg.AdminPassword))
	gotUser, gotPass := sha256.Sum256([]byte(user)), sha256.Sum256([]byte(pass))
	userOK := subtle.ConstantTimeCompare(wantUser[:], gotUser[:])
	passOK := subtle.ConstantTimeCompare(wantPass[:], gotPass[:])
	return userOK&passOK == 1
}

// originError says which header mismatched, for the log only.
type originError struct{ header, host string }

func (e originError) Error() string {
	return e.header + " host " + strconv.Quote(e.host) + " is not this app"
}

// sameOrigin refuses a mutating request whose Origin or Referer names another
// host. Both absent is allowed: non-browser clients (Cloud Scheduler, curl)
// send neither, and basic auth still applies. A browser always sends Origin
// on a cross-site POST, so a forged form cannot pass.
func sameOrigin(r *http.Request) error {
	for _, h := range [...]string{"Origin", "Referer"} {
		v := r.Header.Get(h)
		if v == "" {
			continue
		}
		u, err := url.Parse(v)
		if err != nil || u.Host == "" || !strings.EqualFold(u.Host, r.Host) {
			return originError{header: h, host: v}
		}
	}
	return nil
}

// clientIP is the peer address, or behind Cloud Run's front end the last
// X-Forwarded-For entry (the one the front end appended; earlier entries are
// client-supplied and untrusted).
func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

package web

import (
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFormatTime(t *testing.T) {
	tests := []struct {
		name string
		t    time.Time
		tz   string
		want string
	}{
		// Exact string tests for each timezone (Cursor round 1)
		{
			name: "ET EDT (October, DST)",
			t:    time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
			tz:   TzET,
			want: "2026-10-08 08:00:00 EDT",
		},
		{
			name: "ET EST (January, no DST)",
			t:    time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
			tz:   TzET,
			want: "2026-01-15 07:00:00 EST",
		},
		{
			name: "CT CDT (October, DST)",
			t:    time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
			tz:   TzCT,
			want: "2026-10-08 07:00:00 CDT",
		},
		{
			name: "CT CST (January, no DST)",
			t:    time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
			tz:   TzCT,
			want: "2026-01-15 06:00:00 CST",
		},
		{
			name: "UTC",
			t:    time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
			tz:   TzUTC,
			want: "2026-10-08 12:00:00 UTC",
		},
		{
			name: "zero time",
			t:    time.Time{},
			tz:   TzET,
			want: "unknown",
		},
		{
			name: "unknown tz defaults to ET",
			t:    time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
			tz:   "invalid",
			want: "2026-10-08 08:00:00 EDT",
		},
		// DST boundary case: spring forward (2am -> 3am on March 8, 2026)
		{
			name: "ET DST boundary spring",
			t:    time.Date(2026, 3, 8, 7, 30, 0, 0, time.UTC), // 2:30am ET becomes 3:30am EDT
			tz:   TzET,
			want: "2026-03-08 03:30:00 EDT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatTime(tt.t, tt.tz)
			if got != tt.want {
				t.Errorf("formatTime(%v, %s) = %q, want %q", tt.t, tt.tz, got, tt.want)
			}
		})
	}
}

func TestTimezoneFromRequest(t *testing.T) {
	tests := []struct {
		name      string
		cookieVal string
		wantTz    string
		noCookie  bool
	}{
		{
			name:      "ET cookie",
			cookieVal: "ET",
			wantTz:    TzET,
		},
		{
			name:      "CT cookie",
			cookieVal: "CT",
			wantTz:    TzCT,
		},
		{
			name:      "UTC cookie",
			cookieVal: "UTC",
			wantTz:    TzUTC,
		},
		{
			name:     "no cookie defaults to ET",
			noCookie: true,
			wantTz:   TzET,
		},
		{
			name:      "invalid cookie defaults to ET",
			cookieVal: "invalid",
			wantTz:    TzET,
		},
		{
			name:      "empty cookie defaults to ET",
			cookieVal: "",
			wantTz:    TzET,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if !tt.noCookie {
				//nolint:gosec // G124: Test cookie doesn't need security attributes
				req.AddCookie(&http.Cookie{
					Name:  timezoneCookie,
					Value: tt.cookieVal,
				})
			}
			got := timezoneFromRequest(req)
			if got != tt.wantTz {
				t.Errorf("timezoneFromRequest() = %q, want %q", got, tt.wantTz)
			}
		})
	}
}

func TestHandleTimezone(t *testing.T) {
	s := &Server{}

	tests := []struct {
		name         string
		tz           string
		referer      string
		wantStatus   int
		wantCookie   bool
		wantLocation string
	}{
		{
			name:         "valid ET",
			tz:           "ET",
			wantStatus:   http.StatusSeeOther,
			wantCookie:   true,
			wantLocation: "/",
		},
		{
			name:         "valid CT with same-host referer",
			tz:           "CT",
			referer:      "http://example.com/pairs/123",
			wantStatus:   http.StatusSeeOther,
			wantCookie:   true,
			wantLocation: "/pairs/123",
		},
		{
			name:         "valid UTC with same-host referer and query",
			tz:           "UTC",
			referer:      "http://example.com/log?kind=routing",
			wantStatus:   http.StatusSeeOther,
			wantCookie:   true,
			wantLocation: "/log?kind=routing",
		},
		{
			name:         "cross-host referer rejected",
			tz:           "ET",
			referer:      "http://evil.com/steal",
			wantStatus:   http.StatusSeeOther,
			wantCookie:   true,
			wantLocation: "/",
		},
		{
			name:         "protocol-relative URL rejected",
			tz:           "ET",
			referer:      "http://example.com//evil.com",
			wantStatus:   http.StatusSeeOther,
			wantCookie:   true,
			wantLocation: "/",
		},
		{
			name:         "invalid timezone",
			tz:           "invalid",
			wantStatus:   http.StatusBadRequest,
			wantCookie:   false,
			wantLocation: "",
		},
		{
			name:         "empty timezone",
			tz:           "",
			wantStatus:   http.StatusBadRequest,
			wantCookie:   false,
			wantLocation: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/timezone", nil)
			req.Host = "example.com"
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if tt.referer != "" {
				req.Header.Set("Referer", tt.referer)
			}
			req.PostForm = map[string][]string{
				"tz": {tt.tz},
			}
			w := httptest.NewRecorder()

			s.handleTimezone(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("handleTimezone() status = %d, want %d", w.Code, tt.wantStatus)
			}

			if tt.wantCookie {
				cookies := w.Result().Cookies()
				found := false
				for _, c := range cookies {
					if c.Name == timezoneCookie && c.Value == tt.tz {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("handleTimezone() did not set timezone cookie with value %q", tt.tz)
				}
			}

			if tt.wantLocation != "" {
				loc := w.Header().Get("Location")
				if loc != tt.wantLocation {
					t.Errorf("handleTimezone() Location = %q, want %q", loc, tt.wantLocation)
				}
			}
		})
	}
}

// TestHandleTimezoneSecureFlag tests that the Secure cookie flag is set correctly
// based on TLS and X-Forwarded-Proto header (M3-08 follow-up, Cursor round 1).
func TestHandleTimezoneSecureFlag(t *testing.T) {
	s := &Server{}

	tests := []struct {
		name           string
		useTLS         bool
		forwardedProto string
		wantSecure     bool
	}{
		{
			name:           "Secure with X-Forwarded-Proto: https",
			useTLS:         false,
			forwardedProto: "https",
			wantSecure:     true,
		},
		{
			name:           "Secure with r.TLS set",
			useTLS:         true,
			forwardedProto: "",
			wantSecure:     true,
		},
		{
			name:           "Secure with both TLS and X-Forwarded-Proto",
			useTLS:         true,
			forwardedProto: "https",
			wantSecure:     true,
		},
		{
			name:           "not Secure with X-Forwarded-Proto: http",
			useTLS:         false,
			forwardedProto: "http",
			wantSecure:     false,
		},
		{
			name:           "not Secure with no header and no TLS",
			useTLS:         false,
			forwardedProto: "",
			wantSecure:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/timezone", nil)
			req.Host = "example.com"
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if tt.forwardedProto != "" {
				req.Header.Set("X-Forwarded-Proto", tt.forwardedProto)
			}
			if tt.useTLS {
				req.TLS = &tls.ConnectionState{}
			}
			req.PostForm = map[string][]string{
				"tz": {TzET},
			}
			w := httptest.NewRecorder()

			s.handleTimezone(w, req)

			cookies := w.Result().Cookies()
			found := false
			var cookieSecure bool
			for _, c := range cookies {
				if c.Name == timezoneCookie {
					found = true
					cookieSecure = c.Secure
					break
				}
			}
			if !found {
				t.Fatalf("handleTimezone() did not set timezone cookie")
			}
			if cookieSecure != tt.wantSecure {
				t.Errorf("handleTimezone() cookie.Secure = %v, want %v", cookieSecure, tt.wantSecure)
			}
		})
	}
}

// TestFormatTimeUTCFallback tests that formatTime falls back to UTC when LoadLocation fails.
// This test injects a failing loadLocation to verify the UTC fallback path (M3-08 follow-up).
func TestFormatTimeUTCFallback(t *testing.T) {
	// Save the original loadLocation function and restore it after the test
	origLoadLocation := loadLocation
	t.Cleanup(func() {
		loadLocation = origLoadLocation
	})

	// Inject a loadLocation that always fails
	loadLocation = func(_ string) (*time.Location, error) {
		return nil, errors.New("injected LoadLocation failure for testing")
	}

	tm := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

	// With a failing loadLocation, formatTime should fall back to UTC
	got := formatTime(tm, TzET)
	want := "2026-10-08 12:00:00 UTC"
	if got != want {
		t.Errorf("formatTime(ET) with failing loadLocation = %q, want %q (UTC fallback)", got, want)
	}

	// Test CT also falls back to UTC
	got = formatTime(tm, TzCT)
	want = "2026-10-08 12:00:00 UTC"
	if got != want {
		t.Errorf("formatTime(CT) with failing loadLocation = %q, want %q (UTC fallback)", got, want)
	}

	// Test that unknown tz also falls back to UTC when loadLocation fails
	got = formatTime(tm, "invalid")
	want = "2026-10-08 12:00:00 UTC"
	if got != want {
		t.Errorf("formatTime(invalid) with failing loadLocation = %q, want %q (UTC fallback)", got, want)
	}

	// UTC timezone doesn't use loadLocation, so it should still work
	got = formatTime(tm, TzUTC)
	want = "2026-10-08 12:00:00 UTC"
	if got != want {
		t.Errorf("formatTime(UTC) = %q, want %q (UTC never needs loadLocation)", got, want)
	}
}

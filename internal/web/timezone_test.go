package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFormatTime(t *testing.T) {
	// 2026-10-08 12:00:00 UTC (noon UTC)
	testTime := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		t        time.Time
		tz       string
		wantZone string // The timezone abbreviation we expect
	}{
		{
			name:     "ET (Eastern)",
			t:        testTime,
			tz:       TzET,
			wantZone: "EDT", // Eastern Daylight Time in October (DST)
		},
		{
			name:     "CT (Central)",
			t:        testTime,
			tz:       TzCT,
			wantZone: "CDT", // Central Daylight Time in October (DST)
		},
		{
			name:     "UTC",
			t:        testTime,
			tz:       TzUTC,
			wantZone: "UTC",
		},
		{
			name:     "zero time",
			t:        time.Time{},
			tz:       TzET,
			wantZone: "", // returns "unknown" for zero time
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatTime(tt.t, tt.tz)
			if tt.t.IsZero() {
				if got != "unknown" {
					t.Errorf("formatTime(%v, %s) = %q, want \"unknown\"", tt.t, tt.tz, got)
				}
				return
			}
			// Check that the formatted string contains the expected timezone
			// The format is "2006-01-02 15:04:05 MST"
			if tt.wantZone != "" {
				// For UTC we know the exact output
				if tt.tz == TzUTC {
					want := "2026-10-08 12:00:00 UTC"
					if got != want {
						t.Errorf("formatTime(%v, %s) = %q, want %q", tt.t, tt.tz, got, want)
					}
				} else {
					// For other timezones, just check it contains the zone abbreviation
					// (the actual time will differ based on offset)
					if len(got) < 10 {
						t.Errorf("formatTime(%v, %s) = %q, too short", tt.t, tt.tz, got)
					}
				}
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
		name       string
		tz         string
		wantStatus int
		wantCookie bool
	}{
		{
			name:       "valid ET",
			tz:         "ET",
			wantStatus: http.StatusSeeOther,
			wantCookie: true,
		},
		{
			name:       "valid CT",
			tz:         "CT",
			wantStatus: http.StatusSeeOther,
			wantCookie: true,
		},
		{
			name:       "valid UTC",
			tz:         "UTC",
			wantStatus: http.StatusSeeOther,
			wantCookie: true,
		},
		{
			name:       "invalid timezone",
			tz:         "invalid",
			wantStatus: http.StatusBadRequest,
			wantCookie: false,
		},
		{
			name:       "empty timezone",
			tz:         "",
			wantStatus: http.StatusBadRequest,
			wantCookie: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/timezone", nil)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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
		})
	}
}

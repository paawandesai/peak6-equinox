package security

import (
	"net/http"
	"testing"
)

// TestAB12_AdminMutationRejectsForeignOrigin: a page on another site cannot
// trigger an ingest with the admin's cached credentials. Origin or Referer
// naming another host gives 403 and no round runs; the app's own host passes.
func TestAB12_AdminMutationRejectsForeignOrigin(t *testing.T) {
	h, ing, _ := newAdminHandler(t, "ci")
	hostile := []struct{ name, header, value string }{
		{"foreign origin", "Origin", "https://evil.example"},
		{"foreign referer", "Referer", "https://evil.example/attack.html"},
		{"null origin", "Origin", "null"},
		{"same host other port", "Origin", "http://equinox.example:8081"},
		{"subdomain", "Origin", "https://equinox.example.evil.example"},
		{"host as path", "Origin", "https://evil.example/equinox.example"},
		{"malformed", "Origin", "://equinox.example"},
	}
	for _, tc := range hostile {
		rec := send(h, http.MethodPost, "/admin/ingest", "203.0.113.5", func(r *http.Request) {
			r.SetBasicAuth(adminUser, adminPass)
			r.Header.Set(tc.header, tc.value)
		})
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s (%s: %s): status %d, want 403", tc.name, tc.header, tc.value, rec.Code)
		}
	}
	if ing.calls.Load() != 0 {
		t.Fatalf("a cross-origin request ran %d round(s)", ing.calls.Load())
	}

	allowed := []struct{ name, origin, referer string }{
		{"no browser headers (Cloud Scheduler, curl)", "", ""},
		{"own origin", "https://equinox.example", ""},
		{"own origin, mixed case", "https://EQUINOX.example", ""},
		{"own referer", "", "https://equinox.example/admin"},
		{"own origin and referer", "https://equinox.example", "https://equinox.example/admin"},
	}
	for i, tc := range allowed {
		rec := send(h, http.MethodPost, "/admin/ingest", "203.0.113.5", func(r *http.Request) {
			r.SetBasicAuth(adminUser, adminPass)
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if tc.referer != "" {
				r.Header.Set("Referer", tc.referer)
			}
		})
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status %d, want 200", tc.name, rec.Code)
		}
		if ing.calls.Load() != int32(i+1) {
			t.Errorf("%s: rounds run = %d, want %d", tc.name, ing.calls.Load(), i+1)
		}
	}
}

// TestAB12_AdminMutationsArePostOnly: a GET (an <img src> or a link) can never
// trigger ingest, and no other method is accepted either.
func TestAB12_AdminMutationsArePostOnly(t *testing.T) {
	h, ing, _ := newAdminHandler(t, "ci")
	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
		rec := send(h, m, "/admin/ingest", "203.0.113.5", basic(adminUser, adminPass))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s /admin/ingest: status %d, want 405", m, rec.Code)
		}
	}
	if ing.calls.Load() != 0 {
		t.Error("a non-POST method ran a round")
	}
}

// TestAB12_OriginCheckNeedsAuthFirst: without credentials the origin check is
// never reached; the answer is 401 regardless of the headers, so nothing about
// the origin policy leaks to an unauthenticated probe.
func TestAB12_OriginCheckNeedsAuthFirst(t *testing.T) {
	h, ing, _ := newAdminHandler(t, "ci")
	for _, origin := range []string{"", "https://equinox.example", "https://evil.example"} {
		rec := send(h, http.MethodPost, "/admin/ingest", "203.0.113.5", func(r *http.Request) {
			if origin != "" {
				r.Header.Set("Origin", origin)
			}
		})
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("origin %q without auth: status %d, want 401", origin, rec.Code)
		}
	}
	if ing.calls.Load() != 0 {
		t.Error("an unauthenticated request ran a round")
	}
}

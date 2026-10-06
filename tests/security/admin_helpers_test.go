package security

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/collector"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/web"
)

const (
	adminUser = "admin"
	adminPass = "test-only-password-0123456789"
)

// stubIngester counts rounds; it never touches a venue or the database.
type stubIngester struct{ calls atomic.Int32 }

func (s *stubIngester) Run(context.Context) (collector.Result, error) {
	s.calls.Add(1)
	now := time.Date(2026, 10, 6, 22, 0, 0, 0, time.UTC)
	return collector.Result{RoundID: 1, Status: store.RoundOK, StartedAt: now, FinishedAt: now.Add(time.Second),
		Venues: []collector.VenueResult{{Venue: canonical.VenueKalshi, VenueStatus: store.VenueStatus{OK: true}}}}, nil
}

func (s *stubIngester) Running() (int64, bool) { return 0, false }

// testClock is a settable clock for the auth limiter.
type testClock struct{ t time.Time }

func (c *testClock) now() time.Time { return c.t }

// newAdminHandler builds the real web server with admin credentials, a stub
// ingester and a controllable clock, as the deployed app would be configured.
func newAdminHandler(t *testing.T, appEnv string) (http.Handler, *stubIngester, *testClock) {
	t.Helper()
	ck := &testClock{t: time.Date(2026, 10, 6, 22, 0, 0, 0, time.UTC)}
	ing := &stubIngester{}
	s := web.New(web.Config{AppEnv: appEnv, AdminUser: adminUser, AdminPassword: adminPass},
		web.Deps{Ingester: ing, Now: ck.now})
	return s.Handler(), ing, ck
}

// send performs one request from ip (RemoteAddr) and returns the recorder.
func send(h http.Handler, method, path, ip string, mod func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://equinox.example"+path, nil)
	r.Host = "equinox.example"
	r.RemoteAddr = ip + ":51000"
	if mod != nil {
		mod(r)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func basic(user, pass string) func(*http.Request) {
	return func(r *http.Request) { r.SetBasicAuth(user, pass) }
}

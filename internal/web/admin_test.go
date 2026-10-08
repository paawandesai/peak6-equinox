package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/collector"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store/testdb"
)

var tdb testdb.DB

func TestMain(m *testing.M) { os.Exit(tdb.Run(m)) }

const (
	testUser = "admin"
	testPass = "test-only-password-0123456789"
)

var t0 = time.Date(2026, 10, 6, 22, 0, 0, 0, time.UTC)

// fakeIngester scripts collector results.
type fakeIngester struct {
	res       collector.Result
	err       error
	runningID int64
	calls     atomic.Int32
}

func (f *fakeIngester) Run(context.Context) (collector.Result, error) {
	f.calls.Add(1)
	return f.res, f.err
}

func (f *fakeIngester) Running() (int64, bool) { return f.runningID, f.runningID != 0 }

func okResult() collector.Result {
	return collector.Result{RoundID: 12, Status: store.RoundOK, StartedAt: t0, FinishedAt: t0.Add(41 * time.Second), Venues: []collector.VenueResult{
		{Venue: canonical.VenueKalshi, VenueStatus: store.VenueStatus{OK: true, Instruments: 955, Quotes: 400, DurationMS: 41000}},
		{Venue: canonical.VenuePolymarket, VenueStatus: store.VenueStatus{OK: true, Instruments: 700, Quotes: 400, DurationMS: 6000}},
	}}
}

// clock is a settable test clock.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newAdminServer(t *testing.T, deps Deps, appEnv string) (*Server, *clock) {
	t.Helper()
	ck := &clock{t: t0}
	deps.Now = ck.now
	return New(Config{AppEnv: appEnv, AdminUser: testUser, AdminPassword: testPass}, deps), ck
}

func request(method, path string, opts ...func(*http.Request)) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	r.RemoteAddr = "203.0.113.7:4444"
	for _, o := range opts {
		o(r)
	}
	return r
}

func withAuth(user, pass string) func(*http.Request) {
	return func(r *http.Request) { r.SetBasicAuth(user, pass) }
}

func withHeader(k, v string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set(k, v) }
}

func do(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestAdminRoutesRequireAuth(t *testing.T) {
	s, _ := newAdminServer(t, Deps{}, "ci")
	h := s.Handler()
	n := 0
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/admin"}, {http.MethodGet, "/admin/"}, {http.MethodGet, "/admin/ingest"},
		{http.MethodPost, "/admin/ingest"}, {http.MethodGet, "/admin/anything"}, {http.MethodPost, "/admin"},
		{http.MethodDelete, "/admin/rounds/1"}, {http.MethodHead, "/admin"},
	} {
		for _, auth := range []func(*http.Request){nil, withAuth("admin", "wrong"), withAuth("nobody", testPass)} {
			r := request(tc.method, tc.path)
			n++
			r.RemoteAddr = fmt.Sprintf("203.0.113.%d:4444", n) // one IP per probe, below the AB-05 lockout
			if auth != nil {
				auth(r)
			}
			rec := do(h, r)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("%s %s: status %d, want 401", tc.method, tc.path, rec.Code)
			}
			if !strings.HasPrefix(rec.Header().Get("WWW-Authenticate"), "Basic realm=") {
				t.Errorf("%s %s: WWW-Authenticate %q", tc.method, tc.path, rec.Header().Get("WWW-Authenticate"))
			}
		}
	}
}

func TestAdminDisabledWithoutCredentials(t *testing.T) {
	s := New(Config{}, Deps{})
	rec := do(s.Handler(), request(http.MethodGet, "/admin", withAuth("", "")))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("empty configured credentials must never match: %d", rec.Code)
	}
}

func TestAdminIndexWithoutStore(t *testing.T) {
	s, _ := newAdminServer(t, Deps{Ingester: &fakeIngester{runningID: 3}}, "local")
	for _, path := range []string{"/admin", "/admin/"} {
		rec := do(s.Handler(), request(http.MethodGet, path, withAuth(testUser, testPass)))
		body := rec.Body.String()
		if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("%s: status %d, type %q", path, rec.Code, rec.Header().Get("Content-Type"))
		}
		for _, want := range []string{"database not configured", "Round <strong>3</strong> is running", "No rounds yet", `action="/admin/ingest"`, "disabled"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: body lacks %q", path, want)
			}
		}
	}
	rec := do(s.Handler(), request(http.MethodHead, "/admin", withAuth(testUser, testPass)))
	if rec.Code != http.StatusOK {
		t.Errorf("HEAD /admin: %d", rec.Code)
	}
	rec = do(s.Handler(), request(http.MethodPost, "/admin", withAuth(testUser, testPass)))
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD" {
		t.Errorf("POST /admin: %d Allow %q", rec.Code, rec.Header().Get("Allow"))
	}
	rec = do(s.Handler(), request(http.MethodGet, "/admin/nothing-here", withAuth(testUser, testPass)))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /admin/nothing-here with auth: %d", rec.Code)
	}
	s2, _ := newAdminServer(t, Deps{}, "local")
	rec = do(s2.Handler(), request(http.MethodGet, "/admin?round=12abc<script>", withAuth(testUser, testPass)))
	if !strings.Contains(rec.Body.String(), "Ingest is not configured") || !strings.Contains(rec.Body.String(), "ingest round 12 finished") || strings.Contains(rec.Body.String(), "<script>") {
		t.Errorf("no-ingester page: %s", rec.Body.String())
	}
}

func TestAdminIndexRendersRounds(t *testing.T) {
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)
	ctx := context.Background()
	r1, err := st.CreateRound(ctx, t0)
	if err != nil {
		t.Fatal(err)
	}
	err = st.FinishRound(ctx, r1.ID, store.RoundPartial, map[canonical.Venue]store.VenueStatus{
		canonical.VenueKalshi:     {OK: true, Instruments: 955, Quotes: 400, DurationMS: 41000},
		canonical.VenuePolymarket: {OK: false, Error: "instruments: HTTP status 500 <b>", DurationMS: 300},
	}, t0.Add(41*time.Second+200*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateRound(ctx, t0.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	s, _ := newAdminServer(t, Deps{Store: st, Ingester: &fakeIngester{}}, "ci")
	rec := do(s.Handler(), request(http.MethodGet, "/admin", withAuth(testUser, testPass)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Last 2 rounds", `rowspan="2">partial</td>`, "<td>running</td>", "2026-10-06 22:00:00", "41.2s", "<td>955</td>", "<td>400</td>",
		"instruments: HTTP status 500 &lt;b&gt;", "<td>kalshi</td>", "<td>polymarket</td>", "Database size: <strong>",
		"Run one fetch round now",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
	}
	if strings.Contains(body, "disabled") || strings.Contains(body, "is running now") {
		t.Error("idle ingester rendered as running")
	}
}

func TestAdminIndexDatabaseUnavailable(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://nobody@127.0.0.1:1/none?connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	s, _ := newAdminServer(t, Deps{Store: store.New(pool)}, "ci")
	rec := do(s.Handler(), request(http.MethodGet, "/admin", withAuth(testUser, testPass)))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "database unavailable") {
		t.Errorf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func decodeIngest(t *testing.T, rec *httptest.ResponseRecorder) ingestResponse {
	t.Helper()
	var out ingestResponse
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type %q", ct)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding %q: %v", rec.Body.String(), err)
	}
	return out
}

func TestAdminIngestJSON(t *testing.T) {
	ing := &fakeIngester{res: okResult()}
	s, _ := newAdminServer(t, Deps{Ingester: ing}, "ci")
	rec := do(s.Handler(), request(http.MethodPost, "/admin/ingest", withAuth(testUser, testPass)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	out := decodeIngest(t, rec)
	if out.RoundID != 12 || out.Status != "ok" || out.DurationMS != 41000 || out.Venues[canonical.VenueKalshi].Quotes != 400 ||
		!out.Venues[canonical.VenuePolymarket].OK || out.StartedAt == nil || !out.StartedAt.Equal(t0) || out.Error != "" {
		t.Errorf("body = %+v", out)
	}
	if ing.calls.Load() != 1 {
		t.Errorf("ingester called %d times", ing.calls.Load())
	}

	// A failed round (every venue down) is a 502 so Cloud Scheduler sees it.
	failed := okResult()
	failed.Status = store.RoundFailed
	ing.res = failed
	rec = do(s.Handler(), request(http.MethodPost, "/admin/ingest", withAuth(testUser, testPass)))
	if rec.Code != http.StatusBadGateway || decodeIngest(t, rec).Status != "failed" {
		t.Errorf("failed round: status %d body %s", rec.Code, rec.Body.String())
	}

	ing.res, ing.err, ing.runningID = collector.Result{}, collector.ErrRoundInProgress, 13
	rec = do(s.Handler(), request(http.MethodPost, "/admin/ingest", withAuth(testUser, testPass)))
	if out := decodeIngest(t, rec); rec.Code != http.StatusConflict || out.RoundID != 13 || out.Status != "running" || out.Error == "" {
		t.Errorf("in progress: status %d body %+v", rec.Code, out)
	}

	ing.err, ing.runningID = errors.New("finishing fetch round 14: connection reset"), 0
	ing.res = collector.Result{RoundID: 14, Status: store.RoundOK}
	rec = do(s.Handler(), request(http.MethodPost, "/admin/ingest", withAuth(testUser, testPass)))
	if out := decodeIngest(t, rec); rec.Code != http.StatusInternalServerError || out.RoundID != 14 || strings.Contains(out.Error, "connection reset") {
		t.Errorf("infra error: status %d body %+v (internal detail must not leak)", rec.Code, out)
	}
}

func TestAdminIngestMethodAndAvailability(t *testing.T) {
	ing := &fakeIngester{res: okResult()}
	s, _ := newAdminServer(t, Deps{Ingester: ing}, "ci")
	for _, m := range []string{http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodHead} {
		rec := do(s.Handler(), request(m, "/admin/ingest", withAuth(testUser, testPass)))
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "POST" {
			t.Errorf("%s /admin/ingest: status %d Allow %q", m, rec.Code, rec.Header().Get("Allow"))
		}
	}
	if ing.calls.Load() != 0 {
		t.Error("a non-POST method ran a round")
	}
	s2, _ := newAdminServer(t, Deps{}, "ci")
	rec := do(s2.Handler(), request(http.MethodPost, "/admin/ingest", withAuth(testUser, testPass)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no ingester: status %d", rec.Code)
	}
}

func TestAdminIngestBrowserFormRedirects(t *testing.T) {
	ing := &fakeIngester{res: okResult()}
	s, _ := newAdminServer(t, Deps{Ingester: ing}, "ci")
	rec := do(s.Handler(), request(http.MethodPost, "/admin/ingest", withAuth(testUser, testPass),
		withHeader("Accept", "text/html,application/xhtml+xml,*/*;q=0.8"), withHeader("Origin", "http://example.com")))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin?round=12" {
		t.Errorf("status %d Location %q", rec.Code, rec.Header().Get("Location"))
	}
	// curl's default Accept is */*: JSON.
	rec = do(s.Handler(), request(http.MethodPost, "/admin/ingest", withAuth(testUser, testPass), withHeader("Accept", "*/*")))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("*/*: status %d type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}

func TestLimiterWindowAndReset(t *testing.T) {
	ck := &clock{t: t0}
	l := newFailureLimiter(ck.now)
	for i := 0; i < authFailureLimit-1; i++ {
		l.fail("a")
	}
	if blocked, _ := l.blocked("a"); blocked {
		t.Fatal("blocked one failure early")
	}
	l.fail("a")
	blocked, retry := l.blocked("a")
	if !blocked || retry != authFailureWindow {
		t.Fatalf("blocked = %v retry = %s", blocked, retry)
	}
	if blocked, _ := l.blocked("b"); blocked {
		t.Error("another IP is blocked")
	}
	// Extra failures while blocked do not extend the window past the oldest one.
	ck.t = t0.Add(5 * time.Minute)
	l.fail("a")
	if _, retry := l.blocked("a"); retry > 5*time.Minute {
		t.Errorf("retry after extra failure = %s", retry)
	}
	ck.t = t0.Add(authFailureWindow + time.Second)
	if blocked, _ := l.blocked("a"); blocked {
		t.Error("still blocked after the window")
	}
	l.fail("a")
	l.fail("a")
	l.reset("a")
	if _, ok := l.byIP["a"]; ok {
		t.Error("reset kept the IP")
	}
	for i := 0; i < 3*authFailureLimit; i++ {
		l.fail("c")
	}
	if n := len(l.byIP["c"]); n != authFailureLimit {
		t.Errorf("stored %d failures, want at most %d", n, authFailureLimit)
	}
}

func TestLimiterMemoryCap(t *testing.T) {
	ck := &clock{t: t0}
	l := newFailureLimiter(ck.now)
	for i := 0; i < maxTrackedIPs; i++ {
		l.fail("ip" + string(rune('0'+i%10)) + "-" + strings.Repeat("x", i%7) + itoa(i))
	}
	if len(l.byIP) != maxTrackedIPs {
		t.Fatalf("tracked %d", len(l.byIP))
	}
	for i := 0; i < authFailureLimit; i++ {
		l.fail("overflow")
	}
	if blocked, _ := l.blocked("overflow"); blocked {
		t.Error("an IP beyond the cap was tracked (should fail open)")
	}
	// Once the old failures expire, a sweep makes room again.
	ck.t = t0.Add(authFailureWindow + time.Second)
	for i := 0; i < authFailureLimit; i++ {
		l.fail("overflow")
	}
	if blocked, _ := l.blocked("overflow"); !blocked {
		t.Error("the sweep did not free room")
	}
}

func itoa(i int) string {
	return strings.TrimLeft(strings.Map(func(r rune) rune { return r }, string(rune('0'+i/1000%10))+string(rune('0'+i/100%10))+string(rune('0'+i/10%10))+string(rune('0'+i%10))), "")
}

func TestClientIP(t *testing.T) {
	r := request(http.MethodGet, "/admin", withHeader("X-Forwarded-For", "10.0.0.1, 198.51.100.9"))
	if got := clientIP(r, false); got != "203.0.113.7" {
		t.Errorf("untrusted proxy: %s", got)
	}
	if got := clientIP(r, true); got != "198.51.100.9" {
		t.Errorf("trusted proxy: %s (want the last entry)", got)
	}
	r = request(http.MethodGet, "/admin", withHeader("X-Forwarded-For", " , "))
	if got := clientIP(r, true); got != "203.0.113.7" {
		t.Errorf("blank forwarded: %s", got)
	}
	r.RemoteAddr = "garbage"
	if got := clientIP(r, false); got != "garbage" {
		t.Errorf("unparseable remote: %s", got)
	}
	s, _ := newAdminServer(t, Deps{}, "prod")
	if !s.trustProxy {
		t.Error("prod should trust the front end's X-Forwarded-For")
	}
	s, _ = newAdminServer(t, Deps{}, "local")
	if s.trustProxy {
		t.Error("local must not trust X-Forwarded-For")
	}
}

func TestSameOrigin(t *testing.T) {
	cases := []struct {
		origin, referer string
		ok              bool
	}{
		{"", "", true},
		{"http://example.com", "", true},
		{"https://EXAMPLE.com", "", true},
		{"", "http://example.com/admin", true},
		{"http://example.com", "http://example.com/admin", true},
		{"http://evil.example", "", false},
		{"", "http://evil.example/admin", false},
		{"http://example.com", "http://evil.example/", false},
		{"null", "", false},
		{"http://example.com:8080", "", false},
		{"://bad", "", false},
	}
	for _, tc := range cases {
		r := request(http.MethodPost, "http://example.com/admin/ingest")
		r.Host = "example.com"
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		if tc.referer != "" {
			r.Header.Set("Referer", tc.referer)
		}
		if err := sameOrigin(r); (err == nil) != tc.ok {
			t.Errorf("origin %q referer %q: err %v, want ok=%v", tc.origin, tc.referer, err, tc.ok)
		}
	}
}

func TestHelpers(t *testing.T) {
	for n, want := range map[int64]string{512: "512 B", 2048: "2.0 KB", 5 << 20: "5.0 MB", 3 << 30: "3.0 GB"} {
		if got := humanBytes(n); got != want {
			t.Errorf("humanBytes(%d) = %q", n, got)
		}
	}
	if got := sanitizeID("abc"); got != "?" {
		t.Errorf("sanitizeID = %q", got)
	}
	if got := sanitizeID(strings.Repeat("9", 40)); len(got) != 19 {
		t.Errorf("sanitizeID length = %d", len(got))
	}
	if wantsHTML(request(http.MethodPost, "/", withHeader("Accept", "application/json, text/html"))) {
		t.Error("explicit JSON preference treated as HTML")
	}
	views := roundViews([]store.Round{{ID: 1, StartedAt: t0}})
	if len(views) != 1 || views[0].Duration != "" || len(views[0].Venues) != 0 {
		t.Errorf("running round view = %+v", views[0])
	}
}

// TestAdminUnpin: POST /admin/unpin clears the pinned round (M3-07, NFR-04).
func TestAdminUnpin(t *testing.T) {
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)
	ctx := context.Background()

	// Seed two rounds
	round1, err := st.CreateRound(ctx, t0)
	if err != nil {
		t.Fatalf("creating round 1: %v", err)
	}
	if err := st.FinishRound(ctx, round1.ID, store.RoundOK, nil, t0.Add(time.Second)); err != nil {
		t.Fatalf("finishing round 1: %v", err)
	}

	round2, err := st.CreateRound(ctx, t0.Add(time.Minute))
	if err != nil {
		t.Fatalf("creating round 2: %v", err)
	}
	if err := st.FinishRound(ctx, round2.ID, store.RoundOK, nil, t0.Add(time.Minute+time.Second)); err != nil {
		t.Fatalf("finishing round 2: %v", err)
	}

	// Pin round 1
	if err := st.PinRound(ctx, round1.ID); err != nil {
		t.Fatalf("pinning round 1: %v", err)
	}

	s, _ := newAdminServer(t, Deps{Store: st}, "ci")
	h := s.Handler()

	tests := []struct {
		name       string
		method     string
		auth       bool
		wantStatus int
		wantUnpin  bool
	}{
		{"POST with auth unpins", http.MethodPost, true, http.StatusSeeOther, true},
		{"GET rejected", http.MethodGet, true, http.StatusMethodNotAllowed, false},
		{"no auth rejected", http.MethodPost, false, http.StatusUnauthorized, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Re-pin for each test
			if err := st.PinRound(ctx, round1.ID); err != nil {
				t.Fatalf("re-pinning round 1: %v", err)
			}

			var req *http.Request
			if tt.auth {
				req = request(tt.method, "/admin/unpin", withAuth(testUser, testPass), withHeader("Accept", "text/html"))
			} else {
				req = request(tt.method, "/admin/unpin", withHeader("Accept", "text/html"))
			}
			rec := do(h, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			// Check if round is still pinned
			pinned, err := st.PinnedRound(ctx)
			isPinned := err == nil && pinned.ID != 0

			if tt.wantUnpin && isPinned {
				t.Error("expected round to be unpinned, but still pinned")
			}
			if !tt.wantUnpin && !isPinned && tt.method == http.MethodPost && tt.auth {
				t.Error("expected round to remain pinned after failed/rejected request")
			}
		})
	}
}

// TestAdminUnpinRouteOrder: /admin/unpin is registered before /admin/ catch-all (M3-07 AC).
func TestAdminUnpinRouteOrder(t *testing.T) {
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)

	s, _ := newAdminServer(t, Deps{Store: st}, "ci")
	h := s.Handler()

	// /admin/unpin should not 404 (proves it's registered before catch-all)
	rec := do(h, request(http.MethodPost, "/admin/unpin", withAuth(testUser, testPass)))
	if rec.Code == http.StatusNotFound {
		t.Error("/admin/unpin returned 404; route not registered before catch-all")
	}

	// /admin/unknown should 404 (catch-all works)
	rec = do(h, request(http.MethodGet, "/admin/unknown", withAuth(testUser, testPass)))
	if rec.Code != http.StatusNotFound {
		t.Errorf("/admin/unknown status = %d, want 404", rec.Code)
	}
}

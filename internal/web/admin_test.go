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
		// UI-08: noindex via X-Robots-Tag header
		if got := rec.Header().Get("X-Robots-Tag"); got != "noindex" {
			t.Errorf("%s: X-Robots-Tag = %q, want noindex", path, got)
		}
		// UI-08: updated markup expectations for layout and chips
		for _, want := range []string{"database not configured", "round 3 running", "No rounds yet", `action="/admin/ingest"`, "disabled"} {
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
	// UI-17: table uses data-label attributes, status is badge, health table
	for _, want := range []string{
		"Last 2 rounds", `rowspan="2"`, "partial", "running", "2026-10-06 22:00:00", "41.2s", "955", "400",
		"instruments: HTTP status 500 &lt;b&gt;", "kalshi", "polymarket", "Database size",
		"Run one fetch round now", `class="badge b-partial"`, `class="health"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
	}
	// UI-08: idle ingester should not show running state
	if strings.Contains(body, "chip-info") && strings.Contains(body, "running") {
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
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	// UI-17: Database row should show Unknown (gray) when unavailable
	for _, want := range []string{"database unavailable", "signal-unknown", "h-unknown"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
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

// TestAdminLLMVerifierStates: admin page shows the verifier states with correct precedence (UI-08, issue #40).
// Precedence: disabled > cap_reached > last_run > ready (operator review !59).
func TestAdminLLMVerifierStates(t *testing.T) {
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)

	tests := []struct {
		name         string
		apiKey       string
		callsUsed    int64
		dailyCap     int
		lastRun      *store.LLMVerifyLastRun
		wantChip     string
		wantText     string
		wantDisabled bool
	}{
		{"disabled no key", "", 0, 200, nil, "chip-muted", "disabled: ANTHROPIC_API_KEY not set", true},
		{"ready zero calls", "sk-test", 0, 200, nil, "chip-ok", "ready: 0 of 200 calls used today", false},
		{"ready some calls", "sk-test", 12, 200, nil, "chip-ok", "ready: 12 of 200 calls used today", false},
		{"cap reached", "sk-test", 200, 200, nil, "chip-danger", "cap reached: 200 of 200", true},
		{"last run", "sk-test", 5, 200, &store.LLMVerifyLastRun{Verified: 14, Downgraded: 2, CacheHits: 9, RanAt: t0}, "chip-info", "last run: 14 verified, 2 downgraded, 9 cache hits", false},
		{"cap reached hides last run", "sk-test", 200, 200, &store.LLMVerifyLastRun{Verified: 10, Downgraded: 1, CacheHits: 5, RanAt: t0}, "chip-danger", "cap reached: 200 of 200", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tdb.Reset(t)
			ctx := context.Background()

			// Seed LLM usage if needed
			if tt.callsUsed > 0 {
				if err := st.IncrementLLMUsage(ctx, t0, int(tt.callsUsed), 1000, 500); err != nil {
					t.Fatal(err)
				}
			}

			// Seed last run summary if needed (issue #40)
			if tt.lastRun != nil {
				if err := st.PutLLMVerifyLastRun(ctx, *tt.lastRun); err != nil {
					t.Fatal(err)
				}
			}

			deps := Deps{Store: st, AnthropicAPIKey: tt.apiKey, LLMDailyCallCap: tt.dailyCap}
			s, ck := newAdminServer(t, deps, "ci")
			ck.t = t0 // Match the usage day

			rec := do(s.Handler(), request(http.MethodGet, "/admin", withAuth(testUser, testPass)))
			body := rec.Body.String()

			if rec.Code != http.StatusOK {
				t.Fatalf("status %d", rec.Code)
			}

			// Check chip class and state text
			if !strings.Contains(body, tt.wantChip) {
				t.Errorf("body lacks chip class %q", tt.wantChip)
			}
			if !strings.Contains(body, tt.wantText) {
				t.Errorf("body lacks state text %q", tt.wantText)
			}

			// Check button disabled state (operator review: assert button lacks disabled in enabled cases)
			buttonIdx := strings.Index(body, "Verify equivalent pairs")
			if tt.wantDisabled && tt.apiKey == "" {
				// When disabled due to no API key, the form/button is not rendered at all
				if buttonIdx >= 0 {
					t.Error("button should not be present when API key is missing")
				}
			} else if buttonIdx < 0 {
				t.Error("verify button missing from page")
			} else {
				// Find the button tag before the text
				buttonStart := strings.LastIndex(body[:buttonIdx], "<button")
				if buttonStart < 0 {
					t.Error("button tag not found before button text")
				} else {
					buttonHTML := body[buttonStart : buttonIdx+len("Verify equivalent pairs")+100]
					if tt.wantDisabled {
						if !strings.Contains(buttonHTML, "disabled") {
							t.Errorf("button should be disabled, got: %s", buttonHTML)
						}
					} else {
						// For enabled states, check the button lacks disabled attribute
						if strings.Contains(buttonHTML, "disabled") {
							t.Errorf("button should not be disabled in enabled state, got: %s", buttonHTML)
						}
					}
				}
			}
		})
	}
}

// TestAdminPinnedUnpinnedStrip: admin page shows pinned round info in the health strip (UI-08 AC).
func TestAdminPinnedUnpinnedStrip(t *testing.T) {
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)
	ctx := context.Background()

	// Create and pin a round
	r1, err := st.CreateRound(ctx, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRound(ctx, r1.ID, store.RoundOK, nil, t0.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := st.PinRound(ctx, r1.ID); err != nil {
		t.Fatal(err)
	}

	s, _ := newAdminServer(t, Deps{Store: st}, "ci")

	// Pinned state
	rec := do(s.Handler(), request(http.MethodGet, "/admin", withAuth(testUser, testPass)))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}

	// Check health strip shows pinned round
	if !strings.Contains(body, "Pinned round") {
		t.Error("health strip lacks 'Pinned round' label")
	}
	if !strings.Contains(body, fmt.Sprintf("round %d", r1.ID)) {
		t.Errorf("health strip lacks round ID %d", r1.ID)
	}

	// Unpin
	if err := st.UnpinRounds(ctx); err != nil {
		t.Fatal(err)
	}

	// Unpinned state
	rec = do(s.Handler(), request(http.MethodGet, "/admin", withAuth(testUser, testPass)))
	body = rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}

	// Check health strip shows no pinned round
	if !strings.Contains(body, "Pinned round") {
		t.Error("health strip lacks 'Pinned round' label")
	}
	if !strings.Contains(body, "no round pinned") {
		t.Error("health strip lacks 'no round pinned' text")
	}
}

// TestAdminAB15Banner: breach renders banner banner-danger with role="alert", below threshold no banner (operator review round 2).
func TestAdminAB15Banner(t *testing.T) {
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)

	// Above threshold: banner present
	s, _ := newAdminServer(t, Deps{Store: st, DBSize: func(context.Context) (int64, error) { return 5<<30 + 1, nil }}, "ci")
	rec := do(s.Handler(), request(http.MethodGet, "/admin", withAuth(testUser, testPass)))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(body, `class="banner banner-danger"`) {
		t.Error("breach: body lacks banner banner-danger class")
	}
	if !strings.Contains(body, `role="alert"`) {
		t.Error("breach: body lacks role=alert")
	}

	// Below threshold: no banner
	s, _ = newAdminServer(t, Deps{Store: st, DBSize: func(context.Context) (int64, error) { return 5 << 30, nil }}, "ci")
	rec = do(s.Handler(), request(http.MethodGet, "/admin", withAuth(testUser, testPass)))
	body = rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if strings.Contains(body, `class="banner banner-danger"`) {
		t.Error("below threshold: banner present")
	}
}

// TestWorstHealth: worstHealth returns the correct worst signal (UI-17).
func TestWorstHealth(t *testing.T) {
	tests := []struct {
		name    string
		signals []Signal
		want    HealthState
	}{
		{
			name:    "degraded wins",
			signals: []Signal{{Class: "signal-healthy"}, {Class: "signal-degraded"}, {Class: "signal-warning"}},
			want:    HealthDegraded,
		},
		{
			name:    "warning beats healthy",
			signals: []Signal{{Class: "signal-healthy"}, {Class: "signal-warning"}},
			want:    HealthWarning,
		},
		{
			name:    "healthy alone",
			signals: []Signal{{Class: "signal-healthy"}},
			want:    HealthHealthy,
		},
		{
			name:    "unknown with nothing worse stays unknown",
			signals: []Signal{{Class: "signal-unknown"}},
			want:    HealthUnknown,
		},
		{
			name:    "unknown with healthy becomes healthy",
			signals: []Signal{{Class: "signal-unknown"}, {Class: "signal-healthy"}},
			want:    HealthHealthy,
		},
		{
			name:    "empty signals is unknown",
			signals: []Signal{},
			want:    HealthUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := worstHealth(tt.signals)
			if got != tt.want {
				t.Errorf("worstHealth() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestBuildBars: buildBars generates correct bar classes and heights (UI-17).
func TestBuildBars(t *testing.T) {
	rounds := []store.Round{
		{ID: 1, Status: store.RoundOK},
		{ID: 2, Status: store.RoundPartial},
		{ID: 3, Status: store.RoundFailed},
		{ID: 4, Status: store.RoundRunning},
	}

	bars := buildBars(rounds)

	if len(bars) != 4 {
		t.Fatalf("expected 4 bars, got %d", len(bars))
	}

	tests := []struct {
		idx    int
		class  string
		height string
		title  string
	}{
		{0, "ok", "100%", "Round 1: ok"},
		{1, "partial", "75%", "Round 2: partial"},
		{2, "failed", "50%", "Round 3: failed"},
		{3, "missing", "0", "Round 4: missing"},
	}

	for _, tt := range tests {
		bar := bars[tt.idx]
		if bar.Class != tt.class {
			t.Errorf("bar[%d].Class = %q, want %q", tt.idx, bar.Class, tt.class)
		}
		if bar.Height != tt.height {
			t.Errorf("bar[%d].Height = %q, want %q", tt.idx, bar.Height, tt.height)
		}
		if bar.Title != tt.title {
			t.Errorf("bar[%d].Title = %q, want %q", tt.idx, bar.Title, tt.title)
		}
	}
}

// TestBuildBarsLimit: buildBars limits to 24 rounds (UI-17).
func TestBuildBarsLimit(t *testing.T) {
	var rounds []store.Round
	for i := 1; i <= 30; i++ {
		rounds = append(rounds, store.Round{ID: int64(i), Status: store.RoundOK})
	}

	bars := buildBars(rounds)

	if len(bars) != 24 {
		t.Errorf("expected 24 bars from 30 rounds, got %d", len(bars))
	}
	// Should take the last 24
	if bars[0].Title != "Round 7: ok" {
		t.Errorf("first bar title = %q, want Round 7: ok", bars[0].Title)
	}
	if bars[23].Title != "Round 30: ok" {
		t.Errorf("last bar title = %q, want Round 30: ok", bars[23].Title)
	}
}

// TestAdminHealthTable: admin page renders health table with signals (UI-17).
func TestAdminHealthTable(t *testing.T) {
	s, _ := newAdminServer(t, Deps{Ingester: &fakeIngester{}}, "ci")
	rec := do(s.Handler(), request(http.MethodGet, "/admin", withAuth(testUser, testPass)))
	body := rec.Body.String()

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}

	// Check health table elements
	for _, want := range []string{
		"<table class=\"health\"",
		"<th>Component</th>",
		"<th>Status</th>",
		"<th>Signals</th>",
		">Collectors<",
		">Database<",
		">Matcher<",
		">Pinned round<",
		">LLM verifier<",
		"Freshness windows (warn→stale):", // UI-19: updated text
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
	}
}

// TestAdminLLMVerifyRedirect: POST /admin/llm-verify redirects to /admin for browser posts (issue #40).
func TestAdminLLMVerifyRedirect(t *testing.T) {
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)
	ctx := context.Background()

	// Create a round with no equivalent pairs
	r, err := st.CreateRound(ctx, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRound(ctx, r.ID, store.RoundOK, nil, t0.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	deps := Deps{Store: st, AnthropicAPIKey: "sk-test", LLMDailyCallCap: 200}
	s, _ := newAdminServer(t, deps, "ci")

	tests := []struct {
		name       string
		acceptHdr  string
		wantStatus int
		wantBody   string
	}{
		{"HTML Accept redirects", "text/html", http.StatusSeeOther, ""},
		{"JSON gets JSON", "application/json", http.StatusOK, `"verified":0`},
		{"*/* gets JSON", "*/*", http.StatusOK, `"verified":0`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(s.Handler(), request(http.MethodPost, "/admin/llm-verify", withAuth(testUser, testPass), withHeader("Accept", tt.acceptHdr)))
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusSeeOther {
				if loc := rec.Header().Get("Location"); loc != "/admin" {
					t.Errorf("Location = %q, want /admin", loc)
				}
			} else {
				if !strings.Contains(rec.Body.String(), tt.wantBody) {
					t.Errorf("body lacks %q", tt.wantBody)
				}
			}
		})
	}
}

// UI-26: Test Matcher health row states based on match runs
func TestMatcherHealthStates(t *testing.T) {
	ctx := context.Background()
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)

	// Create a round for the match runs
	round, err := st.CreateRound(ctx, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRound(ctx, round.ID, store.RoundOK, nil, t0.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		matchRun  *store.MatchRun
		now       time.Time
		wantClass string // signal class: signal-unknown, signal-healthy, signal-warning, signal-degraded
		wantText  string // substring of signal text
	}{
		{
			name:      "no run recorded",
			matchRun:  nil,
			now:       t0,
			wantClass: "signal-unknown",
			wantText:  "No match runs recorded",
		},
		{
			name: "recent run, healthy",
			matchRun: &store.MatchRun{
				RoundID:             round.ID,
				StartedAt:           t0,
				FinishedAt:          t0.Add(5 * time.Second),
				DurationMS:          5000,
				PairsCompared:       100,
				RelationshipsStored: 50,
				Outcome:             store.MatchRunOK,
			},
			now:       t0.Add(1 * time.Hour), // ~1h ago, within 6h window
			wantClass: "signal-healthy",
			wantText:  "Last run",
		},
		{
			name: "old run, warning",
			matchRun: &store.MatchRun{
				RoundID:             round.ID,
				StartedAt:           t0,
				FinishedAt:          t0.Add(5 * time.Second),
				DurationMS:          5000,
				PairsCompared:       100,
				RelationshipsStored: 50,
				Outcome:             store.MatchRunOK,
			},
			now:       t0.Add(8 * time.Hour), // ~8h ago, between 6h and 12h
			wantClass: "signal-warning",
			wantText:  "Last run",
		},
		{
			name: "very old run, degraded",
			matchRun: &store.MatchRun{
				RoundID:             round.ID,
				StartedAt:           t0,
				FinishedAt:          t0.Add(5 * time.Second),
				DurationMS:          5000,
				PairsCompared:       100,
				RelationshipsStored: 50,
				Outcome:             store.MatchRunOK,
			},
			now:       t0.Add(25 * time.Hour), // >24h ago, > 12h
			wantClass: "signal-degraded",
			wantText:  "Last run",
		},
		{
			name: "failed run, degraded",
			matchRun: &store.MatchRun{
				RoundID:             round.ID,
				StartedAt:           t0,
				FinishedAt:          t0.Add(5 * time.Second),
				DurationMS:          5000,
				PairsCompared:       0,
				RelationshipsStored: 0,
				Outcome:             store.MatchRunFailed,
			},
			now:       t0.Add(1 * time.Hour),
			wantClass: "signal-degraded",
			wantText:  "Last run failed",
		},
		{
			name: "slow run, warning",
			matchRun: &store.MatchRun{
				RoundID:             round.ID,
				StartedAt:           t0,
				FinishedAt:          t0.Add(90 * time.Second),
				DurationMS:          90000,
				PairsCompared:       100,
				RelationshipsStored: 50,
				Outcome:             store.MatchRunOK,
			},
			now:       t0.Add(1 * time.Hour),
			wantClass: "signal-warning",
			wantText:  "Last run took 90s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean DB for each subtest
			tdb.Reset(t)
			st := store.New(tdb.Pool(t))

			// Recreate round for this subtest
			round, err := st.CreateRound(ctx, t0)
			if err != nil {
				t.Fatal(err)
			}
			if err := st.FinishRound(ctx, round.ID, store.RoundOK, nil, t0.Add(30*time.Second)); err != nil {
				t.Fatal(err)
			}

			// Insert match run if provided
			if tt.matchRun != nil {
				mr := *tt.matchRun
				mr.RoundID = round.ID // Use the fresh round ID
				if _, err := st.InsertMatchRun(ctx, mr); err != nil {
					t.Fatal(err)
				}
			}

			// Build health rows
			ck := &clock{t: tt.now}
			s, _ := newAdminServer(t, Deps{Store: st}, "ci")
			s.deps.Now = ck.now

			// Fetch data as the handler does
			var latestMatchRun *store.MatchRun
			if mr, err := st.GetLatestMatchRun(ctx); err == nil {
				latestMatchRun = &mr
			} else if !isNotFound(err) {
				t.Fatal(err)
			}

			var dailyHistory []store.DailyRoundSummary
			if history, err := st.DailyRoundHistory(ctx, 90); err == nil {
				dailyHistory = history
			} else {
				t.Fatal(err)
			}

			// Build health rows
			rows := buildHealthRows(collectorStats{}, dbGrowth{}, false, "", nil, "ready", nil, latestMatchRun, dailyHistory, tt.now)

			// Find Matcher row
			var matcherRow *HealthRow
			for i := range rows {
				if rows[i].Name == "Matcher" {
					matcherRow = &rows[i]
					break
				}
			}
			if matcherRow == nil {
				t.Fatal("Matcher row not found")
			}

			// Check signals
			if len(matcherRow.Signals) == 0 {
				t.Fatal("Matcher row has no signals")
			}

			// Check that at least one signal matches
			found := false
			for _, sig := range matcherRow.Signals {
				if sig.Class == tt.wantClass && strings.Contains(sig.Text, tt.wantText) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("signal class %q with text %q not found in signals %+v", tt.wantClass, tt.wantText, matcherRow.Signals)
			}
		})
	}
}

// UI-26: Test 90-day daily history bars
func TestDailyHistoryBars(t *testing.T) {
	ctx := context.Background()
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)

	// Use the current date at noon UTC (so test works on any date and timezone)
	// Noon UTC ensures the date is the same in all US timezones (UTC-5 to UTC-8)
	now := time.Now().UTC().Truncate(24 * time.Hour).Add(12 * time.Hour)

	// Create rounds across 90 days with gaps and mixed outcomes
	days := []struct {
		daysAgo int
		status  store.RoundStatus
		rounds  int // number of rounds on this day
	}{
		{89, store.RoundOK, 1},      // oldest day
		{87, store.RoundPartial, 1}, // gap at 88
		{85, store.RoundFailed, 1},  // gap at 86
		{80, store.RoundOK, 2},      // multiple rounds, all OK
		{75, store.RoundPartial, 1}, // gaps 76-84
		{70, store.RoundOK, 1},      // gap 71-74
		{50, store.RoundFailed, 1},  // gap 51-69
		{30, store.RoundPartial, 2}, // gap 31-49, mixed partial
		{10, store.RoundOK, 1},      // gap 11-29
		{5, store.RoundFailed, 1},   // gap 6-9
		{2, store.RoundOK, 1},       // gap 3-4
		{1, store.RoundPartial, 1},  // yesterday
		// Today (day 0): no rounds (missing)
	}

	for _, d := range days {
		dayStart := now.AddDate(0, 0, -d.daysAgo)
		for i := 0; i < d.rounds; i++ {
			r, err := st.CreateRound(ctx, dayStart.Add(time.Duration(i)*time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			if err := st.FinishRound(ctx, r.ID, d.status, nil, dayStart.Add(time.Duration(i)*time.Hour+30*time.Second)); err != nil {
				t.Fatal(err)
			}
		}
	}

	// Get daily history for last 90 days
	history, err := st.DailyRoundHistory(ctx, 90)
	if err != nil {
		t.Fatal(err)
	}

	if len(history) != 90 {
		t.Fatalf("got %d history days, want 90", len(history))
	}

	// Build bars
	bars := buildDailyHistoryBars(history)

	// Verify 90 bars
	if len(bars) != 90 {
		t.Errorf("got %d bars, want 90", len(bars))
	}

	// Spot-check bar properties (bars are in descending date order from history)
	tests := []struct {
		index      int
		wantClass  string
		wantHeight string
		wantTitle  string // substring
	}{
		{0, "missing", "0", "no rounds"},    // today (daysAgo=0, newest first)
		{1, "partial", "75%", "1 partial"},  // yesterday (daysAgo=1)
		{2, "ok", "100%", "1 ok"},           // daysAgo=2
		{3, "missing", "0", "no rounds"},    // daysAgo=3 (gap)
		{5, "failed", "50%", "1 failed"},    // daysAgo=5
		{10, "ok", "100%", "1 ok"},          // daysAgo=10
		{30, "partial", "75%", "2 partial"}, // daysAgo=30 (2 rounds, both partial)
		{89, "ok", "100%", "1 ok"},          // oldest day with data (daysAgo=89)
	}

	for _, tt := range tests {
		if tt.index >= len(bars) {
			t.Errorf("bar index %d out of range", tt.index)
			continue
		}
		bar := bars[tt.index]
		if bar.Class != tt.wantClass {
			t.Errorf("bars[%d].Class = %q, want %q", tt.index, bar.Class, tt.wantClass)
		}
		if bar.Height != tt.wantHeight {
			t.Errorf("bars[%d].Height = %q, want %q", tt.index, bar.Height, tt.wantHeight)
		}
		if !strings.Contains(bar.Title, tt.wantTitle) {
			t.Errorf("bars[%d].Title = %q, want substring %q", tt.index, bar.Title, tt.wantTitle)
		}
	}

	// Verify all 90 bars are present with correct CSS classes (UI-26 acceptance criterion)
	okCount, partialCount, failedCount, missingCount := 0, 0, 0, 0
	for _, bar := range bars {
		switch bar.Class {
		case "ok":
			okCount++
		case "partial":
			partialCount++
		case "failed":
			failedCount++
		case "missing":
			missingCount++
		default:
			t.Errorf("unexpected bar class: %q", bar.Class)
		}
	}

	// Verify that all 90 days are accounted for with correct outcome classes
	total := okCount + partialCount + failedCount + missingCount
	if total != 90 {
		t.Errorf("total bars = %d (ok=%d, partial=%d, failed=%d, missing=%d), want 90", total, okCount, partialCount, failedCount, missingCount)
	}
}

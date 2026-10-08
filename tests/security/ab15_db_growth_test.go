package security

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/web"
)

// Threat-model AB-15: unbounded collector growth exhausts the database or
// the budget. The control is an alert above 5 GB (5 GiB in code) shown on
// /admin and written to the structured log, where Cloud Logging can route it.
const ab15Threshold int64 = 5 << 30

// newSizedAdmin builds the real web server with an injected database size
// and a capturing JSON logger; no database is involved.
func newSizedAdmin(size int64) (http.Handler, *bytes.Buffer) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ck := &testClock{t: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	s := web.New(web.Config{AppEnv: "prod", AdminUser: adminUser, AdminPassword: adminPass},
		web.Deps{Logger: logger, Now: ck.now, DBSize: func(context.Context) (int64, error) { return size, nil }})
	return s.Handler(), &buf
}

// TestAB15_AlertAboveThreshold: with the database size injected just above
// 5 GiB, GET /admin shows the breach next to the size and exactly one
// error-level log entry tagged alert=db_growth is emitted.
func TestAB15_AlertAboveThreshold(t *testing.T) {
	h, logs := newSizedAdmin(ab15Threshold + 1)
	rec := send(h, http.MethodGet, "/admin", "203.0.113.15", basic(adminUser, adminPass))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin: status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Database size: <strong>5.0 GB</strong>", "ALERT: database size is above the 5.0 GB growth threshold (AB-15)"} {
		if !strings.Contains(body, want) {
			t.Errorf("admin page lacks %q", want)
		}
	}
	logged := logs.String()
	for _, want := range []string{`"level":"ERROR"`, `"alert":"db_growth"`, `"abuse_case":"AB-15"`, `"size_bytes":5368709121`, `"threshold_bytes":5368709120`} {
		if !strings.Contains(logged, want) {
			t.Errorf("structured log lacks %q:\n%s", want, logged)
		}
	}
	if n := strings.Count(logged, `"alert":"db_growth"`); n != 1 {
		t.Errorf("alert emitted %d times for one check, want 1", n)
	}
}

// TestAB15_NoAlertAtOrBelowThreshold: at exactly the threshold and far
// below it, neither the page nor the log raises the alert.
func TestAB15_NoAlertAtOrBelowThreshold(t *testing.T) {
	for _, size := range []int64{ab15Threshold, ab15Threshold / 2, 0} {
		h, logs := newSizedAdmin(size)
		rec := send(h, http.MethodGet, "/admin", "203.0.113.16", basic(adminUser, adminPass))
		if rec.Code != http.StatusOK {
			t.Fatalf("size %d: status %d", size, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "ALERT:") {
			t.Errorf("size %d: page shows an alert", size)
		}
		if !strings.Contains(rec.Body.String(), "alert threshold 5.0 GB") {
			t.Errorf("size %d: page does not state the threshold", size)
		}
		if strings.Contains(logs.String(), "db_growth") {
			t.Errorf("size %d: alert logged", size)
		}
	}
}

// TestAB15_IngestTriggersCheck: the Scheduler never views /admin, so the
// check also runs after every POST /admin/ingest.
func TestAB15_IngestTriggersCheck(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	ck := &testClock{t: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	s := web.New(web.Config{AppEnv: "prod", AdminUser: adminUser, AdminPassword: adminPass},
		web.Deps{Logger: logger, Now: ck.now, Ingester: &stubIngester{},
			DBSize: func(context.Context) (int64, error) { return ab15Threshold * 2, nil }})
	rec := send(s.Handler(), http.MethodPost, "/admin/ingest", "203.0.113.17", basic(adminUser, adminPass))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /admin/ingest: status %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(buf.String(), `"alert":"db_growth"`) {
		t.Errorf("ingest did not run the growth check:\n%s", buf.String())
	}
}

// TestAB15_UnauthenticatedSeesNothing: the alert is operator information;
// an anonymous request to /admin learns neither the size nor the breach.
func TestAB15_UnauthenticatedSeesNothing(t *testing.T) {
	h, _ := newSizedAdmin(ab15Threshold * 2)
	rec := send(h, http.MethodGet, "/admin", "203.0.113.18", nil)
	if rec.Code != http.StatusUnauthorized || strings.Contains(rec.Body.String(), "GB") || strings.Contains(rec.Body.String(), "ALERT") {
		t.Errorf("anonymous /admin: status %d, body %q", rec.Code, rec.Body.String())
	}
}

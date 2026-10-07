package web

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// syntheticRounds returns n rounds at the 5-minute cadence ending just
// before now, with statuses cycled from the pattern.
func syntheticRounds(now time.Time, n int, pattern ...store.RoundStatus) []store.Round {
	out := make([]store.Round, 0, n)
	for i := 0; i < n; i++ {
		started := now.Add(-time.Duration(i+1) * collectorCadence)
		st := pattern[i%len(pattern)]
		r := store.Round{ID: int64(n - i), StartedAt: started, Status: st}
		if st != store.RoundRunning {
			r.FinishedAt = started.Add(40 * time.Second)
		}
		out = append(out, r)
	}
	return out
}

func TestCollectorSuccess(t *testing.T) {
	now := t0
	t.Run("full day all ok", func(t *testing.T) {
		st := collectorSuccess(syntheticRounds(now, 288, store.RoundOK), now)
		if st.Expected != 288 || st.Observed != 288 || st.OK != 288 || st.Missing != 0 || st.Rate != "100.00%" || !st.Meets || st.Attempts != 288 {
			t.Errorf("stats = %+v", st)
		}
	})
	t.Run("partial counts as success, failed and missing do not", func(t *testing.T) {
		// 280 rounds: 270 ok, 5 partial, 5 failed; 8 ticks missing.
		rounds := syntheticRounds(now, 280, store.RoundOK)
		for i := 0; i < 5; i++ {
			rounds[i].Status = store.RoundPartial
			rounds[10+i].Status = store.RoundFailed
		}
		st := collectorSuccess(rounds, now)
		if st.OK != 270 || st.Partial != 5 || st.Failed != 5 || st.Missing != 8 || st.Succeeded != 275 || st.Attempts != 288 {
			t.Fatalf("stats = %+v", st)
		}
		// 275 / 288 = 95.486%
		if st.Rate != "95.48%" || !st.Meets {
			t.Errorf("rate = %s, meets = %v", st.Rate, st.Meets)
		}
	})
	t.Run("below target", func(t *testing.T) {
		rounds := syntheticRounds(now, 288, store.RoundOK, store.RoundOK, store.RoundOK, store.RoundOK, store.RoundOK, store.RoundOK, store.RoundOK, store.RoundOK, store.RoundOK, store.RoundFailed)
		st := collectorSuccess(rounds, now)
		if st.Failed != 28 || st.Rate != "90.27%" || st.Meets {
			t.Errorf("stats = %+v", st)
		}
	})
	t.Run("running round is neither success nor failure", func(t *testing.T) {
		rounds := syntheticRounds(now, 288, store.RoundOK)
		rounds[0].Status, rounds[0].FinishedAt = store.RoundRunning, time.Time{}
		st := collectorSuccess(rounds, now)
		if st.Running != 1 || st.Succeeded != 287 || st.Missing != 0 || st.Rate != "99.65%" || !st.Meets {
			t.Errorf("stats = %+v", st)
		}
	})
	t.Run("rounds outside the window are ignored", func(t *testing.T) {
		rounds := syntheticRounds(now, 300, store.RoundFailed) // the last 12 fall before the window
		rounds = append(rounds, store.Round{ID: 999, StartedAt: now.Add(time.Hour), Status: store.RoundOK})
		st := collectorSuccess(rounds, now)
		if st.Observed != 288 || st.Failed != 288 || st.Rate != "0.00%" || st.Meets {
			t.Errorf("stats = %+v", st)
		}
	})
	t.Run("manual rounds beyond the schedule cap the rate at 100%", func(t *testing.T) {
		st := collectorSuccess(syntheticRounds(now, 300, store.RoundOK)[:288], now)
		extra := syntheticRounds(now, 288, store.RoundOK)
		for i := range extra {
			extra[i].StartedAt = extra[i].StartedAt.Add(time.Minute)
			extra[i].ID += 1000
		}
		st = collectorSuccess(append(syntheticRounds(now, 288, store.RoundOK), extra...), now)
		if st.Observed != 576 || st.Attempts != 576 || st.Rate != "100.00%" || st.Missing != 0 {
			t.Errorf("stats = %+v", st)
		}
	})
	t.Run("no rounds at all", func(t *testing.T) {
		st := collectorSuccess(nil, now)
		if st.Missing != 288 || st.Rate != "0.00%" || st.Meets || st.Since != "2026-10-05 22:00:00 UTC" || st.Target != "95.00%" {
			t.Errorf("stats = %+v", st)
		}
	})
	if got := formatBP(9); got != "0.09%" {
		t.Errorf("formatBP(9) = %q", got)
	}
}

// TestAdminCollectorSuccessFromStore (NFR-05): the admin page reads the
// trailing 24 h of fetch_rounds and shows the success rate against 95%.
func TestAdminCollectorSuccessFromStore(t *testing.T) {
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)
	s, ck := newAdminServer(t, Deps{Store: st}, "ci")
	ctx := context.Background()
	// 20 rounds in the window: 18 ok, 1 partial, 1 failed; one old round outside it.
	for i := 0; i < 20; i++ {
		started := t0.Add(-time.Duration(i+1) * collectorCadence)
		r, err := st.CreateRound(ctx, started)
		if err != nil {
			t.Fatal(err)
		}
		status := store.RoundOK
		switch i {
		case 3:
			status = store.RoundPartial
		case 7:
			status = store.RoundFailed
		}
		if err := st.FinishRound(ctx, r.ID, status, nil, started.Add(30*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	old, err := st.CreateRound(ctx, t0.Add(-25*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRound(ctx, old.ID, store.RoundFailed, nil, t0.Add(-25*time.Hour+time.Minute)); err != nil {
		t.Fatal(err)
	}
	ck.t = t0
	rec := do(s.Handler(), request(http.MethodGet, "/admin", withAuth(testUser, testPass)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	// 19 successes of 288 expected ticks = 6.59%; 268 ticks missing.
	for _, want := range []string{"Collector success, trailing 24h0m0s (NFR-05)", "<strong>6.59%</strong> of 288 expected 5m0s ticks succeeded (target 95.00%)",
		"<strong>BELOW TARGET</strong>", "ok 18, partial 1, failed 1, running 0, missing ticks 268 (20 rounds observed of 288 scheduled)",
		"alert threshold 5.0 GB", ", below the threshold"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
	}
	if strings.Contains(body, "ALERT:") {
		t.Error("growth alert shown below the threshold")
	}
}

// captureLog returns a JSON slog logger writing to buf.
func captureLog(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// TestAdminDBGrowthAlert (AB-15): above 5 GiB the admin page shows the breach
// and one error-level structured log entry is written; below it, neither.
func TestAdminDBGrowthAlert(t *testing.T) {
	var buf bytes.Buffer
	size := dbSizeAlertBytes + 1
	s, _ := newAdminServer(t, Deps{Logger: captureLog(&buf), DBSize: func(context.Context) (int64, error) { return size, nil }}, "prod")
	rec := do(s.Handler(), request(http.MethodGet, "/admin", withAuth(testUser, testPass)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	for _, want := range []string{"Database size: <strong>5.0 GB</strong>", "ALERT: database size is above the 5.0 GB growth threshold (AB-15)"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("body lacks %q", want)
		}
	}
	logged := buf.String()
	for _, want := range []string{`"level":"ERROR"`, `"msg":"database size above growth threshold"`, `"alert":"db_growth"`, `"abuse_case":"AB-15"`,
		`"size_bytes":5368709121`, `"threshold_bytes":5368709120`} {
		if !strings.Contains(logged, want) {
			t.Errorf("log lacks %q; got %s", want, logged)
		}
	}
	if strings.Count(logged, "db_growth") != 1 {
		t.Errorf("alert logged %d times for one page view", strings.Count(logged, "db_growth"))
	}

	// Exactly at the threshold is not a breach; well below is not either.
	for _, n := range []int64{dbSizeAlertBytes, 10 << 20} {
		buf.Reset()
		size = n
		rec = do(s.Handler(), request(http.MethodGet, "/admin", withAuth(testUser, testPass)))
		if strings.Contains(rec.Body.String(), "ALERT:") || strings.Contains(buf.String(), "db_growth") {
			t.Errorf("size %d: alert fired", n)
		}
		if !strings.Contains(rec.Body.String(), "below the threshold") {
			t.Errorf("size %d: page lacks the all-clear", n)
		}
	}

	// A failing size query degrades to "database unavailable" with no alert.
	s, _ = newAdminServer(t, Deps{Logger: captureLog(&buf), DBSize: func(context.Context) (int64, error) { return 0, errors.New("boom") }}, "prod")
	buf.Reset()
	rec = do(s.Handler(), request(http.MethodGet, "/admin", withAuth(testUser, testPass)))
	if !strings.Contains(rec.Body.String(), "database unavailable") || strings.Contains(buf.String(), "db_growth") {
		t.Errorf("size error: body/log wrong: %s", buf.String())
	}
}

// TestIngestChecksDBGrowth: the growth check also runs after each ingest
// round, so the alert fires on the Scheduler's cadence.
func TestIngestChecksDBGrowth(t *testing.T) {
	var buf bytes.Buffer
	ing := &fakeIngester{res: okResult()}
	s, _ := newAdminServer(t, Deps{Logger: captureLog(&buf), Ingester: ing,
		DBSize: func(context.Context) (int64, error) { return 6 << 30, nil }}, "prod")
	rec := do(s.Handler(), request(http.MethodPost, "/admin/ingest", withAuth(testUser, testPass)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(buf.String(), `"alert":"db_growth"`) {
		t.Errorf("ingest did not trigger the growth alert; log: %s", buf.String())
	}
}

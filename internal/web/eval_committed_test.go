package web

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestEvalRendersCommittedSummaries renders /eval with the committed
// config/eval_summary.json and config/backtest_summary.json (the files prod
// serves), so a template that calls a method the real data's types lack (the
// AsOf/GeneratedAt .Format bug, hotfix !108) fails here instead of in prod.
func TestEvalRendersCommittedSummaries(t *testing.T) {
	_, st := newTestServer(t)
	s := New(Config{
		MinRouteConfidence:  dec("0.9"),
		EvalSummaryFile:     "../../config/eval_summary.json",
		BacktestSummaryFile: "../../config/backtest_summary.json",
	}, Deps{Store: st, Now: func() time.Time { return seedNow.Add(3 * time.Minute) }})
	rec := get(s.Handler(), "/eval")
	if rec.Code != http.StatusOK {
		t.Fatalf("/eval = %d, body: %.400s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="backtest"`) {
		t.Fatal(`/eval lacks the back-test section (id="backtest")`)
	}
	if strings.Contains(body, "no back-test") && strings.Contains(body, "not available") {
		t.Fatal("/eval did not load the committed back-test summary")
	}
}

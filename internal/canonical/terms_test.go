package canonical

import (
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestTermsValidate(t *testing.T) {
	ptr := func(s string) *decimal.Decimal { v := d(s); return &v }
	utc := t0
	local := t0.In(cdt)
	cases := []struct {
		name    string
		mutate  func(tm *Terms)
		wantErr string
	}{
		{"all unknown", func(*Terms) {}, ""},
		{"parsed", func(tm *Terms) {
			tm.Subject, tm.Metric, tm.Comparator, tm.Threshold = "BTC", "price", ComparatorGT, ptr("120000")
			tm.Deadline, tm.DeadlineTZ, tm.Polarity = &utc, "America/New_York", PolarityPositive
		}, ""},
		{"between ok", func(tm *Terms) {
			tm.Comparator, tm.Threshold, tm.ThresholdHigh = ComparatorBetween, ptr("4"), ptr("4.25")
		}, ""},
		{"between reversed", func(tm *Terms) { tm.Comparator, tm.Threshold, tm.ThresholdHigh = ComparatorBetween, ptr("5"), ptr("4") }, "threshold_high is below"},
		{"high without between", func(tm *Terms) { tm.Comparator, tm.ThresholdHigh = ComparatorGT, ptr("4") }, "only allowed with comparator between"},
		{"empty subject", func(tm *Terms) { tm.Subject = "" }, "terms.subject is empty"},
		{"empty source", func(tm *Terms) { tm.ResolutionSource = "" }, "terms.resolution_source is empty"},
		{"bad comparator", func(tm *Terms) { tm.Comparator = "above" }, "comparator"},
		{"bad polarity", func(tm *Terms) { tm.Polarity = "yes" }, "polarity"},
		{"non-utc deadline", func(tm *Terms) { tm.Deadline = &local }, "terms.deadline must be UTC"},
		{"zero deadline", func(tm *Terms) { tm.Deadline = &time.Time{} }, "terms.deadline is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tm := UnknownTerms()
			tc.mutate(&tm)
			err := tm.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestQuality(t *testing.T) {
	var q Quality
	if !q.OK() {
		t.Error("empty quality not OK")
	}
	q.Add(QualityMissingField, "ticker", "no ticker")
	q.Add(QualityMissingField, "ticker", "no ticker")
	q.Add(QualityInvalidPrice, "yes_bid", strings.Repeat("x", 500))
	if len(q) != 2 {
		t.Fatalf("len = %d, want 2 (duplicate skipped)", len(q))
	}
	if !q.Has(QualityInvalidPrice) || q.Has(QualityNoBook) || q.OK() {
		t.Errorf("Has/OK wrong: %+v", q)
	}
	if n := len([]rune(q[1].Detail)); n > maxQualityDetail+1 {
		t.Errorf("detail not truncated: %d runes", n)
	}
}

func TestFeeScheduleValidate(t *testing.T) {
	ok := FeeSchedule{ID: FeeSchedulePolymarketV1}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid: %v", err)
	}
	bad := FeeSchedule{ID: "", Params: map[string]decimal.Decimal{"m": d("-0.1")}, Verified: true}
	err := bad.Validate()
	for _, want := range []string{"fee schedule id", "negative", "needs a source"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want %q", err, want)
		}
	}
}

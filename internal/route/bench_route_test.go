package route

import (
	"sort"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// BenchmarkRoute measures routing latency for UI-25 p95 AC.
func BenchmarkRoute(b *testing.B) {
	target := cand(kalshiID, nil, []canonical.Level{lv("0.52", "100")}, nil)
	partner := cand(polymarketID, nil, []canonical.Level{lv("0.51", "200")},
		rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.97"))
	partner.Relationship.BasisRisk = &canonical.BasisRisk{Score: d("0"), Components: []canonical.BasisComponent{}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := Route(buy("100"), target, []canonical.Candidate{partner}, cfg, canonical.RelEquivalent, t0)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// TestRouteP95 measures p95 latency for UI-25 AC (≤ 300ms).
func TestRouteP95(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping p95 measurement in short mode")
	}

	target := cand(kalshiID, nil, []canonical.Level{lv("0.52", "100")}, nil)
	partner := cand(polymarketID, nil, []canonical.Level{lv("0.51", "200")},
		rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.97"))
	partner.Relationship.BasisRisk = &canonical.BasisRisk{Score: d("0"), Components: []canonical.BasisComponent{}}

	const samples = 1000
	durations := make([]time.Duration, samples)

	for i := 0; i < samples; i++ {
		start := time.Now()
		_, err := Route(buy("100"), target, []canonical.Candidate{partner}, cfg, canonical.RelEquivalent, t0)
		durations[i] = time.Since(start)
		if err != nil {
			t.Fatalf("Route: %v", err)
		}
	}

	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p50 := durations[samples*50/100]
	p95 := durations[samples*95/100]
	p99 := durations[samples*99/100]

	t.Logf("Route latency over %d samples: p50=%v, p95=%v, p99=%v", samples, p50, p95, p99)
	if p95 > 300*time.Millisecond {
		t.Errorf("p95 latency %v > 300ms AC", p95)
	}
}

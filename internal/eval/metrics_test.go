package eval

import (
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	"pgregory.net/rapid"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

func fixturePredictor(t *testing.T) Predictor {
	t.Helper()
	c, err := LoadCandidates(fixtureCandidates)
	if err != nil {
		t.Fatal(err)
	}
	sug := c.Suggestions()
	return func(a, b string) (canonical.RelationshipType, bool) {
		typ, ok := sug[PairKey(a, b)]
		return typ, ok
	}
}

func TestEvaluateFixture(t *testing.T) {
	labels, err := LoadLabels(fixtureLabels)
	if err != nil {
		t.Fatal(err)
	}
	r := Evaluate(labels, fixturePredictor(t), canonical.MethodRules)
	if r.Method != canonical.MethodRules || r.Evaluated != 6 || r.Correct != 4 || len(r.Missing) != 1 || len(r.Outcomes) != 6 {
		t.Fatalf("report = %+v", r)
	}
	if r.Missing[0].AID != "kalshi:FX-A7" {
		t.Errorf("missing = %+v", r.Missing)
	}
	want := map[canonical.RelationshipType]TypeMetrics{
		canonical.RelEquivalent:           {Labelled: 2, Predicted: 2, TP: 1, FP: 1, FN: 1},
		canonical.RelEquivalentGivenFacts: {},
		canonical.RelComplement:           {Labelled: 1, Predicted: 1, TP: 1},
		canonical.RelImplies:              {Labelled: 1, Predicted: 1, TP: 1},
		canonical.RelRelated:              {Labelled: 1, Predicted: 1, FP: 1, FN: 1},
		canonical.RelUnrelated:            {Labelled: 1, Predicted: 1, TP: 1},
	}
	for typ, w := range want {
		w.Type = typ
		if got := r.Metrics(typ); got != w {
			t.Errorf("%s = %+v, want %+v", typ, got, w)
		}
	}
	if len(r.ByType) != len(canonical.RelationshipTypes()) || r.ByType[0].Type != canonical.RelEquivalent {
		t.Errorf("ByType order = %v", r.ByType)
	}
	p, ok := r.EquivalentPrecision()
	if !ok || !p.Equal(decimal.RequireFromString("0.5")) {
		t.Errorf("equivalent precision = %s, %v", p, ok)
	}
	if met, measured := r.MeetsTarget(); met || !measured {
		t.Errorf("MeetsTarget = %v, %v", met, measured)
	}
	acc, ok := r.Accuracy()
	if !ok || acc.String() != "0.6667" {
		t.Errorf("accuracy = %s", acc)
	}
	rec, ok := r.Metrics(canonical.RelRelated).Recall()
	if !ok || !rec.IsZero() {
		t.Errorf("related recall = %s %v", rec, ok)
	}
	if _, ok := r.Metrics(canonical.RelEquivalentGivenFacts).Precision(); ok {
		t.Error("precision defined with no predictions")
	}
	if _, ok := r.Metrics("bogus").Recall(); ok {
		t.Error("unknown type has metrics")
	}
	out := r.String()
	for _, want := range []string{"6 labels evaluated, 4 correct (accuracy 0.6667), 1 not evaluated",
		"equivalent                      2         2    1    1    1    0.5000    0.5000",
		"equivalent_given_facts          0         0    0    0    0       n/a       n/a",
		"BELOW target 0.95 (NFR-06)", "rules+llm: not evaluated (M3)", "not evaluated: kalshi:FX-A7 / polymarket:1007"} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
	detail := r.Detail()
	if !strings.Contains(detail, "MISS label=equivalent rules=related kalshi:FX-A2") || !strings.Contains(detail, "ok   label=complement") {
		t.Errorf("detail:\n%s", detail)
	}
}

func TestEvaluateTargetMetAndUnmeasured(t *testing.T) {
	l := Labels{Version: 1, Labels: []Label{
		{AID: "kalshi:A", BID: "polymarket:1", Label: canonical.RelEquivalent, ConfirmedBy: "h", ConfirmedOn: "2026-10-07"},
		{AID: "kalshi:B", BID: "polymarket:2", Label: canonical.RelRelated, ConfirmedBy: "h", ConfirmedOn: "2026-10-07"},
	}}
	all := func(typ canonical.RelationshipType) Predictor {
		return func(string, string) (canonical.RelationshipType, bool) { return typ, true }
	}
	r := Evaluate(l, func(a, _ string) (canonical.RelationshipType, bool) {
		if a == "kalshi:A" {
			return canonical.RelEquivalent, true
		}
		return canonical.RelRelated, true
	}, canonical.MethodRules)
	if met, measured := r.MeetsTarget(); !met || !measured {
		t.Errorf("MeetsTarget = %v %v", met, measured)
	}
	if !strings.Contains(r.String(), "equivalent precision 1.0000 on 1 predictions: target >= 0.95 met") {
		t.Errorf("report:\n%s", r.String())
	}
	r = Evaluate(l, all(canonical.RelRelated), canonical.MethodRules)
	if met, measured := r.MeetsTarget(); met || measured {
		t.Errorf("MeetsTarget = %v %v", met, measured)
	}
	if !strings.Contains(r.String(), "equivalent precision: n/a") {
		t.Errorf("report:\n%s", r.String())
	}
	// A type this build does not know counts as a miss, never as a crash.
	r = Evaluate(l, all("future_type"), canonical.MethodRules)
	if r.Correct != 0 || r.Metrics(canonical.RelEquivalent).FN != 1 || r.Metrics(canonical.RelRelated).FN != 1 {
		t.Errorf("unknown prediction: %+v", r.ByType)
	}
	// Nothing to evaluate.
	r = Evaluate(Labels{Version: 1}, all(canonical.RelEquivalent), canonical.MethodRules)
	if _, ok := r.Accuracy(); ok || r.Evaluated != 0 {
		t.Errorf("empty report = %+v", r)
	}
	if !strings.Contains(r.String(), "0 labels evaluated, 0 correct (accuracy n/a)") {
		t.Errorf("empty report:\n%s", r.String())
	}
}

// Confusion counts must always add up, whatever the labels and predictions.
func TestEvaluateCountsAddUp(t *testing.T) {
	types := canonical.RelationshipTypes()
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(0, 60).Draw(rt, "n")
		l := Labels{Version: 1}
		preds := map[string]canonical.RelationshipType{}
		for i := 0; i < n; i++ {
			a, b := canonical.InstrumentID(canonical.VenueKalshi, "K"+itoa(i)), canonical.InstrumentID(canonical.VenuePolymarket, itoa(i))
			l.Labels = append(l.Labels, Label{AID: a, BID: b, Label: rapid.SampledFrom(types).Draw(rt, "label"), ConfirmedBy: "h", ConfirmedOn: "2026-10-07"})
			if rapid.Float64Range(0, 1).Draw(rt, "known") < 0.85 {
				preds[PairKey(a, b)] = rapid.SampledFrom(types).Draw(rt, "pred")
			}
		}
		r := Evaluate(l, func(a, b string) (canonical.RelationshipType, bool) {
			typ, ok := preds[PairKey(a, b)]
			return typ, ok
		}, canonical.MethodRules)
		if r.Evaluated+len(r.Missing) != n || r.Evaluated != len(r.Outcomes) {
			rt.Fatalf("evaluated %d + missing %d != %d", r.Evaluated, len(r.Missing), n)
		}
		sumTP, sumLab, sumPred := 0, 0, 0
		for _, m := range r.ByType {
			if m.TP+m.FN != m.Labelled || m.TP+m.FP != m.Predicted {
				rt.Fatalf("%+v does not add up", m)
			}
			if p, ok := m.Precision(); ok && (p.IsNegative() || p.GreaterThan(decimal.NewFromInt(1))) {
				rt.Fatalf("precision %s out of range", p)
			}
			if rc, ok := m.Recall(); ok && (rc.IsNegative() || rc.GreaterThan(decimal.NewFromInt(1))) {
				rt.Fatalf("recall %s out of range", rc)
			}
			sumTP += m.TP
			sumLab += m.Labelled
			sumPred += m.Predicted
		}
		if sumTP != r.Correct || sumLab != r.Evaluated || sumPred != r.Evaluated {
			rt.Fatalf("totals TP %d correct %d labelled %d predicted %d evaluated %d", sumTP, r.Correct, sumLab, sumPred, r.Evaluated)
		}
	})
}

func itoa(i int) string { return decimal.NewFromInt(int64(i)).String() }

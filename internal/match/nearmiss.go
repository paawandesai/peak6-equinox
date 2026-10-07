package match

import (
	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// NearMissRelative is how close two known thresholds must be (relative to the
// larger) for a related pair to count as a near miss worth storing (D-061).
var NearMissRelative = decimal.RequireFromString("0.001")

// NearMiss reports whether a related pair is close enough to be worth a human
// look (D-061): a fact was consulted in deciding it (the pair is one fact
// away, such as a later meeting's bucket), or the thresholds are known and
// equal, or within NearMissRelative of each other. Ladder neighbours one
// strike apart are not near misses. Types other than related are always
// stored, so NearMiss is false for them.
func NearMiss(rel canonical.Relationship) bool {
	if rel.Type != canonical.RelRelated {
		return false
	}
	if len(rel.FactsUsed) > 0 {
		return true
	}
	for _, d := range rel.Diff {
		if d.Field != "threshold" {
			continue
		}
		if d.Status == canonical.DiffMatch {
			return true
		}
		if d.Status != canonical.DiffDiffer {
			return false
		}
		a, errA := decimal.NewFromString(d.A)
		b, errB := decimal.NewFromString(d.B)
		if errA != nil || errB != nil {
			return false
		}
		larger := decimal.Max(a.Abs(), b.Abs())
		if larger.IsZero() {
			return true
		}
		return a.Sub(b).Abs().Div(larger).LessThanOrEqual(NearMissRelative)
	}
	return false
}

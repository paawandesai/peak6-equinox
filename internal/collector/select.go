package collector

import (
	"sort"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// SelectForQuotes picks at most limit instruments to quote (limit <= 0 means
// all) and returns them in priority order. Categories take turns (round-robin
// in canonical order) so a large strike ladder in one category cannot starve
// another. Within a category, instruments that have not closed yet come first,
// soonest close first; already-closed ones (awaiting settlement) follow, most
// recently closed first; an unknown close time sorts last. Ties break on ID.
// `equinox record-fixtures` uses the same rule, so recorded books match what a
// round quotes.
func SelectForQuotes(ins []canonical.Instrument, limit int, now time.Time) []canonical.Instrument {
	if limit <= 0 || limit >= len(ins) {
		limit = len(ins)
	}
	byCat := map[canonical.Category][]canonical.Instrument{}
	for _, in := range ins {
		byCat[in.Category] = append(byCat[in.Category], in)
	}
	// Canonical categories first, then any other category (the collector
	// validates before selecting, so those only occur for other callers).
	cats := make([]canonical.Category, 0, len(byCat))
	for _, c := range canonical.Categories() {
		if len(byCat[c]) > 0 {
			cats = append(cats, c)
		}
	}
	var others []canonical.Category
	for c := range byCat {
		if !c.Valid() {
			others = append(others, c)
		}
	}
	sort.Slice(others, func(i, j int) bool { return others[i] < others[j] })
	cats = append(cats, others...)
	for _, c := range cats {
		list := byCat[c]
		sort.SliceStable(list, func(i, j int) bool { return quoteBefore(list[i], list[j], now) })
	}
	out := make([]canonical.Instrument, 0, limit)
	for len(out) < limit {
		progressed := false
		for _, c := range cats {
			if len(byCat[c]) == 0 || len(out) >= limit {
				continue
			}
			out = append(out, byCat[c][0])
			byCat[c] = byCat[c][1:]
			progressed = true
		}
		if !progressed {
			break
		}
	}
	return out
}

// closeClass ranks a close time: 0 still open, 1 already closed, 2 unknown.
func closeClass(in canonical.Instrument, now time.Time) int {
	switch {
	case in.CloseTime.IsZero():
		return 2
	case in.CloseTime.Before(now):
		return 1
	}
	return 0
}

// quoteBefore orders a before b in quote priority (see SelectForQuotes).
func quoteBefore(a, b canonical.Instrument, now time.Time) bool {
	ca, cb := closeClass(a, now), closeClass(b, now)
	switch {
	case ca != cb:
		return ca < cb
	case ca == 0 && !a.CloseTime.Equal(b.CloseTime):
		return a.CloseTime.Before(b.CloseTime)
	case ca == 1 && !a.CloseTime.Equal(b.CloseTime):
		return a.CloseTime.After(b.CloseTime)
	}
	return a.ID < b.ID
}

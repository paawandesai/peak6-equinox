package security

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/web"
)

// hostilePathIDs extends hostileIDs with payloads shaped for a numeric path
// segment: SQL in a number, overflow, signs, exponents and Unicode digits.
var hostilePathIDs = []string{
	"1 OR 1=1", "1;DROP TABLE relationships", "1--", "1/**/OR/**/1=1", "0", "-1", "+1", "01", "1.0", "1e3",
	"9223372036854775807", "9223372036854775808", "99999999999999999999999999", "١", "１", "1%00", "1\n", "NaN", "Infinity",
	"../1", "1/../2", "{id}", "$1", "%s",
}

// TestAB08_HostilePairIDsInPath: every injection payload in the /pairs/{id}
// path is a 404 (never a 5xx or a 200), no database error text leaks into the
// response, and no table changes.
func TestAB08_HostilePairIDsInPath(t *testing.T) {
	pool := tdb.Pool(t)
	tdb.Reset(t)
	s := store.New(pool)
	seedAB08(t, s)
	before := tableCounts(t, pool)
	h := web.New(web.Config{}, web.Deps{Store: s}).Handler()

	payloads := append(append([]string{}, hostileIDs...), hostilePathIDs...)
	for _, raw := range payloads {
		path := "/pairs/" + url.PathEscape(raw)
		rec := webGet(h, path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET /pairs/%.40q: status %d, want 404", raw, rec.Code)
		}
		for _, m := range sqlLeakMarkers {
			if strings.Contains(rec.Body.String(), m) {
				t.Errorf("GET /pairs/%.40q: body leaks %q", raw, m)
			}
		}
	}
	after := tableCounts(t, pool)
	for tbl, n := range before {
		if after[tbl] != n {
			t.Errorf("table %s row count changed: %d -> %d", tbl, n, after[tbl])
		}
	}
}

// TestAB08_HostileRouteFormIDs: injection payloads in the /route form's pair
// and target fields are rejected before any lookup (400), the real pair with
// a hostile target is rejected too, and nothing is written.
func TestAB08_HostileRouteFormIDs(t *testing.T) {
	pool := tdb.Pool(t)
	tdb.Reset(t)
	s := store.New(pool)
	seedAB08(t, s)
	// One real relationship exists (seedAB08); fetch its ID for the target case.
	rels, err := s.ListRelationships(t.Context(), store.RelationshipFilter{RoundID: 1})
	if err != nil || len(rels) != 1 {
		t.Fatalf("relationships = %v, %v", rels, err)
	}
	realPair := strconv.FormatInt(rels[0].ID, 10)
	before := tableCounts(t, pool)
	h := web.New(web.Config{MinRouteConfidence: decimal.RequireFromString("0.9")}, web.Deps{Store: s}).Handler()

	post := func(form url.Values) *httptest.ResponseRecorder {
		return webPost(h, "/route", form, true)
	}
	base := func() url.Values {
		return url.Values{"pair": {realPair}, "target": {rels[0].AID}, "side": {"buy"}, "outcome": {"yes"},
			"quantity": {"10"}, "type": {"market"}, "tif": {"IOC"}}
	}
	payloads := append(append([]string{}, hostileIDs...), hostilePathIDs...)
	for _, raw := range payloads {
		for _, field := range []string{"pair", "target"} {
			form := base()
			form.Set(field, raw)
			rec := post(form)
			// 400 from validation, or 413 when the payload alone exceeds the form size cap.
			if rec.Code != http.StatusBadRequest && rec.Code != http.StatusRequestEntityTooLarge {
				t.Errorf("POST /route %s=%.40q: status %d, want 400 or 413", field, raw, rec.Code)
			}
			for _, m := range sqlLeakMarkers {
				if strings.Contains(rec.Body.String(), m) {
					t.Errorf("POST /route %s=%.40q: body leaks %q", field, raw, m)
				}
			}
		}
	}
	// A well-formed instrument ID that is not part of the pair is rejected too.
	form := base()
	form.Set("target", "kalshi:NOT-IN-PAIR")
	if rec := post(form); rec.Code != http.StatusBadRequest {
		t.Errorf("foreign target: status %d, want 400", rec.Code)
	}
	after := tableCounts(t, pool)
	for tbl, n := range before {
		if after[tbl] != n {
			t.Errorf("table %s row count changed: %d -> %d", tbl, n, after[tbl])
		}
	}
}

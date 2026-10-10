package web

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"testing"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// TestGlossaryPage: GET /glossary returns 200 with the question line, an
// anchor for every relationship type, veto code and term, and no inline
// script or style (M5-06 AC1). It needs no database.
func TestGlossaryPage(t *testing.T) {
	s := New(Config{}, Deps{})
	rec := get(s.Handler(), "/glossary")
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()
	wantContains(t, body,
		`<p class="question">What do Equinox&#39;s terms mean?</p>`,
		`<h1>Glossary</h1>`,
		`id="relationship-types"`, `id="veto-codes"`,
		`id="term-round"`, `id="term-pinned"`, `id="term-basis-risk"`, `id="term-all-in"`, `id="term-vwap"`,
		`id="term-bps"`, `id="term-slippage"`, `id="term-confidence"`, `id="term-near-miss"`, `id="term-fact-closed"`,
		"0.01 ¢ per $1 payout", `"low" below 0.20, "moderate" 0.20 to 0.49, "high" 0.50 or more`,
		`"High" is 0.90 or more`, `"medium" is 0.70 to 0.89`,
	)
	for _, typ := range canonical.RelationshipTypes() {
		wantContains(t, body, `id="`+glossaryTypeAnchor(typ)+`"`, template.HTMLEscapeString(typeMeanings[typ]))
		if typeMeanings[typ] == "" {
			t.Errorf("type %s has no meaning", typ)
		}
	}
	for _, v := range glossaryVetoCodes {
		wantContains(t, body, `id="veto-`+v.code+`"`, `<code>`+v.code+`</code>`)
	}
	if m := regexp.MustCompile(`(?i)\sstyle=|<style|<script>|\son[a-z]+=`).FindString(body); m != "" {
		t.Errorf("glossary has inline script or style: %q", m)
	}
}

// TestGlossaryVetoCodesMatchVetoGo: the glossary's copied code list is
// exactly veto.go's codes, in order, plus "fact_did_not_close" (M5-06).
func TestGlossaryVetoCodesMatchVetoGo(t *testing.T) {
	want := append(matchVetoCodes(t), "fact_did_not_close")
	if len(glossaryVetoCodes) != len(want) {
		t.Fatalf("glossary lists %d codes, veto.go + fact_did_not_close has %d: %v", len(glossaryVetoCodes), len(want), want)
	}
	for i, v := range glossaryVetoCodes {
		if v.code != want[i] {
			t.Errorf("glossary code %d = %q, want %q", i, v.code, want[i])
		}
		if v.field == "" {
			t.Errorf("glossary code %q has no field", v.code)
		}
	}
}

// TestGlossaryCoversRenderedTypes: every relationship type badge shown on /
// and /pairs/{id} has a glossary anchor (M5-06 AC3).
func TestGlossaryCoversRenderedTypes(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	glossary := get(s.Handler(), "/glossary").Body.String()
	badgeRe := regexp.MustCompile(`class="badge b-([a-z_]+)"`)
	seen := map[string]bool{}
	for _, path := range []string{"/", "/pairs/" + strconv.FormatInt(sd.eqID, 10), "/pairs/" + strconv.FormatInt(sd.relID, 10)} {
		rec := get(s.Handler(), path)
		wantHTML(t, rec, http.StatusOK)
		for _, m := range badgeRe.FindAllStringSubmatch(rec.Body.String(), -1) {
			typ := canonical.RelationshipType(m[1])
			if !typ.Valid() {
				continue // freshness, status and other non-type badges
			}
			seen[m[1]] = true
			if !regexp.MustCompile(`id="` + regexp.QuoteMeta(glossaryTypeAnchor(typ)) + `"`).MatchString(glossary) {
				t.Errorf("%s shows type %s with no glossary anchor", path, typ)
			}
		}
	}
	if !seen["equivalent"] || !seen["related"] {
		t.Errorf("expected equivalent and related badges on the seeded pages, saw %v", seen)
	}
}

// TestGlossaryReadOnly: /glossary accepts no mutating method (AB-14).
func TestGlossaryReadOnly(t *testing.T) {
	h := New(Config{}, Deps{}).Handler()
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(m, "/glossary", nil))
		if rec.Code != http.StatusMethodNotAllowed && rec.Code != http.StatusNotFound {
			t.Errorf("%s /glossary: status %d, want 404 or 405", m, rec.Code)
		}
	}
}

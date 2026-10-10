package web

import (
	"net/http"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// glossaryPage is the data for glossary.html.
type glossaryPage struct {
	page
	Types  []glossaryType
	Vetoes []glossaryVeto
}

// glossaryType is one relationship type as the glossary defines it.
type glossaryType struct {
	Anchor   string
	Badge    Badge
	Meaning  string
	Routable bool
}

// glossaryVeto is one veto code with the term it checks and its description.
type glossaryVeto struct {
	Anchor      string
	Code        string
	Field       string
	Description string
}

// typeMeanings mirror docs/equivalence.md § Relationship types, in plain words.
var typeMeanings = map[canonical.RelationshipType]string{
	canonical.RelEquivalent:           "Same bet: YES on one contract pays exactly when YES on the other pays.",
	canonical.RelEquivalentGivenFacts: "Same bet once a stated external fact is applied, such as the current Fed upper bound.",
	canonical.RelComplement:           "Opposite sides: YES on one contract pays exactly when NO on the other pays, so you can buy the other side's NO.",
	canonical.RelImplies:              "One contract resolving YES guarantees the other resolves YES, but not the reverse. Never routed.",
	canonical.RelRelated:              "Same event, different payout: a term differs or a veto applies. Never routed.",
	canonical.RelUnrelated:            "Compared and found to be about different things.",
}

// glossaryVetoCodes copies the veto codes from internal/match/veto.go, in its
// order, with the term each one checks. TestGlossaryVetoCodesMatchVetoGo
// fails if this list and veto.go drift apart.
var glossaryVetoCodes = []struct{ code, field string }{
	{"subject_mismatch", "subject"},
	{"subject_unknown", "subject"},
	{"metric_mismatch", "metric"},
	{"metric_unknown", "metric"},
	{"comparator_mismatch", "comparator"},
	{"comparator_unknown", "comparator"},
	{"threshold_mismatch", "threshold"},
	{"threshold_unknown", "threshold"},
	{"deadline_gap", "deadline"},
	{"deadline_mismatch", "deadline"},
	{"deadline_unknown", "deadline"},
	{"source_mismatch", "resolution source"},
	{"source_unknown", "resolution source"},
	{"polarity_mismatch", "polarity"},
	{"polarity_unresolved", "polarity"},
	{"fact_did_not_close", "facts"},
}

// handleGlossary answers GET /glossary: what every relationship type, veto
// code and on-screen term means (M5-06). The content is static; the round
// is only read for the banner.
func (s *Server) handleGlossary(w http.ResponseWriter, r *http.Request) {
	data := glossaryPage{
		page:   s.pageBase(roundFromContext(r.Context()), "Glossary", "What do Equinox's terms mean?", timezoneFromRequest(r), "glossary"),
		Types:  glossaryTypes(),
		Vetoes: glossaryVetoes(),
	}
	s.render(w, http.StatusOK, "glossary", data)
}

// glossaryTypes lists every relationship type, strongest first.
func glossaryTypes() []glossaryType {
	types := canonical.RelationshipTypes()
	out := make([]glossaryType, 0, len(types))
	for _, t := range types {
		out = append(out, glossaryType{Anchor: glossaryTypeAnchor(t), Badge: TypeBadge(t), Meaning: typeMeanings[t], Routable: t.Routable()})
	}
	return out
}

// glossaryVetoes lists every veto code with its plain description.
func glossaryVetoes() []glossaryVeto {
	out := make([]glossaryVeto, 0, len(glossaryVetoCodes))
	for _, v := range glossaryVetoCodes {
		out = append(out, glossaryVeto{Anchor: "veto-" + v.code, Code: v.code, Field: v.field, Description: vetoDescription(v.code)})
	}
	return out
}

// glossaryTypeAnchor is the /glossary fragment that defines t.
func glossaryTypeAnchor(t canonical.RelationshipType) string {
	return "type-" + string(t)
}

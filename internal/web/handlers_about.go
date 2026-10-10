package web

import (
	"net/http"

	"github.com/shopspring/decimal"
)

// aboutPage is the data for about.html: what Equinox is for, how a verdict
// is made and where it stops (P12). Every fact on it comes from the repo
// docs; numbers that change with the data are linked, not copied.
type aboutPage struct {
	page
	Types []glossaryType
	Steps []aboutStep
	// SVGHeight is the pipeline diagram's height in user units.
	SVGHeight int
	// Cost formula inputs, from the running config (D-026, routing.md).
	BasisMult     string
	BasisHalf     string // the penalty at a basis-risk score of 0.5
	MinConfidence string
	Docs          []aboutDoc
}

// aboutStep is one box of the pipeline diagram with its precomputed
// coordinates, so the template holds no arithmetic.
type aboutStep struct {
	N      int
	Name   string
	Short  string // fits in the 300-unit box at 12px
	Detail string
	Y      int // top of the box
	NameY  int
	ShortY int
	// ArrowY1 and ArrowY2 are the connector to the next box; Last has none.
	ArrowY1, ArrowY2 int
	Last             bool
}

// aboutDoc is one methodology document, linked on GitLab because the app
// does not serve /docs.
type aboutDoc struct {
	Name string
	URL  string
	What string
}

// Diagram geometry, in SVG user units.
const (
	aboutBoxHeight = 44
	aboutBoxGap    = 20
	aboutPad       = 8
)

// docsBlobBase is the GitLab blob URL for docs on main.
const docsBlobBase = "https://labs.gauntletai.com/paawandesai/peak6-equinox/-/blob/main/docs/"

// defaultBasisPenaltyMult mirrors config.DefaultBasisPenaltyMult for a
// server built without config (tests).
var defaultBasisPenaltyMult = decimal.RequireFromString("0.01")

// aboutPipeline mirrors docs/equivalence.md § 2 and docs/routing.md.
var aboutPipeline = []struct{ name, short, detail string }{
	{"Collect", "Poll both venues: contract terms, books",
		"A collector polls the public, unauthenticated Kalshi and Polymarket APIs for contract terms and order books. One fetch of both venues is a round."},
	{"Normalise", "Rules text → Terms; unknown stays unknown",
		"The rules text, then the title, is read into structured terms: subject, metric, comparator, threshold, deadline, resolution source and polarity. A field that cannot be read is recorded as unknown, never guessed."},
	{"Block", "Same subject, deadlines within 7 days?",
		"A pair is a candidate only if both contracts are in the same category, have the same known subject and known deadlines within 7 days of each other. Every other pair is unrelated."},
	{"Veto / facts", "Hard vetoes; Fed facts may close a gap",
		"Every term that differs, or is unknown on either side, records a veto. For the Fed, a stated fact (the current upper bound, the 25 bp step) can close a level-versus-change gap."},
	{"Classify", "Type, confidence, basis risk, explanation",
		"No veto gives equivalent (Fed pairs that rely on the 25 bp step report equivalent_given_facts). Set algebra on a shared measurement gives complement or implies; anything else is related. Each verdict gets a confidence, a basis-risk score and a written explanation."},
	{"Route", "Lowest all-in cost over routable pairs",
		"For a simulated order, the router walks the target's book and every routable partner's book and picks the lowest all-in cost (below). Stale, shallow or skewed quotes are excluded with a reason."},
	{"Log", "Every verdict and route, with reasons",
		"Each simulated route is stored with its inputs, legs, exclusions and explanation, and listed on the decision log."},
}

// handleAbout answers GET /about: a static, read-only page for the
// evaluator (P12). The round is read only for the banner.
func (s *Server) handleAbout(w http.ResponseWriter, r *http.Request) {
	mult := s.cfg.BasisPenaltyMult
	if mult.IsZero() {
		mult = defaultBasisPenaltyMult
	}
	minConf := s.cfg.MinRouteConfidence
	if minConf.IsZero() {
		minConf = decimal.RequireFromString("0.90")
	}
	steps, height := aboutSteps()
	data := aboutPage{
		page:          s.pageBase(roundFromContext(r.Context()), "About", "What is Equinox for, how does it decide, and where does it stop?", timezoneFromRequest(r), "about"),
		Types:         glossaryTypes(),
		Steps:         steps,
		SVGHeight:     height,
		BasisMult:     mult.String(),
		BasisHalf:     mult.Div(decimal.NewFromInt(2)).String(),
		MinConfidence: minConf.StringFixed(2),
		Docs:          aboutDocs(),
	}
	s.render(w, http.StatusOK, "about", data)
}

// aboutSteps lays the pipeline out top to bottom and returns the steps with
// the diagram's total height.
func aboutSteps() ([]aboutStep, int) {
	out := make([]aboutStep, 0, len(aboutPipeline))
	for i, p := range aboutPipeline {
		y := aboutPad + i*(aboutBoxHeight+aboutBoxGap)
		out = append(out, aboutStep{
			N: i + 1, Name: p.name, Short: p.short, Detail: p.detail,
			Y: y, NameY: y + 19, ShortY: y + 36,
			ArrowY1: y + aboutBoxHeight, ArrowY2: y + aboutBoxHeight + aboutBoxGap - 2,
			Last: i == len(aboutPipeline)-1,
		})
	}
	height := 2*aboutPad + len(aboutPipeline)*aboutBoxHeight + (len(aboutPipeline)-1)*aboutBoxGap
	return out, height
}

// aboutDocs lists the methodology documents the page points to.
func aboutDocs() []aboutDoc {
	return []aboutDoc{
		{"equivalence.md", docsBlobBase + "equivalence.md", "The payout definition, the veto catalogue, the facts file, basis risk, confidence and the known failure modes."},
		{"routing.md", docsBlobBase + "routing.md", "Eligibility, the all-in cost formula, the greedy sweep, complement routing and worked examples."},
		{"decisions.md", docsBlobBase + "decisions.md", "Every decision with who made it, why, what was rejected and how to reverse it."},
		{"ai-usage.md", docsBlobBase + "ai-usage.md", "The runtime LLM verifier and the AI tools used to build Equinox."},
	}
}

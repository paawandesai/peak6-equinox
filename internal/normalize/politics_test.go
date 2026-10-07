package normalize

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"pgregory.net/rapid"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/match"
)

// Real venue text recorded on 2026-10-07 (testdata/fixtures: Kalshi
// KXPRESPERSON / KXPRESNOMD / KXPRESNOMR open events, Polymarket president and
// primaries tags). Kalshi titles carry the yes_sub_title suffix the way the
// Kalshi adapter appends it.
const (
	pmWinRules = "The 2028 US Presidential Election is scheduled to take place on November 7, 2028.\n\n" +
		"This market will resolve to the person who wins the 2028 US Presidential Election.\n\n" +
		"The resolution source for this market is the Associated Press, Fox News, and NBC. This market will resolve once all three " +
		"sources call the race for the same candidate. If all three sources haven’t called the race for the same candidate by the " +
		"inauguration date (January 20, 2029) this market will resolve based on who is inaugurated."
	pmNomRepRules = "This market will resolve to “Yes” if the named individual wins and accepts the 2028 nomination of the Republican Party " +
		"for U.S. president. Otherwise, this market will resolve to “No”.\n\n" +
		"The resolution source for this market will be a consensus of official Republican Party sources.\n\n" +
		"Any replacement of the Republican nominee before election day will not change the resolution of the market."
	pmNomDemRules = "This market will resolve to “Yes” if the named individual wins and accepts the 2028 nomination of the Democratic Party " +
		"for U.S. president. Otherwise, this market will resolve to “No”.\n\n" +
		"The resolution source for this market will be a consensus of official Democratic Party sources.\n\n" +
		"Any replacement of the democratic nominee before election day will not change the resolution of the market."
	pmVPDemRules = "This market will resolve according to the first individual to be formally nominated by the Democratic Party for Vice " +
		"President of the United States in the 2028 Presidential election cycle who accepts the nomination. \n\n" +
		"The resolution source for this market will be a consensus of official Democratic Party sources, including https://democrats.org/.\n\n" +
		"Any replacement of the nominee before election day will not change the resolution of the market."
	pmAnnounceRules = "This market will resolve to “Yes” if the listed individual officially announces that they are running for U.S. President " +
		"in the 2028 United States presidential election by December 31, 2027, 11:59 PM ET. Otherwise, this market will resolve to “No”."
	pmSCPrimaryRules = "South Carolina is expected to hold its Democratic presidential primary for the 2028 presidential election on January 22, 2028. \n\n" +
		"This market will resolve to the candidate who wins the most votes in the South Carolina Democratic primary, caucus, or other " +
		"official process by which the public directs the selection of delegates for the 2028 Democratic Convention."
	kxNomRVanceRules  = "If J.D. Vance wins and accepts the nomination for the Presidency for the Republican party in 2028, then the market resolves to Yes."
	kxNomDNewsomRules = "If Gavin Newsom wins and accepts the nomination for the Presidency for the Democratic party in 2028, then the market resolves to Yes."
	kxPresNewsomRules = "If Gavin Newsom is the next person inaugurated as President for the term beginning in 2029, then the market resolves to Yes."
)

var (
	pmPolClose  = time.Date(2028, 11, 8, 4, 59, 0, 0, time.UTC) // Polymarket endDate for the 2028 events
	kxNomClose  = time.Date(2028, 11, 7, 15, 0, 0, 0, time.UTC) // Kalshi KXPRESNOMD / KXPRESNOMR close
	kxPresClos  = time.Date(2029, 11, 7, 15, 0, 0, 0, time.UTC) // Kalshi KXPRESPERSON close
	electionDy  = time.Date(2028, 11, 7, 0, 0, 0, 0, time.UTC)  // "scheduled to take place on November 7, 2028"
	pmAnnClose  = time.Date(2028, 1, 1, 4, 59, 0, 0, time.UTC)  // announce-a-run close
	pmSCClose   = time.Date(2028, 1, 23, 4, 59, 0, 0, time.UTC) // South Carolina primary close
	scPrimaryDy = time.Date(2028, 1, 22, 0, 0, 0, 0, time.UTC)  // "hold its ... primary ... on January 22, 2028"
	one         = decimal.NewFromInt(1)
	pmPolID     = "polymarket:12345678901234567890"
	kxPolID     = "kalshi:KXPRESNOMR-28-JDV"
	pmPolID2    = "polymarket:98765432109876543210"
	kxPolDefDl  = kxNomClose
)

func politics(id, title, rules string, closeTime time.Time) canonical.Instrument {
	in := crypto(title, rules, closeTime)
	in.ID, in.Category = id, canonical.CategoryPolitics
	in.Venue = canonical.VenueOf(id)
	return in
}

func TestExtractPoliticsRealVenueText(t *testing.T) {
	defaulted := []canonical.QualityFlag{canonical.QualityDeadlineDefaulted}
	dateOnly := []canonical.QualityFlag{QualityDeadlineDateOnly}
	cases := []struct {
		name string
		in   canonical.Instrument
		want want
	}{
		{"polymarket presidential winner", politics(pmPolID, "Will JD Vance win the 2028 US Presidential Election?", pmWinRules, pmPolClose), want{
			"jd_vance", "win_election:us_president:2028", canonical.ComparatorEQ, &one, nil, UnitOutcome,
			&electionDy, canonical.Unknown, "ap+fox_news+nbc", canonical.PolarityPositive, dateOnly}},
		{"polymarket republican nominee", politics(pmPolID, "Will J.D. Vance win the 2028 Republican presidential nomination?", pmNomRepRules, pmPolClose), want{
			"jd_vance", "win_nomination:us_president:republican:2028", canonical.ComparatorEQ, &one, nil, UnitOutcome,
			&pmPolClose, canonical.Unknown, "republican_party_official", canonical.PolarityPositive, defaulted}},
		{"polymarket democratic nominee", politics(pmPolID, "Will Gavin Newsom win the 2028 Democratic presidential nomination?", pmNomDemRules, pmPolClose), want{
			"gavin_newsom", "win_nomination:us_president:democratic:2028", canonical.ComparatorEQ, &one, nil, UnitOutcome,
			&pmPolClose, canonical.Unknown, "democratic_party_official", canonical.PolarityPositive, defaulted}},
		{"polymarket democratic VP nominee", politics(pmPolID, "Will Gavin Newsom be the 2028 Democratic Vice-Presidential nominee?", pmVPDemRules, pmPolClose), want{
			"gavin_newsom", "win_nomination:us_vice_president:democratic:2028", canonical.ComparatorEQ, &one, nil, UnitOutcome,
			&pmPolClose, canonical.Unknown, "democratic_party_official", canonical.PolarityPositive, defaulted}},
		{"kalshi republican nominee", politics(kxPolID, "Will J.D. Vance be the nominee for the Presidency for the Republican party?", kxNomRVanceRules, kxNomClose), want{
			"jd_vance", "win_nomination:us_president:republican:2028", canonical.ComparatorEQ, &one, nil, UnitOutcome,
			&kxPolDefDl, canonical.Unknown, canonical.Unknown, canonical.PolarityPositive, defaulted}},
		{"kalshi democratic nominee", politics(kxPolID, "Will Gavin Newsom be the Democratic Presidential nominee in 2028?", kxNomDNewsomRules, kxNomClose), want{
			"gavin_newsom", "win_nomination:us_president:democratic:2028", canonical.ComparatorEQ, &one, nil, UnitOutcome,
			&kxPolDefDl, canonical.Unknown, canonical.Unknown, canonical.PolarityPositive, defaulted}},
		// Kalshi's presidential series resolves on the inauguration and never
		// says which country: the subject is known, the metric is not.
		{"kalshi presidential (no country)", politics(kxPolID, "Who will win the next presidential election? — Gavin Newsom", kxPresNewsomRules, kxPresClos), want{
			"gavin_newsom", canonical.Unknown, canonical.ComparatorUnknown, nil, nil, canonical.Unknown,
			&kxPresClos, canonical.Unknown, canonical.Unknown, canonical.PolarityUnknown, defaulted}},
		// Off-topic titles under the same tags are not about who wins a race.
		{"polymarket announce a run", politics(pmPolID, "Will J.D. Vance announce a presidential run before 2028?", pmAnnounceRules, pmAnnClose), want{
			canonical.Unknown, canonical.Unknown, canonical.ComparatorUnknown, nil, nil, canonical.Unknown,
			&pmAnnClose, canonical.Unknown, canonical.Unknown, canonical.PolarityUnknown, defaulted}},
		// A state primary is not modelled; its scheduled day is still the deadline.
		{"polymarket state primary", politics(pmPolID, "Will Gavin Newsom win the South Carolina Democratic Presidential primary?", pmSCPrimaryRules, pmSCClose), want{
			"gavin_newsom", canonical.Unknown, canonical.ComparatorUnknown, nil, nil, canonical.Unknown,
			&scPrimaryDy, canonical.Unknown, canonical.Unknown, canonical.PolarityUnknown, dateOnly}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checkTerms(t, Normalize(tc.in), tc.want)
		})
	}
}

// TestPoliticsUnparsedIsUnknown: anything the text does not state, states two
// ways, or states for an office or race this extractor does not model is unknown.
func TestPoliticsUnparsedIsUnknown(t *testing.T) {
	cases := []struct {
		name, title, rules string
		subject, metric    string
		flag               canonical.QualityFlag
	}{
		{"rules and title name different people (Kalshi typo)", "Will Hilary Clinton be the Democratic Presidential nominee in 2028? — Hillary Clinton",
			"If Hillary Clinton wins and accepts the nomination for the Presidency for the Democratic party in 2028, then the market resolves to Yes.",
			canonical.Unknown, "win_nomination:us_president:democratic:2028", QualityTermsAmbiguous},
		{"no year", "Will Gavin Newsom win the Democratic presidential nomination?", "", "gavin_newsom", canonical.Unknown, ""},
		{"two years in the binding text", "Will Gavin Newsom win the Democratic presidential nomination?",
			"If Gavin Newsom wins the Democratic presidential nomination in 2028 or 2032, the market resolves to Yes.",
			"gavin_newsom", canonical.Unknown, QualityTermsAmbiguous},
		{"nomination without a party", "Will Gavin Newsom be the 2028 U.S. presidential nominee?", "", "gavin_newsom", canonical.Unknown, ""},
		{"nomination naming both parties", "Will Gavin Newsom win the 2028 Democratic or Republican presidential nomination?", "", "gavin_newsom", canonical.Unknown, ""},
		{"another office", "Will Gavin Newsom win the 2028 U.S. Senate election in California?", "", "gavin_newsom", canonical.Unknown, ""},
		{"a foreign presidency", "Will Lula win the 2026 Brazilian presidential election?", "", "lula", canonical.Unknown, ""},
		{"no stage", "Gavin Newsom 2028?", "", canonical.Unknown, canonical.Unknown, ""},
		{"odds listing", "JD Vance Republican nomination odds leader on October 31?", "", canonical.Unknown, canonical.Unknown, ""},
		{"popular vote", "Will JD Vance win the popular vote in the 2028 US Presidential Election?", "", canonical.Unknown, canonical.Unknown, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := Normalize(politics(pmPolID, tc.title, tc.rules, pmPolClose))
			if err := out.Terms.Validate(); err != nil {
				t.Fatal(err)
			}
			if out.Terms.Subject != tc.subject || out.Terms.Metric != tc.metric {
				t.Errorf("subject %q metric %q, want %q %q", out.Terms.Subject, out.Terms.Metric, tc.subject, tc.metric)
			}
			if (out.Terms.Subject == canonical.Unknown || out.Terms.Metric == canonical.Unknown) &&
				(out.Terms.Comparator != canonical.ComparatorUnknown || out.Terms.Threshold != nil || out.Terms.Polarity != canonical.PolarityUnknown) {
				t.Errorf("proposition set without subject and metric: %+v", out.Terms)
			}
			if tc.flag != "" && !out.Quality.Has(tc.flag) {
				t.Errorf("missing flag %s in %v", tc.flag, out.Quality)
			}
		})
	}
}

// TestPoliticsStageAndPolarity: "wins", "is the nominee", "wins the runoff"
// and "is inaugurated" are different metrics; "not" and "lose" flip polarity.
func TestPoliticsStageAndPolarity(t *testing.T) {
	cases := []struct {
		title    string
		metric   string
		polarity canonical.Polarity
	}{
		{"Will JD Vance win the 2028 US Presidential Election?", "win_election:us_president:2028", canonical.PolarityPositive},
		{"Will JD Vance be elected U.S. President in 2028?", "win_election:us_president:2028", canonical.PolarityPositive},
		{"Will JD Vance not win the 2028 US Presidential Election?", "win_election:us_president:2028", canonical.PolarityNegative},
		{"Will JD Vance lose the 2028 US Presidential Election?", "win_election:us_president:2028", canonical.PolarityNegative},
		{"Will JD Vance be the 2028 Republican presidential nominee?", "win_nomination:us_president:republican:2028", canonical.PolarityPositive},
		{"Will JD Vance fail to win the 2028 Republican presidential nomination?", "win_nomination:us_president:republican:2028", canonical.PolarityNegative},
		{"Will JD Vance win the 2028 U.S. presidential runoff?", "win_runoff:us_president:2028", canonical.PolarityPositive},
		{"Will JD Vance be inaugurated as U.S. President in 2029?", "inaugurated:us_president:2029", canonical.PolarityPositive},
		{"Will the Republican Party win the 2028 US Presidential Election?", "win_election:us_president:2028", canonical.PolarityPositive},
	}
	for _, tc := range cases {
		out := Normalize(politics(pmPolID, tc.title, "", pmPolClose))
		if out.Terms.Metric != tc.metric || out.Terms.Polarity != tc.polarity {
			t.Errorf("%q: metric %q polarity %q, want %q %q", tc.title, out.Terms.Metric, out.Terms.Polarity, tc.metric, tc.polarity)
		}
	}
	// A title that negates what the rules state positively is ambiguous.
	conflict := Normalize(politics(pmPolID, "Will JD Vance not win the 2028 US Presidential Election?", pmWinRules, pmPolClose))
	if conflict.Terms.Polarity != canonical.PolarityUnknown || conflict.Terms.Threshold != nil || !conflict.Quality.Has(QualityTermsAmbiguous) {
		t.Errorf("title/rules polarity conflict: %+v %v", conflict.Terms, conflict.Quality)
	}
	if s := Extract(politics(pmPolID, "Will the Republican Party win the 2028 US Presidential Election?", "", pmPolClose)).Subject; s != "republican_party" {
		t.Errorf("party subject = %q", s)
	}
	// The rules' condition is binding: Kalshi's title says "win", the rules say "inaugurated".
	if st, _, _ := politicsStage(new(canonical.Quality), kxPresNewsomRules, "Who will win the next presidential election? — Gavin Newsom"); st != StageInaugurated {
		t.Errorf("stage = %q, want %q", st, StageInaugurated)
	}
}

// TestPoliticsStageSurvivesInitials: middle initials and "Jr." in recorded
// Kalshi rules must not cut the binding condition clause short (the stage
// word comes after the name). Regression for the MR !22 review.
func TestPoliticsStageSurvivesInitials(t *testing.T) {
	const presTitle = "Who will win the next presidential election? — "
	cases := []struct {
		title, rules, stage string
	}{
		// Recorded KXPRESPERSON / KXPRESNOMR / KXPRESNOMD rules (2026-10-07).
		{presTitle + "J.D. Vance", "If J.D. Vance is the next person inaugurated as President for the term beginning in 2029, then the market resolves to Yes.", StageInaugurated},
		{presTitle + "Donald J. Trump", "If Donald J. Trump is the next person inaugurated as President for the term beginning in 2029, then the market resolves to Yes.", StageInaugurated},
		{presTitle + "Donald J. Trump Jr.", "If Donald J. Trump Jr. is the next person inaugurated as President for the term beginning in 2029, then the market resolves to Yes.", StageInaugurated},
		{presTitle + "Stephen A. Smith", "If Stephen A. Smith is the next person inaugurated as President for the term beginning in 2029, then the market resolves to Yes.", StageInaugurated},
		{presTitle + "J.B. Pritzker", "If J.B. Pritzker is the next person inaugurated as President for the term beginning in 2029, then the market resolves to Yes.", StageInaugurated},
		// A title with no stage word, so only the rules can supply it.
		{"J.D. Vance 2028?", kxNomRVanceRules, StageWinNomination},
		{"Robert F. Kennedy Jr. 2028?", "If Robert F. Kennedy Jr. wins and accepts the nomination for the Presidency for the Republican party in 2028, then the market resolves to Yes.", StageWinNomination},
		{"Donald J. Trump Jr. 2028?", "If Donald J. Trump Jr. wins and accepts the nomination for the Presidency for the Republican party in 2028, then the market resolves to Yes.", StageWinNomination},
	}
	for _, tc := range cases {
		if st, _, ok := politicsStage(new(canonical.Quality), tc.rules, tc.title); st != tc.stage || !ok {
			t.Errorf("%q: stage %q ok=%v, want %q", tc.rules, st, ok, tc.stage)
		}
	}
	out := Normalize(politics(kxPolID, "Donald J. Trump Jr. 2028?",
		"If Donald J. Trump Jr. wins and accepts the nomination for the Presidency for the Republican party in 2028, then the market resolves to Yes.", kxNomClose))
	if out.Terms.Subject != "donald_j_trump_jr" || out.Terms.Metric != "win_nomination:us_president:republican:2028" {
		t.Errorf("subject %q metric %q", out.Terms.Subject, out.Terms.Metric)
	}
	got := sentencesOf("If Donald J. Trump Jr. wins the U.S. election. The resolution source is the AP.\n\nOtherwise “No”. Next")
	want := []string{"If Donald J. Trump Jr. wins the U.S. election.", "The resolution source is the AP.", "Otherwise “No”.", "Next"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("sentencesOf = %q, want %q", got, want)
	}
}

// TestPoliticsPairs runs the extracted terms through the matcher: wins vs
// nominee is related (never equivalent), an election-date mismatch is vetoed,
// and the same race on both venues with the same source is equivalent.
func TestPoliticsPairs(t *testing.T) {
	compare := func(a, b canonical.Instrument) canonical.Relationship {
		return match.Compare(Normalize(a), Normalize(b), nil)
	}
	hasVeto := func(r canonical.Relationship, code string) bool {
		for _, v := range r.Vetoes {
			if v.Code == code {
				return true
			}
		}
		return false
	}

	t.Run("wins vs nominee is related", func(t *testing.T) {
		win := politics(pmPolID, "Will JD Vance win the 2028 US Presidential Election?", pmWinRules, pmPolClose)
		nom := politics(kxPolID, "Will J.D. Vance be the nominee for the Presidency for the Republican party?", kxNomRVanceRules, kxNomClose)
		r := compare(win, nom)
		if r.Type != canonical.RelRelated || !hasVeto(r, match.VetoMetricMismatch) {
			t.Errorf("type %s vetoes %+v, want related with metric_mismatch", r.Type, r.Vetoes)
		}
	})

	t.Run("polarity flip is vetoed", func(t *testing.T) {
		yes := politics(pmPolID, "Will JD Vance win the 2028 US Presidential Election?", pmWinRules, pmPolClose)
		notRules := strings.Replace(pmWinRules, "resolve to the person who wins", `resolve to "Yes" if JD Vance does not win`, 1)
		no := politics(pmPolID2, "Will JD Vance not win the 2028 US Presidential Election?", notRules, pmPolClose)
		r := compare(yes, no)
		if r.Type == canonical.RelEquivalent || !hasVeto(r, match.VetoPolarityMismatch) {
			t.Errorf("type %s vetoes %+v, want polarity_mismatch", r.Type, r.Vetoes)
		}
	})

	t.Run("election-date mismatch is vetoed", func(t *testing.T) {
		a := politics(pmPolID, "Will JD Vance win the 2028 US Presidential Election?", pmWinRules, pmPolClose)
		moved := strings.Replace(pmWinRules, "November 7, 2028", "November 14, 2028", 1)
		b := politics(pmPolID2, "Will JD Vance win the 2028 US Presidential Election?", moved, pmPolClose.Add(7*24*time.Hour))
		r := compare(a, b)
		if r.Type == canonical.RelEquivalent || !hasVeto(r, match.VetoDeadlineGap) {
			t.Errorf("type %s vetoes %+v, want deadline_gap", r.Type, r.Vetoes)
		}
	})

	t.Run("nominee on both venues without a Kalshi source is uncertain", func(t *testing.T) {
		pm := politics(pmPolID, "Will J.D. Vance win the 2028 Republican presidential nomination?", pmNomRepRules, pmPolClose)
		kx := politics(kxPolID, "Will J.D. Vance be the nominee for the Presidency for the Republican party?", kxNomRVanceRules, kxNomClose)
		r := compare(pm, kx)
		if r.Type != canonical.RelRelated || len(r.Vetoes) != 1 || !hasVeto(r, match.VetoSourceUnknown) {
			t.Errorf("type %s vetoes %+v, want related on source_unknown only", r.Type, r.Vetoes)
		}
	})

	t.Run("same race and source is equivalent", func(t *testing.T) {
		pm := politics(pmPolID, "Will J.D. Vance win the 2028 Republican presidential nomination?", pmNomRepRules, pmPolClose)
		kx := politics(kxPolID, "Will J.D. Vance be the nominee for the Presidency for the Republican party?",
			kxNomRVanceRules+"\n\nThe resolution source for this market is a consensus of official Republican Party sources.", kxNomClose)
		r := compare(pm, kx)
		if r.Type != canonical.RelEquivalent {
			t.Errorf("type %s vetoes %+v explanation %q, want equivalent", r.Type, r.Vetoes, r.Explanation)
		}
	})
}

func TestPoliticsDeadline(t *testing.T) {
	two := "The election is scheduled to take place on November 7, 2028. The runoff will be held on December 5, 2028."
	out := Normalize(politics(pmPolID, "Will JD Vance win the 2028 US Presidential Election?", two, pmPolClose))
	if !out.Quality.Has(QualityTermsAmbiguous) || out.Terms.Deadline == nil || !out.Terms.Deadline.Equal(pmPolClose) {
		t.Errorf("two scheduled dates: deadline %v quality %v, want the close time and terms_ambiguous", out.Terms.Deadline, out.Quality)
	}
	late := "The 2032 US Presidential Election is scheduled to take place on November 2, 2032."
	out = Normalize(politics(pmPolID, "Will JD Vance win the 2032 US Presidential Election?", late, pmPolClose))
	if !out.Quality.Has(canonical.QualityDeadlineDefaulted) || !out.Terms.Deadline.Equal(pmPolClose) {
		t.Errorf("date after the close: deadline %v quality %v, want the close time", out.Terms.Deadline, out.Quality)
	}
	out = Normalize(politics(pmPolID, "Will JD Vance win the 2028 US Presidential Election?", pmWinRules, time.Time{}))
	if out.Terms.Deadline == nil || !out.Terms.Deadline.Equal(electionDy) {
		t.Errorf("no close time: deadline %v, want the stated election day", out.Terms.Deadline)
	}
	out = Normalize(politics(pmPolID, "Will JD Vance win the 2028 US Presidential Election?", "", time.Time{}))
	if out.Terms.Deadline != nil || !out.Quality.Has(canonical.QualityMissingField) {
		t.Errorf("nothing stated: deadline %v quality %v, want nil and missing_field", out.Terms.Deadline, out.Quality)
	}
}

func TestPoliticsSubjectSlug(t *testing.T) {
	for in, want := range map[string]string{
		"J.D. Vance": "jd_vance", "JD Vance": "jd_vance", "Alexandria Ocasio-Cortez": "alexandria_ocasio_cortez",
		"Beto O'Rourke": "beto_orourke", "Donald Trump Jr.": "donald_trump_jr", "the Democratic Party": "democratic_party",
		"Democrats": "democratic_party", "Republicans": "republican_party", "GOP": "republican_party", "...": canonical.Unknown,
	} {
		if got := subjectSlug(in); got != want {
			t.Errorf("subjectSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestPoliticsRecordedFixtures normalises every recorded politics market (no
// network: the committed fixture files are read directly) and pins how many
// of each metric the extractor finds.
func TestPoliticsRecordedFixtures(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "fixtures")
	var ins []canonical.Instrument
	kalshiFiles, _ := filepath.Glob(filepath.Join(root, "kalshi", "events_series_ticker-KXPRES*.json"))
	for _, f := range kalshiFiles {
		var page struct {
			Events []struct {
				Markets []struct {
					Ticker         string `json:"ticker"`
					Title          string `json:"title"`
					YesSubTitle    string `json:"yes_sub_title"`
					RulesPrimary   string `json:"rules_primary"`
					RulesSecondary string `json:"rules_secondary"`
					CloseTime      string `json:"close_time"`
				} `json:"markets"`
			} `json:"events"`
		}
		readJSON(t, f, &page)
		for _, e := range page.Events {
			for _, m := range e.Markets {
				title := m.Title
				if m.YesSubTitle != "" && !strings.Contains(title, m.YesSubTitle) {
					title += " — " + m.YesSubTitle
				}
				ct, _ := time.Parse(time.RFC3339, m.CloseTime)
				ins = append(ins, politics("kalshi:"+m.Ticker, title, strings.TrimSpace(m.RulesPrimary+"\n\n"+m.RulesSecondary), ct.UTC()))
			}
		}
	}
	pmFiles, _ := filepath.Glob(filepath.Join(root, "polymarket", "gamma_events_active-true_closed-false_*tag_slug-pr*.json"))
	for _, f := range pmFiles {
		var events []struct {
			Markets []struct {
				Question, Description, EndDate string
				Active, Closed                 bool
			} `json:"markets"`
		}
		readJSON(t, f, &events)
		for _, e := range events {
			for _, m := range e.Markets {
				if !m.Active || m.Closed {
					continue
				}
				ct, _ := time.Parse(time.RFC3339, m.EndDate)
				ins = append(ins, politics(pmPolID, m.Question, m.Description, ct.UTC()))
			}
		}
	}
	if len(kalshiFiles) != 3 || len(pmFiles) != 2 || len(ins) < 500 {
		t.Fatalf("read %d kalshi and %d polymarket files, %d markets", len(kalshiFiles), len(pmFiles), len(ins))
	}
	metrics := map[string]int{}
	for _, in := range ins {
		out := Normalize(in)
		if err := out.Terms.Validate(); err != nil {
			t.Errorf("%s %q: %v", in.ID, in.Title, err)
		}
		metrics[out.Terms.Metric]++
	}
	// Counts from the 2026-10-07 recording; the VP events are listed under both tags.
	for metric, want := range map[string]int{
		"win_election:us_president:2028":                   53 + 1,
		"win_nomination:us_president:democratic:2028":      48 + 53,
		"win_nomination:us_president:republican:2028":      39 + 42,
		"win_nomination:us_vice_president:democratic:2028": 2 * 43,
		"win_nomination:us_vice_president:republican:2028": 35,
	} {
		if metrics[metric] != want {
			t.Errorf("%s: %d markets, want %d (all: %v)", metric, metrics[metric], want, metrics)
		}
	}
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func TestExtractPoliticsHostileTextDoesNotPanic(t *testing.T) {
	huge := strings.Repeat("If J.D. Vance wins the 2028 U.S. presidential nomination of the Republican Party scheduled to take place on November 7, 2028. "+
		"Will Gavin Newsom not lose the runoff? The resolution source is the AP and Fox. ", 2000)
	out := Normalize(politics(pmPolID, huge, huge+"\x00\xff\xfe\u202e", pmPolClose))
	if err := out.Terms.Validate(); err != nil {
		t.Fatal(err)
	}
	if out.Terms.Deadline == nil {
		t.Error("no deadline")
	}
	names := strings.Repeat("Aa ", 5000) + "wins"
	if err := Normalize(politics(pmPolID, "Will "+names, "If "+names, pmPolClose)).Terms.Validate(); err != nil {
		t.Fatal(err)
	}
}

// TestPoliticsExtractNeverPanicsProperty: any text, including hostile bytes, gives valid Terms.
func TestPoliticsExtractNeverPanicsProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		piece := rapid.SampledFrom([]string{"Will ", "If ", "J.D. Vance ", "wins ", "not ", "the 2028 ", "U.S. ", "presidential ",
			"nomination ", "nominee ", "runoff ", "Democratic ", "Republican ", "scheduled to take place on November 7, 2028. ",
			"resolution source is the AP. ", "— Gavin Newsom", "\x00", "\xff", "?", "."})
		title := strings.Join(rapid.SliceOfN(piece, 0, 12).Draw(t, "title"), "") + rapid.String().Draw(t, "titleTail")
		rules := strings.Join(rapid.SliceOfN(piece, 0, 30).Draw(t, "rules"), "") + rapid.String().Draw(t, "rulesTail")
		out := Normalize(politics(pmPolID, title, rules, pmPolClose))
		if err := out.Terms.Validate(); err != nil {
			t.Fatalf("invalid terms for %q / %q: %v", title, rules, err)
		}
		if (out.Terms.Threshold != nil) != (out.Terms.Subject != canonical.Unknown && out.Terms.Metric != canonical.Unknown) {
			t.Fatalf("threshold set without a subject and metric (or the reverse): %+v", out.Terms)
		}
	})
}

func FuzzExtractPolitics(f *testing.F) {
	f.Add("Will JD Vance win the 2028 US Presidential Election?", pmWinRules)
	f.Add("Will J.D. Vance be the nominee for the Presidency for the Republican party?", kxNomRVanceRules)
	f.Add("Who will win the next presidential election? — Gavin Newsom", kxPresNewsomRules)
	f.Add("", "")
	f.Fuzz(func(t *testing.T, title, rules string) {
		out := Normalize(politics(pmPolID, title, rules, pmPolClose))
		if err := out.Terms.Validate(); err != nil {
			t.Fatal(err)
		}
	})
}

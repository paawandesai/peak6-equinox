package normalize

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// Politics markets name one person or party and one outcome of one race:
// Kalshi "If Hunter Biden wins and accepts the nomination for the Presidency
// for the Democratic party in 2028, then the market resolves to Yes" and
// Polymarket "Will Gavin Newsom win the 2028 Democratic presidential
// nomination?" with rules about "the named individual".
//
// Field sources (D-060 precedence: rules first, title second):
//   - subject: the person or party the YES outcome names, as a slug
//     ("gavin_newsom", "jd_vance" for both "JD" and "J.D.", "democratic_party").
//     Read from the rules' condition ("If <Name> is/wins ..."), else the title
//     ("Will <Name> win/be ...", Kalshi's "... — <Name>" suffix). Rules and
//     title naming different people is ambiguous. Titles about something other
//     than winning a race (announcing a run, endorsements, odds listings,
//     charges, counts, the popular vote) get an unknown subject.
//   - metric: "<stage>:<office>[:<party>]:<year>". The stage is read from the
//     first condition clause of the rules, else the title: win_nomination
//     ("nominee", "nomination", "nominated"), win_runoff ("runoff"),
//     inaugurated ("inaugurated"), win_election ("win", "elected", "lose").
//     So "wins the presidency" and "is the nominee" never share a metric, and
//     the matcher sees a known metric mismatch (related, not equivalent).
//     A primary or caucus (state-specific, not modelled) is unknown. The
//     office is us_vice_president or us_president; any other office, or a
//     presidency the text never ties to the United States, is unknown. A
//     nomination needs exactly one party. The year is the election cycle the
//     text states outside calendar dates ("the 2028 nomination", "the term
//     beginning in 2029"); none, or several in the binding text, is unknown.
//   - comparator, threshold, unit: a race outcome is an indicator, so a known
//     metric is "eq 1 outcome" (the named outcome happens). Unknown otherwise.
//   - polarity: positive, negative for "not win", "fail to win" or "lose".
//   - deadline: the election day the text schedules ("scheduled to take place
//     on November 7, 2028"), as 00:00 UTC on that date with an unknown zone
//     and the deadline_date_only flag, since the text states no time. Several
//     scheduled dates is ambiguous. A date more than 7 days after the close
//     time is a misread. Otherwise the close time, flagged deadline_defaulted.
//   - resolution source: the authorities named in the sentences that say
//     "resolution source", joined in sorted order ("ap+fox_news+nbc",
//     "democratic_party_official"). No such sentence is unknown.

// Metric parts this extractor produces.
const (
	StageWinElection   = "win_election"   // wins the general election
	StageWinNomination = "win_nomination" // wins (and accepts) a party's nomination
	StageWinRunoff     = "win_runoff"     // wins a runoff
	StageInaugurated   = "inaugurated"    // is inaugurated into the office
	OfficeUSPresident  = "us_president"
	OfficeUSVicePres   = "us_vice_president"
	PartyDemocratic    = "democratic"
	PartyRepublican    = "republican"
	// UnitOutcome: the threshold is an indicator, 1 when the named outcome happens.
	UnitOutcome = "outcome"
)

// QualityDeadlineDateOnly flags a deadline from an election day stated with
// no time or zone; the deadline is 00:00 UTC on that day.
const QualityDeadlineDateOnly canonical.QualityFlag = "deadline_date_only"

func init() { Register(canonical.CategoryPolitics, extractPolitics) }

// outcomeOne is the threshold of an indicator proposition.
var outcomeOne = decimal.NewFromInt(1)

const (
	// namePattern: one to six capitalised words ("JD Vance", "J.D. Vance",
	// "Alexandria Ocasio-Cortez", "Donald Trump Jr.", "Stephen A. Smith").
	namePattern = `(\p{Lu}[\p{L}\p{M}'’.\-]{0,30}(?:\s+(?:\p{Lu}[\p{L}\p{M}'’.\-]{0,30}|de|da|del|van|von|der|la|le)){0,5})`
	// stageVerbs: the verbs that follow a subject in a condition.
	stageVerbs = `(?i:is|was|wins?|won|has|becomes?|will|be|receives?|gets?|loses?|not|fails?|advances?|does|did|doesn['’]t)\b`
)

var (
	// rulesSubjectRe: "If Gavin Newsom is the next person inaugurated ...".
	rulesSubjectRe = regexp.MustCompile(`\b(?i:if)\s+(?:the\s+)?` + namePattern + `\s+` + stageVerbs)
	// titleSubjectRe: "Will JD Vance win the 2028 US Presidential Election?".
	titleSubjectRe = regexp.MustCompile(`^\s*(?i:will)\s+(?:the\s+)?` + namePattern + `\s+` + stageVerbs)
	// titleSuffixRe: Kalshi's "<question> — <yes_sub_title>".
	titleSuffixRe = regexp.MustCompile(`\s—\s+` + namePattern + `\s*$`)
	// partyNameRe: a subject that is a party, not a person.
	partyNameRe = regexp.MustCompile(`(?i)^(?:the\s+)?(?:(democratic|republican)(?:\s+party)?|(democrats|republicans)|(gop))$`)

	// politicsOffTopicRe: title words that mean the market is not about who wins a race.
	politicsOffTopicRe = regexp.MustCompile(`(?i)\b(?:announce\w*|endorse\w*|odds|leader|first|flips?|charge[sd]?|indict\w*|out|` +
		`resign\w*|how\s+many|incumbents?|debates?|polls?|polling|approval|margin|popular\s+vote|electoral\s+votes?|turnout|` +
		`within|cabinet|speaker|impeach\w*|pardon\w*|says?|mentions?|tweets?|run\s+for|running|visit\w*|meet\w*)\b`)

	// conditionRe: a clause that states the condition ("If X wins ...",
	// "This market will resolve to the person who wins ..."). It is matched
	// inside one sentence from sentencesOf, so the periods of "J.D. Vance",
	// "Donald J. Trump" and "U.S." do not cut it short.
	conditionRe = regexp.MustCompile(`(?i)(?:\bif\b|\bresolves?\s+(?:to|according\s+to)\b).{0,240}`)
	// Stage words, strongest first: a nomination clause may also say "wins".
	nominationRe  = regexp.MustCompile(`(?i)\bnomin(?:ee|ees|ation|ated)\b`)
	runoffRe      = regexp.MustCompile(`(?i)\brun-?offs?\b`)
	primaryRe     = regexp.MustCompile(`(?i)\b(?:primar(?:y|ies)|caucus(?:es)?)\b`)
	inauguratedRe = regexp.MustCompile(`(?i)\binaugurat(?:ed|ion)\b`)
	winRe         = regexp.MustCompile(`(?i)\b(?:wins?|won|winner|elected|loses?|lost)\b`)
	loseRe        = regexp.MustCompile(`(?i)\b(?:loses?|lost)\b`)
	// stageNegatedRe: a negation before the stage word in the same clause.
	stageNegatedRe = regexp.MustCompile(`(?i)\b(?:not|never|fails?\s+to|failed\s+to|won['’]t|doesn['’]t|didn['’]t)\b`)

	// Offices.
	vicePresidentRe = regexp.MustCompile(`(?i)\bvice[\s-]*presiden(?:t|tial|cy)\b|\bVP\b`)
	presidentRe     = regexp.MustCompile(`(?i)\bpresiden(?:t|tial|cy)\b`)
	// otherOfficeRe: a title about another office or legislature.
	otherOfficeRe = regexp.MustCompile(`(?i)\b(?:senate|house|governor|gubernatorial|mayor\w*|congress\w*|district|parliament\w*|` +
		`prime\s+minister|chancellor|legislat\w*|assembly|council|attorney\s+general|secretary\s+of\s+state)\b`)
	// usCueRe: the text ties the presidency to the United States.
	usCueRe = regexp.MustCompile(`\bU\.S\.(?:A\.)?|\bUSA?\b|(?i:\bunited\s+states\b|\bamerican?\b|\bdemocrat(?:ic|s)?\b|\brepublicans?\b|\bgop\b|\bwhite\s+house\b)`)
	// Parties named in a nomination.
	democraticRe = regexp.MustCompile(`(?i)\bdemocrat(?:ic|s)?\b`)
	republicanRe = regexp.MustCompile(`(?i)\b(?:republicans?|gop)\b`)
	// cycleYearRe: a year in the plausible election range.
	cycleYearRe = regexp.MustCompile(`\b(20[2-9]\d)\b`)

	// sentenceEndRe: a candidate sentence end. The character before the
	// period must be lower case, a digit or a closing bracket or quote, so
	// initials ("U.S.", "J.D.", "Donald J.") never end a sentence;
	// sentencesOf also skips an end followed by a lower-case word ("Jr. wins").
	sentenceEndRe = regexp.MustCompile(`[\p{Ll}\d)\]”"'’][.!?]\s+|\n+`)
	// electionNounRe and scheduleCueRe: a sentence that schedules a vote.
	electionNounRe = regexp.MustCompile(`(?i)\b(?:elections?|primar(?:y|ies)|caucus(?:es)?|run-?offs?|vote)\b`)
	scheduleCueRe  = regexp.MustCompile(`(?i)\b(?:take\s+place|be\s+held|held|hold|scheduled)\b`)
	// resolutionSourceRe: a sentence that names the resolution source.
	resolutionSourceRe = regexp.MustCompile(`(?i)\bresolution\s+source\b`)
)

// politicsSourceNames maps authorities named in a resolution-source sentence
// to canonical IDs.
var politicsSourceNames = []sourceName{
	{regexp.MustCompile(`(?i)associated\s+press|\bAP\b`), "ap"},
	{regexp.MustCompile(`(?i)\bfox(?:\s+news)?\b`), "fox_news"},
	{regexp.MustCompile(`(?i)\bnbc\b`), "nbc"},
	{regexp.MustCompile(`(?i)\bcnn\b`), "cnn"},
	{regexp.MustCompile(`(?i)\babc(?:\s+news)?\b`), "abc_news"},
	{regexp.MustCompile(`(?i)\bcbs\b`), "cbs"},
	{regexp.MustCompile(`(?i)\breuters\b`), "reuters"},
	{regexp.MustCompile(`(?i)decision\s+desk`), "decision_desk_hq"},
	{regexp.MustCompile(`(?i)official\s+democratic(?:\s+party)?\s+sources|democrats\.org|\bdnc\b`), "democratic_party_official"},
	{regexp.MustCompile(`(?i)official\s+(?:republican(?:\s+party)?|gop)\s+sources|gop\.com|rnc\.org|\brnc\b`), "republican_party_official"},
	{regexp.MustCompile(`(?i)credible\s+(?:news\s+)?(?:sources?|reporting)`), "credible_reporting"},
	{regexp.MustCompile(`(?i)official\s+statements?`), "official_statements"},
	{regexp.MustCompile(`(?i)predictionmarketodds`), "predictionmarketodds"},
}

func extractPolitics(in canonical.Instrument) (canonical.Terms, canonical.Quality) {
	var q canonical.Quality
	t := canonical.UnknownTerms()
	srcs := sourcesOf(in)
	rules, title := srcs[0].text, srcs[1].text

	if !politicsOffTopicRe.MatchString(title) {
		t.Subject = politicsSubject(&q, rules, title)
		stage, negative, polarityOK := politicsStage(&q, rules, title)
		t.Metric = politicsMetric(&q, stage, srcs)
		if t.Subject != canonical.Unknown && t.Metric != canonical.Unknown && polarityOK {
			one := outcomeOne
			t.Comparator, t.Threshold, t.Unit = canonical.ComparatorEQ, &one, UnitOutcome
			t.Polarity = canonical.PolarityPositive
			if negative {
				t.Polarity = canonical.PolarityNegative
			}
		}
	}
	politicsDeadline(&t, &q, in.CloseTime, srcs)
	t.ResolutionSource = politicsSource(rules)
	return t, q
}

// politicsSubject returns the person or party the condition names, rules
// first. Different names in the rules and the title are ambiguous.
func politicsSubject(q *canonical.Quality, rules, title string) string {
	fromRules := subjectIn(rulesSubjectRe, rules)
	fromTitle := subjectIn(titleSubjectRe, title)
	if fromTitle == canonical.Unknown {
		fromTitle = subjectIn(titleSuffixRe, title)
	}
	switch {
	case fromRules != canonical.Unknown && fromTitle != canonical.Unknown && fromRules != fromTitle:
		q.Add(QualityTermsAmbiguous, "subject", fmt.Sprintf("the rules name %s and the title %s", fromRules, fromTitle))
		return canonical.Unknown
	case fromRules != canonical.Unknown:
		return fromRules
	}
	return fromTitle
}

// subjectIn returns the slug of the first name re captures in text, else unknown.
func subjectIn(re *regexp.Regexp, text string) string {
	m := re.FindStringSubmatch(text)
	if m == nil {
		return canonical.Unknown
	}
	return subjectSlug(m[1])
}

// notANameWords are capitalised question and sentence openers that the name
// pattern can capture on their own ("If Will not win ..."). A real first
// name such as "Will Hurd" keeps its surname and is unaffected.
var notANameWords = map[string]bool{
	"will": true, "if": true, "the": true, "this": true, "that": true, "who": true, "which": true,
	"what": true, "yes": true, "no": true, "it": true, "they": true, "he": true, "she": true,
}

// subjectSlug turns a name into a subject: parties map to
// "<party>_party"; people are lower-cased word by word with dots and
// apostrophes dropped and words joined with underscores ("J.D. Vance" is
// "jd_vance", "Ocasio-Cortez" is "ocasio_cortez"). A single-letter middle
// initial is dropped, because venues write the same person with and without
// it ("Donald J. Trump" and "Donald Trump" are both "donald_trump"); a
// leading initial ("J. Smith") is kept so the slug never loses its first word.
func subjectSlug(name string) string {
	name = strings.TrimSpace(name)
	if m := partyNameRe.FindStringSubmatch(name); m != nil {
		switch {
		case strings.HasPrefix(strings.ToLower(m[1]+m[2]), "democrat"):
			return PartyDemocratic + "_party"
		default:
			return PartyRepublican + "_party"
		}
	}
	var words []string
	for _, field := range strings.Fields(strings.ToLower(name)) {
		word := strings.NewReplacer(".", "", "'", "", "’", "").Replace(field)
		if word == "" {
			continue
		}
		if len(words) > 0 && utf8.RuneCountInString(word) == 1 {
			continue // middle initial
		}
		for _, part := range strings.Split(word, "-") {
			if part != "" {
				words = append(words, part)
			}
		}
	}
	slug := strings.Join(words, "_")
	if slug == "" || notANameWords[slug] {
		return canonical.Unknown
	}
	return slug
}

// politicsStage reads the stage from the rules' first condition clause that
// names one, else from the title, and reports whether the clause negates it.
// It returns "" when no stage is stated. A title that negates what the rules
// state positively (or the reverse) is ambiguous: ok is false.
func politicsStage(q *canonical.Quality, rules, title string) (stage string, negative, ok bool) {
	titleStage, titleNeg := stageIn(title)
	for _, sentence := range sentencesOf(rules) {
		if clause := conditionRe.FindString(sentence); clause != "" {
			if stage, negative = stageIn(clause); stage != "" {
				break
			}
		}
	}
	if stage == "" {
		return titleStage, titleNeg, true
	}
	if titleStage != "" && titleNeg != negative {
		q.Add(QualityTermsAmbiguous, "polarity", "the rules and the title disagree on whether the outcome is negated")
		return stage, negative, false
	}
	return stage, negative, true
}

// stageIn classifies one clause, strongest stage first. A primary or caucus is
// returned as "primary" so the caller can record it as unknown.
func stageIn(clause string) (string, bool) {
	type cue struct {
		re    *regexp.Regexp
		stage string
	}
	for _, c := range []cue{{nominationRe, StageWinNomination}, {runoffRe, StageWinRunoff}, {primaryRe, "primary"},
		{inauguratedRe, StageInaugurated}, {winRe, StageWinElection}} {
		loc := c.re.FindStringIndex(clause)
		if loc == nil {
			continue
		}
		neg := stageNegatedRe.MatchString(clause[:loc[0]])
		// "loses the election", "lose the nomination" and "lost the runoff" are
		// "does not win" it: a lose word up to and including the stage word flips.
		if loseRe.MatchString(clause[:loc[1]]) {
			neg = !neg
		}
		return c.stage, neg
	}
	return "", false
}

// politicsMetric builds "<stage>:<office>[:<party>]:<year>", or unknown when
// any part is missing or not modelled.
func politicsMetric(q *canonical.Quality, stage string, srcs []source) string {
	rules, title := srcs[0].text, srcs[1].text
	all := rules + "\n" + title
	if stage == "" || stage == "primary" || otherOfficeRe.MatchString(title) {
		return canonical.Unknown
	}
	office := canonical.Unknown
	switch {
	case vicePresidentRe.MatchString(all):
		office = OfficeUSVicePres
	case presidentRe.MatchString(all):
		office = OfficeUSPresident
	}
	if office == canonical.Unknown || !usCueRe.MatchString(all) {
		return canonical.Unknown
	}
	parts := []string{stage, office}
	if stage == StageWinNomination {
		dem, rep := democraticRe.MatchString(all), republicanRe.MatchString(all)
		switch {
		case dem && !rep:
			parts = append(parts, PartyDemocratic)
		case rep && !dem:
			parts = append(parts, PartyRepublican)
		default:
			return canonical.Unknown
		}
	}
	year, ok, ambiguousIn := firstStated(srcs, cycleYearsIn, strconv.Itoa)
	if ambiguousIn != "" {
		q.Add(QualityTermsAmbiguous, "metric", "the "+ambiguousIn+" text states more than one election year")
	}
	if !ok {
		return canonical.Unknown
	}
	return strings.Join(append(parts, strconv.Itoa(year)), ":")
}

// cycleYearsIn returns the years text states outside full calendar dates, so
// "the 2028 nomination" counts and "(January 20, 2029)" does not.
func cycleYearsIn(text string) []int {
	text = monthDayRe.ReplaceAllStringFunc(text, func(date string) string {
		if m := monthDayRe.FindStringSubmatch(date); m != nil && m[3] != "" {
			return " "
		}
		return date
	})
	text = isoDateRe.ReplaceAllString(text, " ")
	var out []int
	for _, m := range cycleYearRe.FindAllStringSubmatch(text, -1) {
		y, _ := strconv.Atoi(m[1])
		out = append(out, y)
	}
	return out
}

// sentencesOf splits text into sentences without breaking initials ("U.S.",
// "J.D. Vance", "Donald J. Trump") or "Jr." before a lower-case word.
func sentencesOf(text string) []string {
	var out []string
	start := 0
	for _, loc := range sentenceEndRe.FindAllStringIndex(text, -1) {
		end := loc[0] // a newline run ends the sentence where it starts
		if text[loc[0]] != '\n' {
			if next, _ := utf8.DecodeRuneInString(text[loc[1]:]); unicode.IsLower(next) {
				continue // "Donald Trump Jr. wins": not a sentence end
			}
			_, size := utf8.DecodeRuneInString(text[loc[0]:])
			end += size + 1 // keep the character before the period and the period
		}
		if end > start {
			out = append(out, text[start:end])
		}
		start = loc[1]
	}
	if start < len(text) {
		out = append(out, text[start:])
	}
	return out
}

// scheduledDatesIn returns the full dates of every sentence that schedules a
// vote ("The 2028 US Presidential Election is scheduled to take place on
// November 7, 2028").
func scheduledDatesIn(text string) []calendarDate {
	var out []calendarDate
	for _, s := range sentencesOf(text) {
		if !electionNounRe.MatchString(s) || !scheduleCueRe.MatchString(s) {
			continue
		}
		for _, d := range datesIn(s) {
			if d.year != 0 {
				out = append(out, d)
			}
		}
	}
	return out
}

// politicsDeadline sets the deadline to the scheduled election day, else to
// the close time (flagged).
func politicsDeadline(t *canonical.Terms, q *canonical.Quality, closeTime time.Time, srcs []source) {
	date, ok, ambiguousIn := firstStated(srcs, scheduledDatesIn, calendarDate.key)
	var detail string
	switch {
	case ambiguousIn != "":
		detail = "the " + ambiguousIn + " text schedules several election dates"
		q.Add(QualityTermsAmbiguous, "deadline", detail)
	case !ok:
		detail = "no scheduled election date stated"
	default:
		day := time.Date(date.year, date.month, date.day, 0, 0, 0, 0, time.UTC)
		if closeTime.IsZero() || !day.After(closeTime.Add(deadlineSanityWindow)) {
			t.Deadline = &day
			q.Add(QualityDeadlineDateOnly, "deadline", "election day "+day.Format(time.DateOnly)+" stated without a time; using 00:00 UTC")
			return
		}
		detail = fmt.Sprintf("scheduled date %s is more than 7 days after the close time", day.Format(time.DateOnly))
	}
	if closeTime.IsZero() {
		q.Add(canonical.QualityMissingField, "deadline", detail+"; no close time to default to")
		return
	}
	d := closeTime.UTC()
	t.Deadline = &d
	q.Add(canonical.QualityDeadlineDefaulted, "deadline", detail+"; using close time")
}

// politicsSource returns the authorities named in the rules' resolution-source
// sentences, sorted and joined with "+", else unknown.
func politicsSource(rules string) string {
	ids := map[string]bool{}
	for _, s := range sentencesOf(rules) {
		if !resolutionSourceRe.MatchString(s) {
			continue
		}
		for _, n := range politicsSourceNames {
			if n.re.MatchString(s) {
				ids[n.id] = true
			}
		}
	}
	if len(ids) == 0 {
		return canonical.Unknown
	}
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Strings(out)
	return strings.Join(out, "+")
}

package kalshi

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// Kalshi's wire formats (verified against the live API on 2026-10-06):
//   - prices are dollar strings with up to 4 decimals, e.g. "0.9900";
//   - sizes are fixed-point ("_fp") strings with 2 decimals, e.g. "7246.15";
//   - the order book is {"orderbook_fp":{"yes_dollars":[[price,size],...],
//     "no_dollars":[...]}}, bids only, each side sorted worst (lowest) first.
//
// The parsers accept only plain non-negative decimals: no sign, exponent,
// NaN or Inf, and a bounded number of digits, so hostile input can never
// build a huge decimal.
var (
	dollarPattern = regexp.MustCompile(`^[0-9]{1,6}(\.[0-9]{1,8})?$`)
	fpPattern     = regexp.MustCompile(`^[0-9]{1,15}(\.[0-9]{1,8})?$`)
	one           = decimal.NewFromInt(1)
)

// parseDollars parses a Kalshi dollar-price string into a probability in [0,1].
func parseDollars(s string) (decimal.Decimal, error) {
	if !dollarPattern.MatchString(s) {
		return decimal.Decimal{}, fmt.Errorf("price %q is not a plain decimal", truncate(s, 32))
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Decimal{}, fmt.Errorf("price %q: %w", truncate(s, 32), err)
	}
	if err := canonical.ValidatePrice(d); err != nil {
		return decimal.Decimal{}, err
	}
	return d, nil
}

// parseFP parses a Kalshi fixed-point ("_fp") size string into a non-negative decimal.
func parseFP(s string) (decimal.Decimal, error) {
	if !fpPattern.MatchString(s) {
		return decimal.Decimal{}, fmt.Errorf("size %q is not a plain non-negative decimal", truncate(s, 32))
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Decimal{}, fmt.Errorf("size %q: %w", truncate(s, 32), err)
	}
	return d, nil
}

// market is the subset of a Kalshi market object the adapter reads.
type market struct {
	Ticker              string `json:"ticker"`
	EventTicker         string `json:"event_ticker"`
	MVECollectionTicker string `json:"mve_collection_ticker"`
	Title               string `json:"title"`
	YesSubTitle         string `json:"yes_sub_title"`
	RulesPrimary        string `json:"rules_primary"`
	RulesSecondary      string `json:"rules_secondary"`
	Status              string `json:"status"`
	CloseTime           string `json:"close_time"`
	ExpirationTime      string `json:"expiration_time"`
	Result              string `json:"result"`
	SettlementTS        string `json:"settlement_ts"`
}

// isMVE reports whether m is a multivariate (parlay) market, which is always excluded.
func (m market) isMVE() bool { return strings.TrimSpace(m.MVECollectionTicker) != "" }

// skip is one record dropped while parsing, with the reason.
type skip struct {
	ticker string // may be empty
	reason string
}

// marketsPage is one decoded /markets page.
type marketsPage struct {
	markets []market
	cursor  string
	skipped []skip
}

// parseMarkets decodes a /markets response. A record that is not a JSON object
// of the expected shape is skipped; only an invalid top-level document is an error.
func parseMarkets(body []byte) (marketsPage, error) {
	var doc struct {
		Markets []json.RawMessage `json:"markets"`
		Cursor  string            `json:"cursor"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return marketsPage{}, fmt.Errorf("decoding markets: %w", err)
	}
	p := marketsPage{cursor: doc.Cursor}
	p.markets, p.skipped = decodeMarkets(doc.Markets)
	return p, nil
}

// settlementSource is one decoded settlement source from the event level.
type settlementSource struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// event is one decoded Kalshi event with its nested markets.
type event struct {
	ticker            string
	settlementSources []settlementSource
	markets           []market
	skipped           []skip
}

// eventsPage is one decoded /events page.
type eventsPage struct {
	events  []event
	cursor  string
	skipped []skip // events that could not be decoded at all
}

// parseEvents decodes an /events?with_nested_markets=true response.
func parseEvents(body []byte) (eventsPage, error) {
	var doc struct {
		Events []json.RawMessage `json:"events"`
		Cursor string            `json:"cursor"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return eventsPage{}, fmt.Errorf("decoding events: %w", err)
	}
	p := eventsPage{cursor: doc.Cursor}
	for _, raw := range doc.Events {
		var e struct {
			EventTicker       string             `json:"event_ticker"`
			SettlementSources []settlementSource `json:"settlement_sources"`
			Markets           []json.RawMessage  `json:"markets"`
		}
		if err := json.Unmarshal(raw, &e); err != nil {
			p.skipped = append(p.skipped, skip{reason: "event: " + err.Error()})
			continue
		}
		ev := event{ticker: e.EventTicker, settlementSources: e.SettlementSources}
		ev.markets, ev.skipped = decodeMarkets(e.Markets)
		p.events = append(p.events, ev)
	}
	return p, nil
}

func decodeMarkets(raws []json.RawMessage) ([]market, []skip) {
	var (
		out     []market
		skipped []skip
	)
	for _, raw := range raws {
		var m market
		if err := json.Unmarshal(raw, &m); err != nil {
			skipped = append(skipped, skip{reason: "market: " + err.Error()})
			continue
		}
		out = append(out, m)
	}
	return out, skipped
}

// Text caps keep hostile venue text from bloating stored records.
const (
	maxTitleBytes = 1000
	maxRulesBytes = 20000
)

// feeSchedule is the Kalshi taker fee (domain.md § Fees).
// Verified 2026-10-07 against "Fee Schedule for July 2026, 7.7.26 Update"
// (effective July 7, 2026): taker fee = round up(M × 0.07 × C × P × (1-P)),
// where M is the series multiplier (default 1). The document states "round up
// such that fee + positionCost is rounded to a centicent", but the published
// table matches whole-order ceiling to the cent (default round_to = 0.01).
func feeSchedule() canonical.FeeSchedule {
	return canonical.FeeSchedule{
		ID:       canonical.FeeScheduleKalshiTakerV1,
		Params:   map[string]decimal.Decimal{"multiplier": decimal.RequireFromString("0.07")},
		Verified: true,
		Source:   "https://kalshi.com/docs/kalshi-fee-schedule.pdf",
	}
}

// toInstrument maps a Kalshi market into a canonical Instrument. It returns an
// error (and the record must be skipped) when the market has no usable ticker
// or title; other problems are recorded in Quality. settlementSources are event-level
// settlement_sources from the Kalshi API, appended to the rules text for normalizer access.
func toInstrument(m market, cat canonical.Category, settlementSources []settlementSource) (canonical.Instrument, error) {
	ticker := strings.TrimSpace(m.Ticker)
	if ticker == "" {
		return canonical.Instrument{}, errors.New("missing ticker")
	}
	id := canonical.InstrumentID(canonical.VenueKalshi, ticker)
	if err := canonical.ValidateInstrumentID(id); err != nil {
		return canonical.Instrument{}, fmt.Errorf("invalid ticker: %w", err)
	}
	var q canonical.Quality
	title := cleanText(&q, "title", m.Title, maxTitleBytes)
	if title == "" {
		return canonical.Instrument{}, errors.New("missing title")
	}
	if sub := cleanText(&q, "yes_sub_title", m.YesSubTitle, maxTitleBytes); sub != "" && !strings.Contains(title, sub) {
		title += " — " + sub
	}
	// Capture rules layers (UI-21): keep RulesText as combined for matching compatibility,
	// but also store clarifications separately for display.
	rulesPrimary := cleanText(&q, "rules_primary", m.RulesPrimary, maxRulesBytes)
	rulesSecondary := cleanText(&q, "rules_secondary", m.RulesSecondary, maxRulesBytes)
	rules := strings.TrimSpace(rulesPrimary + "\n\n" + rulesSecondary)
	if rules == "" {
		q.Add(canonical.QualityMissingField, "rules_primary", "no rules text")
	}
	// Append event-level settlement_sources to rules for normalizer access (D-108).
	// Use a specific marker format that won't trigger existing politicsSource patterns.
	if len(settlementSources) > 0 {
		var sourcesText string
		for _, src := range settlementSources {
			if sourcesText != "" {
				sourcesText += ", "
			}
			sourcesText += src.Name
		}
		if rules != "" {
			rules += "\n\n"
		}
		rules += "KALSHI_EVENT_SETTLEMENT_SOURCES: " + sourcesText
	}
	clarifications := strings.TrimSpace(rulesSecondary)
	eventTicker := cleanText(&q, "event_ticker", m.EventTicker, 128)
	if eventTicker == "" {
		q.Add(canonical.QualityMissingField, "event_ticker", "no event ticker")
	}
	ins := canonical.Instrument{
		ID:             id,
		Venue:          canonical.VenueKalshi,
		VenueMarketID:  ticker,
		VenueEventID:   eventTicker,
		Category:       cat,
		Title:          title,
		RulesText:      rules,
		RulesSummary:   rulesPrimary, // Kalshi's rules_primary is the per-strike summary
		Clarifications: clarifications,
		// ClarificationsUpdatedAt: Kalshi does not provide a specific timestamp for clarifications
		Terms:          canonical.UnknownTerms(),
		CloseTime:      parseTime(&q, "close_time", m.CloseTime, true),
		ExpirationTime: parseTime(&q, "expiration_time", m.ExpirationTime, false),
		Status:         mapStatus(m.Status),
		FeeSchedule:    feeSchedule(),
	}
	ins.Quality = q
	if err := ins.Validate(); err != nil {
		return canonical.Instrument{}, err
	}
	return ins, nil
}

// cleanText trims s, drops NUL bytes (Postgres text cannot hold them) and
// caps its length, flagging the record when it had to cut.
func cleanText(q *canonical.Quality, field, s string, maxBytes int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\x00", ""))
	if len(s) > maxBytes {
		q.Add("text_truncated", field, fmt.Sprintf("cut to %d bytes", maxBytes))
		s = truncate(s, maxBytes)
	}
	return s
}

// parseTime parses an RFC 3339 timestamp into UTC. An empty or unparseable
// value gives the zero time and a Quality flag (missing only if required).
func parseTime(q *canonical.Quality, field, s string, required bool) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		if required {
			q.Add(canonical.QualityMissingField, field, "not published")
		}
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil || t.Year() < 2000 || t.Year() > 2200 {
		q.Add(canonical.QualityInvalidTime, field, "unparseable timestamp "+truncate(s, 40))
		return time.Time{}
	}
	return t.UTC()
}

// mapStatus maps Kalshi market statuses onto canonical statuses.
func mapStatus(s string) canonical.InstrumentStatus {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "active", "open":
		return canonical.StatusOpen
	case "closed", "inactive", "paused":
		return canonical.StatusClosed
	case "settled", "finalized", "determined":
		return canonical.StatusSettled
	}
	return canonical.StatusUnknown
}

// mapResult maps a settled market's result to an outcome; ok is false for
// anything but a plain yes or no (e.g. a voided or scalar result).
func mapResult(s string) (canonical.Outcome, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "yes":
		return canonical.OutcomeYes, true
	case "no":
		return canonical.OutcomeNo, true
	}
	return "", false
}

// book is a decoded order book: bids on each side, best (highest) first.
type book struct {
	yesBids []canonical.Level
	noBids  []canonical.Level
	quality canonical.Quality
}

// parseOrderbook decodes a /markets/{ticker}/orderbook response. Malformed
// levels are skipped and flagged; an invalid top-level document is an error.
func parseOrderbook(body []byte) (book, error) {
	var doc struct {
		Orderbook *struct {
			Yes json.RawMessage `json:"yes_dollars"`
			No  json.RawMessage `json:"no_dollars"`
		} `json:"orderbook_fp"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return book{}, fmt.Errorf("decoding orderbook: %w", err)
	}
	if doc.Orderbook == nil {
		return book{}, errors.New("decoding orderbook: no orderbook_fp object")
	}
	var b book
	b.yesBids = parseSide(&b.quality, "yes_dollars", doc.Orderbook.Yes)
	b.noBids = parseSide(&b.quality, "no_dollars", doc.Orderbook.No)
	return b, nil
}

// parseSide decodes one side's [[price, size], ...] levels, skipping and
// flagging bad ones, merging duplicate prices and sorting best (highest) first.
func parseSide(q *canonical.Quality, field string, raw json.RawMessage) []canonical.Level {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		q.Add(canonical.QualityRecordSkipped, field, "side is not an array of levels")
		return nil
	}
	byPrice := map[string]canonical.Level{}
	for i, r := range rows {
		var pair []string
		if err := json.Unmarshal(r, &pair); err != nil || len(pair) != 2 {
			q.Add(canonical.QualityRecordSkipped, field, fmt.Sprintf("level %d is not a [price, size] pair of strings", i))
			continue
		}
		p, err := parseDollars(pair[0])
		if err != nil {
			q.Add(canonical.QualityInvalidPrice, field, fmt.Sprintf("level %d: %v", i, err))
			continue
		}
		s, err := parseFP(pair[1])
		if err != nil || !s.IsPositive() {
			q.Add(canonical.QualityInvalidSize, field, fmt.Sprintf("level %d: size %q is not positive", i, truncate(pair[1], 32)))
			continue
		}
		key := p.String()
		l := byPrice[key]
		l.Price = p
		l.Size = l.Size.Add(s)
		byPrice[key] = l
	}
	out := make([]canonical.Level, 0, len(byPrice))
	for _, l := range byPrice {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Price.GreaterThan(out[j].Price) })
	return out
}

// yesAsks derives YES asks from NO bids (D-010): a NO bid at p is a YES offer
// at 1 − p for the same size. noBids is best (highest) first, so the result is
// best (lowest) first.
func yesAsks(noBids []canonical.Level) []canonical.Level {
	out := make([]canonical.Level, 0, len(noBids))
	for _, l := range noBids {
		out = append(out, canonical.Level{Price: one.Sub(l.Price), Size: l.Size})
	}
	return out
}

// truncate shortens s to at most n bytes on a rune boundary.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := 0
	for i := range s {
		if i > n {
			break
		}
		cut = i
	}
	return s[:cut]
}

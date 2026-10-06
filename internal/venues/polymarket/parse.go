package polymarket

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// Limits on individual fields, so hostile values cannot cost much to parse.
const (
	maxDecimalLen     = 40   // longest price, size or fee string accepted
	maxEncodedArray   = 4096 // longest double-encoded array string accepted
	maxEncodedEntries = 16   // binary markets have two outcomes; allow some slack
)

// tokenIDPattern is a CLOB token ID: a decimal uint256 (up to 78 digits).
var tokenIDPattern = regexp.MustCompile(`^[0-9]{1,78}$`)

// gammaEvent is the subset of a Gamma /events record the adapter reads.
type gammaEvent struct {
	ID              string            `json:"id"`
	Slug            string            `json:"slug"`
	Title           string            `json:"title"`
	EndDate         string            `json:"endDate"`
	NegRisk         bool              `json:"negRisk"`
	NegRiskMarketID string            `json:"negRiskMarketID"`
	Markets         []json.RawMessage `json:"markets"`
}

// gammaMarket is the subset of a Gamma market record the adapter reads.
// outcomes, outcomePrices and clobTokenIds are JSON arrays encoded as strings.
type gammaMarket struct {
	ID              string          `json:"id"`
	Question        string          `json:"question"`
	Description     string          `json:"description"`
	ConditionID     string          `json:"conditionId"`
	Slug            string          `json:"slug"`
	EndDate         string          `json:"endDate"`
	ClosedTime      string          `json:"closedTime"`
	Active          *bool           `json:"active"`
	Closed          bool            `json:"closed"`
	NegRisk         bool            `json:"negRisk"`
	Outcomes        json.RawMessage `json:"outcomes"`
	OutcomePrices   json.RawMessage `json:"outcomePrices"`
	ClobTokenIDs    json.RawMessage `json:"clobTokenIds"`
	FeesEnabled     *bool           `json:"feesEnabled"`
	FeeSchedule     *gammaFee       `json:"feeSchedule"`
	EnableOrderBook *bool           `json:"enableOrderBook"`
}

// gammaFee is the per-market fee schedule Gamma publishes. Its formula is
// interpreted by internal/fees (M1-07); the adapter only carries the numbers.
type gammaFee struct {
	Rate     json.Number `json:"rate"`
	Exponent json.Number `json:"exponent"`
}

// clobBook is a CLOB /book response.
type clobBook struct {
	Market    string      `json:"market"`
	AssetID   string      `json:"asset_id"`
	Timestamp string      `json:"timestamp"`
	Bids      []clobLevel `json:"bids"`
	Asks      []clobLevel `json:"asks"`
}

type clobLevel struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

// binaryMarket is a Gamma market that passed validation as a YES/NO binary.
type binaryMarket struct {
	gammaMarket
	YesToken string
	NoToken  string
	// YesIndex is the position of "Yes" in outcomes (and outcomePrices).
	YesIndex int
}

// errNotBinary marks a well-formed market that is not a YES/NO binary (e.g.
// "Up"/"Down"); it is out of scope rather than malformed.
var errNotBinary = errors.New("not a yes/no binary market")

// decodeStringArray decodes one of Gamma's double-encoded fields: a JSON
// string whose content is a JSON array of strings, e.g. "[\"Yes\", \"No\"]".
// A plain JSON array is accepted too, in case Gamma stops double-encoding.
// null or an absent field gives an empty slice.
func decodeStringArray(raw json.RawMessage) ([]string, error) {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return nil, nil
	}
	if len(s) > maxEncodedArray {
		return nil, fmt.Errorf("encoded array is %d bytes, over %d", len(s), maxEncodedArray)
	}
	inner := []byte(s)
	if s[0] == '"' {
		var str string
		if err := json.Unmarshal(inner, &str); err != nil {
			return nil, fmt.Errorf("decoding outer string: %w", err)
		}
		inner = []byte(str)
	}
	var out []string
	if err := json.Unmarshal(inner, &out); err != nil {
		return nil, fmt.Errorf("decoding inner array: %w", err)
	}
	if len(out) > maxEncodedEntries {
		return nil, fmt.Errorf("array has %d entries, over %d", len(out), maxEncodedEntries)
	}
	return out, nil
}

// parseDecimal parses a venue decimal string, rejecting empty, overlong and
// non-finite values.
func parseDecimal(s string) (decimal.Decimal, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return decimal.Decimal{}, errors.New("empty decimal")
	}
	if len(s) > maxDecimalLen {
		return decimal.Decimal{}, fmt.Errorf("decimal %q is too long", s[:maxDecimalLen])
	}
	if strings.ContainsAny(s, "eE") {
		// Exponent notation is never used by the CLOB or Gamma for prices and
		// would let "1e999999999" cost unbounded work downstream.
		return decimal.Decimal{}, fmt.Errorf("decimal %q uses exponent notation", s)
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Decimal{}, fmt.Errorf("decimal %q: %w", s, err)
	}
	return d, nil
}

// parsePrice parses a probability in [0,1] (NFR-08).
func parsePrice(s string) (decimal.Decimal, error) {
	d, err := parseDecimal(s)
	if err != nil {
		return decimal.Decimal{}, err
	}
	if err := canonical.ValidatePrice(d); err != nil {
		return decimal.Decimal{}, err
	}
	return d, nil
}

// parseSize parses a positive order size.
func parseSize(s string) (decimal.Decimal, error) {
	d, err := parseDecimal(s)
	if err != nil {
		return decimal.Decimal{}, err
	}
	if !d.IsPositive() {
		return decimal.Decimal{}, fmt.Errorf("size %s must be positive", d)
	}
	return d, nil
}

// gammaTimeLayouts are the timestamp forms seen in Gamma responses.
var gammaTimeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02 15:04:05.999999-07",
	"2006-01-02 15:04:05-07",
	"2006-01-02T15:04:05.999999",
	"2006-01-02",
}

// parseGammaTime parses a Gamma timestamp into UTC. Empty input gives the zero
// time and no error; the caller flags it as missing.
func parseGammaTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	if len(s) > 64 {
		return time.Time{}, errors.New("timestamp is too long")
	}
	for _, layout := range gammaTimeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("timestamp %q has an unknown format", s)
}

// parseBookTimestamp parses the CLOB book timestamp: Unix milliseconds as a string.
func parseBookTimestamp(s string) (time.Time, error) {
	ms, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("book timestamp %q is not unix milliseconds", truncate(s, 32))
	}
	// Accept 2000-01-01 .. 2100-01-01; anything else is not a real book time.
	if ms < 946684800000 || ms > 4102444800000 {
		return time.Time{}, fmt.Errorf("book timestamp %d is out of range", ms)
	}
	return time.UnixMilli(ms).UTC(), nil
}

// parseEvents decodes a Gamma /events page. One malformed event does not stop
// the others: it is returned in skipped with the reason. A body that is not a
// JSON array is an error.
func parseEvents(body []byte) (events []gammaEvent, skipped []string, err error) {
	var raws []json.RawMessage
	if err := json.Unmarshal(body, &raws); err != nil {
		return nil, nil, fmt.Errorf("decoding events page: %w", err)
	}
	for i, raw := range raws {
		var ev gammaEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			skipped = append(skipped, fmt.Sprintf("event[%d]: %v", i, err))
			continue
		}
		if ev.Slug == "" {
			skipped = append(skipped, fmt.Sprintf("event[%d] %q: missing slug", i, truncate(ev.ID, 32)))
			continue
		}
		events = append(events, ev)
	}
	return events, skipped, nil
}

// parseMarket decodes one market and checks it is a YES/NO binary with two
// valid token IDs. It returns errNotBinary for well-formed non-binary markets.
func parseMarket(raw json.RawMessage) (binaryMarket, error) {
	var m gammaMarket
	if err := json.Unmarshal(raw, &m); err != nil {
		return binaryMarket{}, fmt.Errorf("decoding market: %w", err)
	}
	if strings.TrimSpace(m.Question) == "" {
		return binaryMarket{}, errors.New("missing question")
	}
	if m.ConditionID == "" {
		return binaryMarket{}, errors.New("missing conditionId")
	}
	outcomes, err := decodeStringArray(m.Outcomes)
	if err != nil {
		return binaryMarket{}, fmt.Errorf("outcomes: %w", err)
	}
	yes, no := -1, -1
	for i, o := range outcomes {
		switch strings.ToLower(strings.TrimSpace(o)) {
		case "yes":
			yes = i
		case "no":
			no = i
		}
	}
	if len(outcomes) != 2 || yes < 0 || no < 0 {
		return binaryMarket{}, errNotBinary
	}
	tokens, err := decodeStringArray(m.ClobTokenIDs)
	if err != nil {
		return binaryMarket{}, fmt.Errorf("clobTokenIds: %w", err)
	}
	if len(tokens) != 2 {
		return binaryMarket{}, fmt.Errorf("clobTokenIds has %d entries, want 2", len(tokens))
	}
	for _, t := range tokens {
		if !tokenIDPattern.MatchString(t) {
			return binaryMarket{}, fmt.Errorf("clob token id %q is not a decimal token id", truncate(t, 32))
		}
	}
	if tokens[0] == tokens[1] {
		return binaryMarket{}, errors.New("yes and no token ids are equal")
	}
	return binaryMarket{gammaMarket: m, YesToken: tokens[yes], NoToken: tokens[no], YesIndex: yes}, nil
}

// settledOutcome reads a closed binary market's final outcomePrices. Exactly
// 1 for YES and 0 for NO (or the reverse) resolves it; anything else, such as a
// 50/50 split or a not-yet-final price, is ambiguous.
func settledOutcome(m binaryMarket) (canonical.Outcome, error) {
	prices, err := decodeStringArray(m.OutcomePrices)
	if err != nil {
		return "", fmt.Errorf("outcomePrices: %w", err)
	}
	if len(prices) != 2 {
		return "", fmt.Errorf("outcomePrices has %d entries, want 2", len(prices))
	}
	yes, errY := parsePrice(prices[m.YesIndex])
	no, errN := parsePrice(prices[1-m.YesIndex])
	if errY != nil || errN != nil {
		return "", fmt.Errorf("outcomePrices %v: %w", prices, errors.Join(errY, errN))
	}
	one := decimal.NewFromInt(1)
	switch {
	case yes.Equal(one) && no.IsZero():
		return canonical.OutcomeYes, nil
	case yes.IsZero() && no.Equal(one):
		return canonical.OutcomeNo, nil
	}
	return "", fmt.Errorf("final outcomePrices %v are not 1/0", prices)
}

// feeSchedule builds the canonical fee schedule from Gamma's per-market fee
// fields. Params carry the venue's numbers verbatim ("rate", "exponent");
// feesEnabled=false is recorded as rate 0. Unparseable numbers are left out
// and flagged, so internal/fees treats the fee as unknown.
func feeSchedule(m gammaMarket, q *canonical.Quality) canonical.FeeSchedule {
	fs := canonical.FeeSchedule{ID: canonical.FeeSchedulePolymarketV1}
	switch {
	case m.FeesEnabled != nil && !*m.FeesEnabled:
		fs.Params = map[string]decimal.Decimal{"rate": decimal.Zero}
	case m.FeeSchedule != nil:
		params := map[string]decimal.Decimal{}
		for name, n := range map[string]json.Number{"rate": m.FeeSchedule.Rate, "exponent": m.FeeSchedule.Exponent} {
			if n == "" {
				continue
			}
			d, err := parseDecimal(n.String())
			if err != nil || d.IsNegative() {
				q.Add(canonical.QualityMissingField, "feeSchedule."+name, fmt.Sprintf("unusable fee %s", truncate(n.String(), 32)))
				continue
			}
			params[name] = d
		}
		if len(params) > 0 {
			fs.Params = params
		}
	default:
		q.Add(canonical.QualityMissingField, "feeSchedule", "market publishes no fee schedule")
	}
	return fs
}

// truncate shortens s to at most n bytes on a rune boundary, for logs and flags.
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
	return s[:cut] + "…"
}

package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// Decoder limits (AB-13). A valid verdict is well under 1 KiB; anything
// larger is refused before it is parsed.
const (
	MaxResponseBytes        = 16 << 10
	MaxDifferingFields      = 16
	MaxDifferingFieldChars  = 64
	maxResponseNestingDepth = 8
)

// Outcome records what the guard did with a verdict. Outcomes are the
// decision-log kinds for LLM events.
type Outcome string

// Outcomes.
const (
	// OutcomeConfirmed: the model agreed with the rules type.
	OutcomeConfirmed Outcome = "llm_confirmed"
	// OutcomeDowngraded: the model chose a strictly weaker type, which was applied.
	OutcomeDowngraded Outcome = "llm_downgraded"
	// OutcomeRejectedUpgrade: the model tried to upgrade, create or move the
	// relationship sideways; the verdict was discarded (D-022).
	OutcomeRejectedUpgrade Outcome = "llm_rejected_upgrade"
	// OutcomeError: the call failed or the output failed the strict decode or
	// validation; the rules verdict stands.
	OutcomeError Outcome = "llm_error"
)

// Result is the guard's full answer: the relationship to store, what
// happened, and a short reason suitable for the decision log.
type Result struct {
	Relationship canonical.Relationship
	Outcome      Outcome
	Reason       string
}

// wireVerdict is the exact JSON schema accepted from the model. Pointers
// detect missing keys; confidence stays raw so a quoted number is rejected.
type wireVerdict struct {
	Relationship    *string         `json:"relationship"`
	DifferingFields []string        `json:"differing_fields"`
	Confidence      json.RawMessage `json:"confidence"`
	Evidence        []string        `json:"evidence"`
}

// Decode strictly decodes a model response into an LLMVerdict: at most
// MaxResponseBytes, exactly one JSON object with no unknown keys and nothing
// after it, confidence a JSON number in [0,1], a valid relationship type, at
// most MaxDifferingFields printable field names, and evidence within the
// canonical limits. A ```json fence around the object is tolerated; anything
// else fails. Decode never panics on hostile input.
func Decode(raw string) (canonical.LLMVerdict, error) {
	if len(raw) > MaxResponseBytes {
		return canonical.LLMVerdict{}, fmt.Errorf("response is %d bytes; at most %d allowed", len(raw), MaxResponseBytes)
	}
	s := stripFence(strings.TrimSpace(raw))
	if err := checkNesting(s); err != nil {
		return canonical.LLMVerdict{}, err
	}
	dec := json.NewDecoder(strings.NewReader(s))
	dec.DisallowUnknownFields()
	var w wireVerdict
	if err := dec.Decode(&w); err != nil {
		return canonical.LLMVerdict{}, fmt.Errorf("response is not the verdict schema: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return canonical.LLMVerdict{}, errors.New("response has content after the JSON object")
	}
	return toVerdict(w)
}

func toVerdict(w wireVerdict) (canonical.LLMVerdict, error) {
	if w.Relationship == nil {
		return canonical.LLMVerdict{}, errors.New("relationship is required")
	}
	conf, err := decodeConfidence(w.Confidence)
	if err != nil {
		return canonical.LLMVerdict{}, err
	}
	v := canonical.LLMVerdict{
		Relationship:    canonical.RelationshipType(*w.Relationship),
		DifferingFields: w.DifferingFields,
		Confidence:      conf,
		Evidence:        w.Evidence,
	}
	if v.DifferingFields == nil {
		v.DifferingFields = []string{}
	}
	if v.Evidence == nil {
		v.Evidence = []string{}
	}
	if err := v.Validate(); err != nil {
		return canonical.LLMVerdict{}, err
	}
	if len(v.DifferingFields) > MaxDifferingFields {
		return canonical.LLMVerdict{}, fmt.Errorf("differing_fields has %d entries; at most %d allowed", len(v.DifferingFields), MaxDifferingFields)
	}
	for i, f := range v.DifferingFields {
		if utf8.RuneCountInString(f) > MaxDifferingFieldChars || !printable(f, false) {
			return canonical.LLMVerdict{}, fmt.Errorf("differing_fields[%d] is not a short printable field name", i)
		}
	}
	for i, e := range v.Evidence {
		if !printable(e, true) {
			return canonical.LLMVerdict{}, fmt.Errorf("evidence[%d] contains control or format characters", i)
		}
	}
	return v, nil
}

// confidencePattern is the only number form accepted for confidence: a plain
// decimal with at most 12 fraction digits and no exponent, so a hostile
// "1e999999999" never reaches decimal arithmetic (the range check follows).
var confidencePattern = regexp.MustCompile(`^-?[0-9](\.[0-9]{1,12})?$`)

// decodeConfidence accepts only a bare, plain JSON number.
func decodeConfidence(raw json.RawMessage) (decimal.Decimal, error) {
	if len(raw) == 0 {
		return decimal.Zero, errors.New("confidence is required")
	}
	if c := raw[0]; c != '-' && (c < '0' || c > '9') {
		return decimal.Zero, errors.New("confidence must be a JSON number")
	}
	if !confidencePattern.Match(raw) {
		return decimal.Zero, errors.New("confidence must be a plain decimal number")
	}
	d, err := decimal.NewFromString(string(raw))
	if err != nil {
		return decimal.Zero, fmt.Errorf("confidence %q: %w", raw, err)
	}
	return d, nil
}

// stripFence removes a surrounding ```json … ``` or ``` … ``` fence.
func stripFence(s string) string {
	if !strings.HasPrefix(s, "```") || !strings.HasSuffix(s, "```") || len(s) < 6 {
		return s
	}
	body := strings.TrimSuffix(s, "```")
	body = strings.TrimPrefix(body, "```")
	body = strings.TrimPrefix(body, "json")
	return strings.TrimSpace(body)
}

// checkNesting refuses deeply nested payloads before the decoder sees them.
func checkNesting(s string) error {
	depth := 0
	for _, r := range s {
		switch r {
		case '{', '[':
			depth++
			if depth > maxResponseNestingDepth {
				return fmt.Errorf("response nests deeper than %d levels", maxResponseNestingDepth)
			}
		case '}', ']':
			depth--
		}
	}
	return nil
}

// printable reports whether s holds no control, format, private-use or
// invalid characters. Newlines and tabs are allowed only when multiline.
func printable(s string, multiline bool) bool {
	for _, r := range s {
		if multiline && (r == '\n' || r == '\t') {
			continue
		}
		if r == utf8.RuneError || unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Co, r) {
			return false
		}
	}
	return true
}

// Apply enforces downgrade-only (D-022, brief hard constraint 5): the result's
// Type is never stronger than r.Type. A confirmation or strict downgrade is
// applied with Method rules+llm and the lower of the two confidences. An
// upgrade, a new relationship where the rules said unrelated, or a sideways
// move between complement and implies is discarded and logged as
// llm_rejected_upgrade; an invalid verdict is logged as llm_error. In both
// cases the rules verdict stands. Logs go to slog.Default(); use Guard for an
// explicit logger and the outcome.
func Apply(r canonical.Relationship, v canonical.LLMVerdict) canonical.Relationship {
	return Guard(context.Background(), slog.Default(), r, v).Relationship
}

// Guard is Apply with an explicit context and logger, returning the outcome
// and reason as well as the relationship.
func Guard(ctx context.Context, log *slog.Logger, r canonical.Relationship, v canonical.LLMVerdict) Result {
	if !r.Type.Valid() {
		return fail(ctx, log, r, fmt.Errorf("rules relationship type %q is not valid", r.Type))
	}
	if err := v.Validate(); err != nil {
		return fail(ctx, log, r, fmt.Errorf("verdict invalid: %w", err))
	}
	if !r.Type.CanBecome(v.Relationship) {
		reason := fmt.Sprintf("LLM proposed %s but the rules say %s; an LLM may only downgrade, so the proposal was discarded", v.Relationship, r.Type)
		log.LogAttrs(ctx, slog.LevelWarn, "llm verdict rejected",
			slog.String("event", string(OutcomeRejectedUpgrade)),
			slog.String("a_id", r.AID), slog.String("b_id", r.BID),
			slog.String("rules_type", string(r.Type)), slog.String("llm_type", string(v.Relationship)),
			slog.String("llm_confidence", v.Confidence.String()))
		return Result{Relationship: withNote(r, reason+"."), Outcome: OutcomeRejectedUpgrade, Reason: reason}
	}
	out := r
	out.Method = canonical.MethodRulesLLM
	out.Confidence = decimal.Min(r.Confidence, v.Confidence)
	outcome := OutcomeConfirmed
	reason := fmt.Sprintf("LLM confirmed %s (confidence %s)", r.Type, v.Confidence)
	if v.Relationship != r.Type {
		out.Type = v.Relationship
		outcome = OutcomeDowngraded
		reason = fmt.Sprintf("LLM downgraded %s to %s (confidence %s)", r.Type, v.Relationship, v.Confidence)
	}
	if len(v.DifferingFields) > 0 {
		reason += "; differing fields: " + strings.Join(v.DifferingFields, ", ")
	}
	log.LogAttrs(ctx, slog.LevelInfo, "llm verdict applied",
		slog.String("event", string(outcome)),
		slog.String("a_id", r.AID), slog.String("b_id", r.BID),
		slog.String("rules_type", string(r.Type)), slog.String("llm_type", string(v.Relationship)),
		slog.String("llm_confidence", v.Confidence.String()))
	return Result{Relationship: withNote(out, reason+"."), Outcome: outcome, Reason: reason}
}

// GuardError records a failed call or decode (AB-13): the rules verdict
// stands, the relationship notes the failure, and the outcome is llm_error.
func GuardError(ctx context.Context, log *slog.Logger, r canonical.Relationship, err error) Result {
	return fail(ctx, log, r, err)
}

func fail(ctx context.Context, log *slog.Logger, r canonical.Relationship, err error) Result {
	reason := "LLM verdict unavailable (" + summary(err) + "); rules verdict stands"
	log.LogAttrs(ctx, slog.LevelWarn, "llm verdict unavailable",
		slog.String("event", string(OutcomeError)),
		slog.String("a_id", r.AID), slog.String("b_id", r.BID),
		slog.String("rules_type", string(r.Type)), slog.String("error", summary(err)))
	return Result{Relationship: withNote(r, reason+"."), Outcome: OutcomeError, Reason: reason}
}

// summary shortens an error for logs and explanations. Errors may quote
// model output, so they are sanitised and capped.
func summary(err error) string {
	if err == nil {
		return "unknown error"
	}
	return truncateRunes(stripRunes(strings.ReplaceAll(err.Error(), "\n", " ")), 200)
}

// withNote appends the guard's note to the relationship explanation, keeping
// it non-empty and human-readable.
func withNote(r canonical.Relationship, note string) canonical.Relationship {
	switch {
	case strings.TrimSpace(r.Explanation) == "":
		r.Explanation = note
	default:
		r.Explanation = strings.TrimRight(r.Explanation, " ") + " " + note
	}
	return r
}

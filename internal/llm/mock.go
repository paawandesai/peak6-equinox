package llm

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// HonestResponse is the benign JSON a well-behaved model returns in tests.
const HonestResponse = `{"relationship":"related","differing_fields":[],"confidence":0.5,"evidence":[]}`

// MockVerifier is a scripted Verifier: it returns Verdicts in order (the
// last one repeats) or Err, and records every call.
type MockVerifier struct {
	Verdicts []canonical.LLMVerdict
	Err      error

	mu    sync.Mutex
	calls int
}

// Verify returns the next scripted verdict or the scripted error.
// Mocks return zero usage (no actual API call).
func (m *MockVerifier) Verify(_ context.Context, _, _ canonical.Instrument, _ canonical.Relationship) (VerifyResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := m.calls
	m.calls++
	if m.Err != nil {
		return VerifyResult{}, m.Err
	}
	if len(m.Verdicts) == 0 {
		return VerifyResult{}, errors.New("mock verifier has no scripted verdicts")
	}
	if i >= len(m.Verdicts) {
		i = len(m.Verdicts) - 1
	}
	return VerifyResult{Verdict: m.Verdicts[i], Usage: Usage{}}, nil
}

// Calls reports how many times Verify ran.
func (m *MockVerifier) Calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// ScriptedModel is a Model that returns Responses in order (the last one
// repeats) or Err. It records the prompts it saw.
type ScriptedModel struct {
	Responses []string
	Err       error

	mu      sync.Mutex
	prompts []Prompt
}

// Complete returns the next scripted response.
// Mocks return zero usage (no actual API call).
func (m *ScriptedModel) Complete(_ context.Context, p Prompt) (ModelResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := len(m.prompts)
	m.prompts = append(m.prompts, p)
	if m.Err != nil {
		return ModelResult{}, m.Err
	}
	if len(m.Responses) == 0 {
		return ModelResult{}, errors.New("scripted model has no responses")
	}
	if i >= len(m.Responses) {
		i = len(m.Responses) - 1
	}
	return ModelResult{Text: ExpandPayload(m.Responses[i]), Usage: Usage{}}, nil
}

// Prompts returns a copy of every prompt the model received.
func (m *ScriptedModel) Prompts() []Prompt {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Prompt(nil), m.prompts...)
}

// AdversarialMock is a Model that is fully compromised: whenever the venue
// text in the prompt contains one of its Demands (matched after
// NormalizeText, so obfuscation does not hide it), it emits the demanded raw
// response, whatever that is: an upgrade, malformed JSON, extra fields or a
// huge payload. Otherwise it answers with Honest. CI runs every injection case
// through it to prove the guard holds even when the model obeys the attacker
// (AB-01).
type AdversarialMock struct {
	// Demands maps injected text to the response the attacker wants.
	Demands map[string]string
	// Honest is returned when no demand matches. Empty means HonestResponse.
	Honest string

	mu     sync.Mutex
	obeyed []string
}

// NewAdversarialMock returns a mock that obeys one demand.
func NewAdversarialMock(demand, response string) *AdversarialMock {
	return &AdversarialMock{Demands: map[string]string{demand: response}}
}

// Complete obeys the first matching demand found in the prompt's user turn.
// Mocks return zero usage (no actual API call).
func (m *AdversarialMock) Complete(_ context.Context, p Prompt) (ModelResult, error) {
	user := NormalizeText(p.User)
	for _, demand := range slices.Sorted(maps.Keys(m.Demands)) {
		needle := NormalizeText(demand)
		if needle != "" && strings.Contains(user, needle) {
			m.mu.Lock()
			m.obeyed = append(m.obeyed, demand)
			m.mu.Unlock()
			return ModelResult{Text: ExpandPayload(m.Demands[demand]), Usage: Usage{}}, nil
		}
	}
	text := m.Honest
	if text == "" {
		text = HonestResponse
	}
	return ModelResult{Text: text, Usage: Usage{}}, nil
}

// Obeyed returns the demands the mock has followed so far.
func (m *AdversarialMock) Obeyed() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.obeyed...)
}

// HugePayloadBytes is the default size of the "@huge" payload.
const HugePayloadBytes = 2 << 20

// ExpandPayload turns the shorthand "@huge" or "@huge:<bytes>" into a
// syntactically valid verdict whose evidence is padded to the requested size,
// so case files stay readable. Any other string is returned unchanged.
func ExpandPayload(s string) string {
	rest, ok := strings.CutPrefix(strings.TrimSpace(s), "@huge")
	if !ok {
		return s
	}
	n := HugePayloadBytes
	if v, err := strconv.Atoi(strings.TrimPrefix(rest, ":")); err == nil && v > 0 && rest != "" {
		n = v
	} else if rest != "" {
		return s
	}
	return `{"relationship":"equivalent","differing_fields":[],"confidence":1,"evidence":["` +
		strings.Repeat("A", n) + `"]}`
}

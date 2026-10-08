// Package llm holds the LLM verifier contract and its safety harness: the
// Verifier interface, the fixed prompt (prompt.go), the strict verdict decoder
// and the downgrade-only guard (guard.go), the verdict cache key (cache.go) and
// the test doubles (mock.go). The Anthropic client itself arrives in M3.
//
// The only untrusted input the verifier ever sees is venue text, so this
// package treats every byte of a title or rules text as data (AB-01), and every
// byte the model returns as hostile until it has passed the strict decoder
// (AB-13) and the guard (D-022: an LLM may only downgrade).
package llm

import (
	"context"
	"fmt"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// VerifyResult is the response from one Verifier.Verify call.
type VerifyResult struct {
	Verdict canonical.LLMVerdict
	Usage   Usage
}

// Verifier is the LLM second opinion on a rules-based relationship
// (architecture.md). Verify returns the model's schema-valid verdict and
// per-call token usage; it never changes the relationship itself. Callers pass
// the verdict through Apply, which enforces downgrade-only.
type Verifier interface {
	Verify(ctx context.Context, a, b canonical.Instrument, r canonical.Relationship) (VerifyResult, error)
}

// Usage holds the token counts from one API call.
type Usage struct {
	InputTokens  int64
	OutputTokens int64
}

// ModelResult is the response from one Model.Complete call.
type ModelResult struct {
	Text  string
	Usage Usage
}

// Model is the raw text interface the verifier talks to: a prompt in, the
// model's response (text + usage) out. The Anthropic client (M3) and the mocks
// in mock.go implement it. Nothing a Model returns is trusted until Decode accepts it.
type Model interface {
	Complete(ctx context.Context, p Prompt) (ModelResult, error)
}

// verifier is the reference Verifier: BuildPrompt, then Model.Complete, then
// the strict Decode. It does not consult the rules relationship, so the model
// classifies the pair blind and cannot be steered by it.
type verifier struct {
	model Model
}

// NewVerifier wraps a Model in the standard prompt-and-decode pipeline.
func NewVerifier(m Model) Verifier { return verifier{model: m} }

// Verify builds the prompt for (a, b), asks the model and strictly decodes
// the answer. Returns the verdict and per-call token usage. A transport or
// decode failure is returned as an error; the caller records it as llm_error
// and keeps the rules verdict (GuardError). Token usage is recorded even when
// the decode fails (Cursor: record tokens even on decode error after successful HTTP).
func (v verifier) Verify(ctx context.Context, a, b canonical.Instrument, _ canonical.Relationship) (VerifyResult, error) {
	result, err := v.model.Complete(ctx, BuildPrompt(a, b))
	if err != nil {
		return VerifyResult{}, fmt.Errorf("completing verifier prompt: %w", err)
	}
	verdict, err := Decode(result.Text)
	if err != nil {
		// Cursor: record tokens even when decode fails (HTTP succeeded but JSON is bad)
		return VerifyResult{Usage: result.Usage}, fmt.Errorf("decoding verifier response: %w", err)
	}
	return VerifyResult{Verdict: verdict, Usage: result.Usage}, nil
}

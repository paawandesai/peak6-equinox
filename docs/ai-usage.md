<!-- OWNER: shared. Created in M1-11 (safety harness); M1-12 completes the deployment and development sections. Kept current by every ticket that touches LLM use. -->
# AI usage disclosure

Equinox uses AI in two places: a runtime LLM verifier inside the product, and AI tools during development. Both are described here (brief deliverable 6).

## Runtime LLM use

**Purpose.** The matcher is deterministic rules first (D-003). An LLM verifier gives a second opinion on two kinds of pairs: `uncertain` pairs the rules could not settle, and every rules `equivalent` (D-022). It arrives in M3; the safety harness it runs inside was built and tested first, in M1-11.

**Model.** Anthropic Messages API, model `claude-haiku-4-5-20251001`, the cheapest tier (D-046). `LLM_MODEL` overrides it. The production client is admin-triggered only; no public web request can cause a call (NFR-11, AB-04).

**The downgrade-only rule (brief hard constraint 5).** The LLM may confirm, downgrade or reject a rules-based relationship. It may never create or upgrade one. `internal/llm.Apply` enforces this in code using the strength order `equivalent` > `equivalent_given_facts` > `complement` = `implies` > `related` > `unrelated`:

| Model says | Guard does | Outcome logged |
|---|---|---|
| The same type as the rules | Keeps the type, records method `rules+llm`, confidence = the lower of the two | `llm_confirmed` |
| A strictly weaker type | Applies it, records method `rules+llm`, confidence = the lower of the two | `llm_downgraded` |
| A stronger type, any type when the rules said `unrelated`, or a sideways move between `complement` and `implies` | Discards the verdict; the rules verdict stands | `llm_rejected_upgrade` |
| Anything that fails the strict schema, or the call fails | Discards the output; the rules verdict stands | `llm_error` |

Every outcome appends a human-readable note to the relationship's explanation.

**Guardrails.**

- *Venue text is data.* Titles and rules are the only untrusted input (AB-01). `BuildPrompt` places them inside `<<<BEGIN:LABEL>>>` … `<<<END:LABEL>>>` blocks after a fixed system prompt that states the blocks are data and never instructions. Each field is NFKC-normalised, stripped of control, format (zero-width, bidi override, BOM), private-use and invalid characters, has every run of `<` or `>` collapsed and angle-bracket look-alikes removed (so no venue text can open or close a block), and is capped at 4,000 characters. The rules verdict is not shown to the model, so it cannot be steered by it.
- *Strict output schema (AB-13).* The model must answer with exactly `{"relationship", "differing_fields", "confidence", "evidence"}`. The decoder refuses responses over 16 KiB, unknown keys, anything after the JSON object, nesting deeper than 8 levels, a non-numeric or exponent-form confidence, a confidence outside [0,1], more than 3 evidence quotes or any over 200 characters, more than 16 differing fields or any over 64 characters, and control or format characters in either list. A ```json fence is tolerated.
- *Cache (AB-11).* Verdicts are cached by `CacheKey` = SHA-256 of both instruments' normalised title and rules text, the prompt version and the model ID, length-prefixed and order-sensitive (because `implies` is directional). A cached verdict goes through `Apply` like a fresh one, so a poisoned entry can never upgrade.
- *Spend cap.* `LLM_DAILY_CALL_CAP` (default 200) calls per UTC day; once reached the verifier is skipped and logged (M3).
- *Logging.* Guard events are structured `slog` records with the pair IDs, the rules type, the model's type and confidence. Raw model output and venue text are never logged.

**Fixed prompt-injection and jailbreak suite.** `testdata/llm/injection_cases.yaml` holds 32 fixed attacks: "ignore previous instructions", fake closing delimiters and fake system-role text, DAN-style jailbreaks, instructions in the title, zero-width, homoglyph and bidi obfuscation, prompt-in-a-URL, a request for the system prompt, JSON embedded in venue text, and hostile outputs (non-JSON, extra fields, out-of-range confidence, oversized evidence, a 2 MiB payload, trailing text, two objects, deep nesting), plus two control cases. CI runs every case through `BuildPrompt` → `AdversarialMock` (a model that obeys whatever the injected text demands) → `Decode` → `Apply` in `tests/security/ab01_prompt_injection_test.go`, and asserts the final relationship is never stronger than the rules verdict, every output validates, and nothing panics. **No live or paid API is ever called in CI.**

**Live eval (manual, admin only).** `make llm-eval-live` runs `internal/llm/live_eval_test.go` (build tag `llmlive`) against the real model: it skips unless `ANTHROPIC_API_KEY` is set, refuses to run when `CI` is set, and prints one PASS/FAIL line per case plus how often the model itself obeyed the injection and how often its output failed the schema. A run is about 32 calls of roughly 1,200 input tokens on the Haiku tier, well under one cent. Run it from the repo root with the key exported in the shell (never committed); results are informational, since the guard, not the model, is the control. The live eval has not yet been run as of 2026-10-06.

## Development AI use

Development is run through Agent Orchestrator (AO) per [operating-rules.md](operating-rules.md): a Grok operator drives AO, Claude Code workers implement one ticket each on their own branch, and a Cursor agent reviews every MR. Humans review at the points listed there: the kickoff pack, every Tier 2 decision (veto after the fact), every 2-critical decision (approval before acting), human-owned files, and the golden labels. M1-12 completes this section with the deployment details.

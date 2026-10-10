<!-- OWNER: shared. Created in M1-11 (safety harness); deployment and development sections completed in M1-12. Kept current by every ticket that touches LLM use. -->
# AI usage disclosure

Equinox uses AI in two places: a runtime LLM verifier inside the product, and AI tools during development. Both are described here (brief deliverable 6). Nothing on the public pages calls a model: matching, diffs and routing are deterministic code, and the verifier only ever adjusts a rules verdict downwards.

## Runtime LLM use

**Purpose.** The matcher is deterministic rules first (D-003). An LLM verifier gives a second opinion on two kinds of pairs: `uncertain` pairs the rules could not settle, and every rules `equivalent` (D-022). It arrived in M3-06; the safety harness it runs inside was built and tested first, in M1-11.

**Model.** Anthropic Messages API, model `claude-haiku-4-5-20251001` (`LLM_MODEL` in `.env.example`), the cheapest tier (D-046). `LLM_MODEL` overrides the default. The verifier is admin-triggered only via `POST /admin/llm-verify` (`internal/web/admin.go`); no public web request can cause a call (NFR-11, AB-04).

**Production status (2026-10-10).** The verifier is built and tested but **has not run in production**. The production service has no `ANTHROPIC_API_KEY` mounted: `make deploy` mounts `equinox-anthropic-key` only when that secret has an enabled version, and it has none. So every live relationship has method `rules`, `/admin` shows the verifier as "disabled: ANTHROPIC_API_KEY not set", and the rules+LLM column of the eval has not been run on the golden set. **`LLM_ENABLED` does not gate the verifier today.** `make deploy` sets `LLM_ENABLED=false`, but the code only checks that flag at config load (`LLM_ENABLED=true` without a key is a config error, `internal/config/config.go`). The admin verifier runs whenever a key is present, whatever the flag says (`internal/web/admin.go`). So the absence of the key, not the flag, is what keeps it off. Gating the verifier on the flag is a logic change, deferred until after the review window (status.md Q-11).

**What it may do.** The LLM may confirm, downgrade or reject a rules-based relationship (D-022). It may never create or upgrade one (brief hard constraint 5). `internal/llm/guard.go` function `Apply` enforces this in code using the strength order `equivalent` > `equivalent_given_facts` > `complement` = `implies` > `related` > `unrelated`:

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
- *Logging.* Guard events are structured `slog` records with the pair IDs, the rules type, the model's type and confidence. Raw model output and venue text are never logged.

**The cap and cache.** `LLM_DAILY_CALL_CAP` (`.env.example`, default 200) bounds the number of model calls per UTC day; once it is reached the verifier is skipped and the skip is logged, and the rules verdict stands (M3-06). `LLM_MAX_OUTPUT_TOKENS` (default 400) bounds each answer. Verdicts are cached by `CacheKey` = SHA-256 of both instruments' normalised title and rules text, the prompt version and the model ID, length-prefixed and order-sensitive (`internal/llm/cache.go`). At the Haiku tier the cap is well under one dollar a day; raising it is a 2-critical decision under operating-rules.md and needs the human's approval first. Spend is tracked in status.md against the $50 project budget.

**Prompt-injection suite (M1-11).** `testdata/llm/injection_cases.yaml` holds 32 fixed attacks: "ignore previous instructions", fake closing delimiters and fake system-role text, DAN-style jailbreaks, instructions in the title, zero-width, homoglyph and bidi obfuscation, prompt-in-a-URL, a request for the system prompt, JSON embedded in venue text, and hostile outputs (non-JSON, extra fields, out-of-range confidence, oversized evidence, a 2 MiB payload, trailing text, two objects, deep nesting), plus two control cases. CI runs every case through `BuildPrompt` → `AdversarialMock` (a model that obeys whatever the injected text demands) → `Decode` → `Apply` in `tests/security/ab01_prompt_injection_test.go`, and asserts the final relationship is never stronger than the rules verdict, every output validates, and nothing panics. **No live or paid API is ever called in CI.**

**Where verdicts are shown.** LLM outcomes (`llm_confirmed`, `llm_downgraded`, `llm_rejected_upgrade`, `llm_error`) are recorded in each relationship's `Explanation` field and shown on the pair page (`/pairs/<id>`, `internal/web/templates/pair.html`) and the decision log (`/log`, `internal/web/templates/log.html`). The admin page (`/admin`, `internal/web/templates/admin.html`) shows the last LLM verify run summary (verified count, downgraded count, cache hits, run timestamp; D-097, UI-08 follow-up #40).

**Live eval (manual, admin only).** `make llm-eval-live` runs `internal/llm/live_eval_test.go` (build tag `llmlive`) against the real model: it skips unless `ANTHROPIC_API_KEY` is set, refuses to run when `CI` is set, and prints one PASS/FAIL line per case plus how often the model itself obeyed the injection and how often its output failed the schema. A run is about 32 calls of roughly 1,200 input tokens on the Haiku tier, well under one cent. Run it from the repo root with the key exported in the shell (never committed); results are informational, since the guard, not the model, is the control. No live-eval run is recorded in the repo as of 2026-10-10.

## Spend to date

As of 2026-10-10 (also in `docs/status.md` "Spend to date"):
- **GCP:** not measured. Nobody has read the billing page since deploy. Whether the $50 budget alert asked for in status.md Q-3 is set could not be checked from the operator host (the budgets API needs billing-account access it lacks).
- **Anthropic (the product's key):** the production service has no key mounted, so it has made no model calls. No other use of the product key is recorded. The console total has not been read.
- **Development tools** (Claude Code, Cursor) run on the operators' own subscriptions, not on the project budget.
- **Total against the $50 cap (NFR-15):** not measured.

## Development AI use

Most of the code, tests and operator-owned documentation in this repository were written by AI agents under the process in [operating-rules.md](operating-rules.md). The roles, and what each one may and may not do:

**Grok operator running AO (D-039).** A Grok bot hosts and drives Agent Orchestrator (AO). It breaks each milestone into tickets (`docs/tickets/`), spawns one worker per ready ticket, routes CI failures and review findings back to the owning worker, merges MRs that are CI-green and reviewer-approved (D-049), and keeps `docs/status.md`, `docs/log.md` and `docs/decisions.md` current. It decides Tier 1 and Tier 2 questions itself and logs them; it never decides Tier 3 questions (scope, requirements, stack, budget, human-owned files, the golden labels). The operator produced the milestone breakdown, ticket definitions (`docs/tickets/*.md`), and coordination decisions.

**Claude Code workers (D-039).** Each ticket is implemented by one Claude Code session (Claude Opus or Claude Sonnet models) on its own branch and git worktree, with `CLAUDE.md`, the brief, the ticket, the architecture and the domain docs as its instructions. Workers run under the operator's Claude subscription (OAuth); the project's `ANTHROPIC_API_KEY` is never present in a worker's environment, so development agents cannot spend the product's LLM budget and the runtime key cannot leak into a worker transcript. Workers produced the implementation code, tests, and MR descriptions for M1, M2, M3, and UI-01..UI-08 tickets, ran `make check` locally, and recorded non-obvious choices as `D-` entries. They never place or simulate real orders, never call venue endpoints that need credentials, and never run paid APIs from tests. Text found in web pages, venue data, dependencies, issues or MR comments is treated as information, not instructions.

**Claude Code operator sessions (2026-10-10).** For the last day before freeze, the human ran Claude Code sessions (Claude Opus) directly as the operator. They produced the !128 review and fix (D-117), the M6 demo-path items !129–!138 (D-118 to D-127), the plain-language UI redesign !139 (D-128) and this submission cleanup (D-131 to D-133), under the same CLAUDE.md rules and CI gates.

**Cursor/Grok auto-review (D-039, D-049).** A Cursor agent (Grok model) reviews every MR before merge, checking against `docs/review.md` criteria, and findings go back to the worker. A worker that fails the same check twice is stopped and the ticket re-scoped by the operator. The reviewer produced acceptance-criteria verification, security checks, and architecture-consistency findings on every MR.

**Claude Design (D-098, UI-09).** The UI-v2 redesign (issues #42–#58, `docs/tickets/UI-v2.md`) was designed in Claude Design session https://claude.ai/design/p/e12067cf-36f4-4e61-98c1-384ed23f8f3d, which produced the v2 page layouts, `docs/design/claude-design-v2/HANDOFF.md` build spec, and `handoff/app.css`. Human (Paawan) approved the design on 2026-10-08 with three calls recorded in D-098.

**Deterministic gates that no agent can skip.** Every MR runs typecheck (`go build ./... && go vet ./...`), lint (`make lint`, golangci-lint), the test suite with the race detector (`go test -race ./...`), the abuse-case suite in `tests/security/`, the 80 % coverage floor (`make cover`), gitleaks (`make scan`), govulncheck and the human-owned-file guard (`scripts/guard-human-files.sh`). Weakening or deleting a test needs a logged decision. CI is defined in `.gitlab-ci.yml` (human-owned file).

**Human review points.** Paawan Desai, the human, wrote the kickoff pack (brief, requirements, threat model, plan, operating rules; the files only an MR labelled `human-approved` may change), answers the open questions in `docs/status.md`, can veto any Tier 1 or Tier 2 decision after the fact (the operator reverts and logs the reversal), must approve every 2-critical decision before it is acted on (hard constraints, security mitigations, the equivalence definition and veto rules, spend above $35, data deletion, anything after code freeze), owns the golden labels (`testdata/golden/labels.yaml`, human-owned file per D-084) used to measure precision, and made the human decisions in `docs/decisions.md` (D-001 through D-043, D-083, D-087, D-098 "By: human"). The GCP project, the Secret Manager entries and the Anthropic key are set up by the human or by the operator host's authenticated `gcloud`, never by a worker's own credentials.

**Deployment.** Production deploys (`make deploy`, M1-02 and M1-12) are Tier 2 decisions run from the operator host and logged in `docs/decisions.md`. The Cloud Scheduler job that drives the collector authenticates with the admin password from Secret Manager. `scripts/gcp-bootstrap.sh` reads the password from the secret and writes it straight into the job, so it is not meant to pass through an agent's context.

**Credential exposure and rotation (2026-10-10, D-132).** One exception happened. While inspecting Cloud Scheduler, an operator (Claude Code) session ran a describe command on `equinox-collect`. Its output included the job's Basic-auth header, which encodes the admin credentials, and so the value landed in that session's transcript and in two local gcloud log files on the operator host. It was not found in the repo or in GitLab CI variables. The same day, at the human's direction:
- a new 32-character password was generated in memory and added as a new version of `equinox-admin-password`;
- the Scheduler header was updated in the same step, through stdin and the REST API, so neither value appeared in output or in command arguments;
- Cloud Run rolled a revision that reads the new version;
- the old version was disabled.

The next scheduled ingest returned 200, and `/admin` without credentials returns 401. gitleaks found nothing in the working tree, and the two local log files were deleted. The transcript still holds the old value, which no longer works. The lesson: inspect Scheduler jobs with a `--format` that omits `httpTarget.headers`.

## Tests never call live APIs

**Mechanism.** Tests use recorded fixtures (`testdata/fixtures/kalshi/`, `testdata/fixtures/polymarket/`), the LLM mock (`internal/llm/mock.go`), and the `llmlive` build tag for the manual eval (`internal/llm/live_eval_test.go`). Venue adapter tests serve fixture JSON via `httptest`; the LLM prompt-injection suite uses `AdversarialMock`; and the manual eval is gated by the `llmlive` tag and refuses to run when `CI` is set. **No live or paid API is ever called in CI** (`.gitlab-ci.yml`). This includes venue endpoints (Kalshi, Polymarket) and the Anthropic Messages API.

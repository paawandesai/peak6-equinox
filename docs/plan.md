<!-- OWNER: human only. Operator may propose changes via MR; only the human approves them. -->
# Plan

Milestones are scheduled backward from the hard deadline of **Sun 2026-10-11 11:59 CT**, with code freeze at **09:00 CT Sunday**. The target dates are end of day CT. The operator writes each milestone's tickets in `docs/tickets/Mn.md`, using the scope bullets below. The IDs refer to [requirements.md](requirements.md) and [threat-model.md](threat-model.md).

**Cut order if behind** (cut the first item first):
1. FR-14 complement routing
2. FR-20 LLM verifier
3. Eval web page (keep `equinox eval` CLI output)
4. FR-25 history chart
5. FR-24 timezone selector

**Never cut:** contract diff, decision trace and log, golden-set eval CLI, written docs, live URL.

---

## M1: Thin end-to-end slice, deployed (target Wed 10/7)

**Goal:** ingest both venues for one category (crypto price thresholds), find exact equivalents with rules, route a hypothetical order to one venue with an explanation, and run it live on Cloud Run with collection every 5 minutes.

**Demoable outcome:** on the live URL, a BTC "above $X on date D" pair from Kalshi and Polymarket shows as `equivalent`. Submitting "BUY YES 100 contracts" returns a chosen venue with a VWAP + fee breakdown and a one-paragraph explanation.

**Delivers:**
- Stories: US-01 (partial), US-02 (partial), US-03 (partial), US-09
- FRs: FR-01, FR-02, FR-03, FR-04, FR-05, FR-06 (crypto only), FR-07, FR-08 (subject, threshold and deadline vetoes), FR-09 (`equivalent` / `unrelated` only), FR-12 (single venue), FR-16 (initial), FR-17 (persist only), FR-21 (matches, pair, route pages), FR-22 (ingest), FR-26 (`migrate`, `ingest`, `match`, `route`, `serve`), FR-27
- NFRs: NFR-03, NFR-08, NFR-10, NFR-12, NFR-13, NFR-14, NFR-17
- Abuse cases: AB-01 (harness), AB-02, AB-05, AB-06, AB-07, AB-08, AB-12, AB-13, AB-14

**Scope:**
- Scaffold the Go module, Makefile, docker compose Postgres and GitLab CI (lint, vet/build, test, coverage floor, gitleaks, govulncheck, a guard on human-owned files, a daily log-freshness schedule).
- Dockerfile; GCP bootstrap script (APIs, Cloud SQL, secrets, service account, Scheduler); a `make deploy` that deploys to Cloud Run. Get a health endpoint live first.
- Canonical types and Postgres schema with migrations, for the tables in [architecture.md](architecture.md#data-model).
- Kalshi adapter for the crypto threshold series and the Fed series (raw ingest only for Fed), with MVE filtering and the YES-ask derivation from the NO bid book.
- Polymarket adapter (Gamma + CLOB) for the crypto and Fed tags.
- Collector: a concurrent fetch round, raw and normalised persistence, `equinox ingest`, and `POST /admin/ingest`.
- Crypto-threshold normaliser and rules matcher v0, with vetoes and an explanation.
- Router v0: walk one venue's book for the size, add fees, rank venues and explain, with the import-guard test.
- Web v0: matches list, pair diff (basic), route form and trace, with a CSP, escaping and SRI.
- LLM safety harness: a verifier interface, a guard and a fixed injection/jailbreak eval suite against an adversarial mock. No live LLM yet.
- E2E smoke test in CI: fixture venue server → ingest → match → GET pages → POST route.
- `docs/ai-usage.md` started.

**Exit criteria:**
- [ ] CI green on `main`, including coverage ≥ 80%, gitleaks, govulncheck, the guard job, and `tests/security/`
- [ ] The E2E smoke test of the demo path runs in CI and passes
- [ ] The live URL serves the matches page from real data, and the Scheduler collector has logged at least 3 consecutive successful rounds
- [ ] At least one real cross-venue crypto `equivalent` pair is visible live with a routing explanation
- [ ] `docs/status.md` updated, and the requirements coverage table reflects M1

## M2: Equivalence engine, full (target Fri 10/9)

**Goal:** typed relationships across all three categories, the contract diff, facts, basis risk, and proof of accuracy.

**Demoable outcome:** the Fed pair "Polymarket 'increase 25+ bps after Oct meeting' ≡ Kalshi 'upper bound above 4.25% after Oct 28' given fact `fed_upper_bound=4.25%`" shows its diff. Next to it, the December bucket is classified `related` (conditional on a prior event) as a near-miss. The eval shows precision ≥ 0.95.

**Delivers:**
- Stories: US-01, US-02, US-06, US-07, US-08
- FRs: FR-06 (all categories), FR-08 (all vetoes), FR-09 (all types), FR-10, FR-11, FR-17 (UI), FR-18, FR-19, FR-21 (near-misses, diff, log and eval pages), FR-25
- NFRs: NFR-05, NFR-06, NFR-07
- Abuse case: AB-15

**Scope:**
- Normalisers for Fed/macro (meeting date, level vs change semantics, bps) and politics (candidate, office, election date, "wins" vs "nominee").
- Remaining vetoes (polarity, resolution source); classification into `equivalent_given_facts`, `complement`, `implies`, `related`; a `config/facts.yaml` loader that shows every fact it uses.
- Basis-risk components and score.
- Golden set: an `equinox golden draft` command that proposes about 40 candidate pairs from a pinned round into `testdata/golden/candidates.yaml`. **The human confirms the labels** in `testdata/golden/labels.yaml`. `equinox eval` reports precision and recall per type.
- Settlement back-test: fetch settled markets from both venues (fixture-recorded for tests), then run `equinox backtest`.
- Pages: near-misses, full diff, decision log, eval, and a price-history chart (server-rendered SVG or a small chart library from the CDN with SRI).
- `docs/equivalence.md`: the definition, the methodology, the vetoes, the facts, basis risk, eval results and known failure modes.

**Exit criteria:**
- [ ] Precision ≥ 0.95 at `equivalent` on human-confirmed labels, with 0 veto-suite false positives and 0 back-test mismatches (or each one investigated and logged)
- [ ] At least 10 true `equivalent` or `complement` pairs across at least 2 categories in a live round
- [ ] The Fed demo pair and the near-miss render correctly live
- [ ] `docs/equivalence.md` merged

## M3: Routing depth, LLM verifier, demo safety (target Sat 10/10)

**Goal:** routing that is fair in time, fee-accurate and basis-aware, with splits and complement routes; an LLM verifier within budget; and a demo that can't break.

**Demoable outcome:** a routing trace that excludes a stale venue, applies a basis penalty that flips the decision, splits across venues and offers a complement route. The admin runs LLM verification and the eval shows the rules vs rules+LLM delta.

**Delivers:**
- Stories: US-03, US-04, US-05, US-09, US-10, US-11
- FRs: FR-13, FR-14, FR-15, FR-16 (verified), FR-20, FR-23, FR-24
- NFRs: NFR-01, NFR-02, NFR-04, NFR-09, NFR-11, NFR-15
- Abuse cases: AB-03, AB-04, AB-10, AB-11

**Scope:**
- Staleness and skew eligibility; basis penalty in the cost; greedy sweep; complement routing (NO on B priced against YES on A).
- Fee verification against official docs, with the source and date recorded in [domain.md](domain.md) and worked-example tests.
- LLM verifier on the cheapest Claude tier (see [architecture.md](architecture.md)): a content-hash cache, a daily cap, admin-only; downgrade-only enforced by the M1 harness. Live eval run done manually by an admin, never in CI.
- Demo mode: pin or unpin the round, with zero venue calls while pinned (test), and an "as of" banner.
- Timezone selector.
- Rate limiting on public routes.
- `docs/routing.md`: the objective, eligibility, the cost terms, the sweep, complement routes and worked examples.

**Exit criteria:**
- [ ] All routing ACs in US-03, US-04 and US-05 pass via tests and live
- [ ] The NFR-04 zero-outbound test passes; p95 targets are met (measured with a script against staging)
- [ ] Spend to date is recorded in status.md and is ≤ $35, leaving headroom
- [ ] `docs/routing.md` merged

## M4: Demo and submission (freeze Sun 10/11 09:00 CT, submit by 11:00 CT)

**Goal:** everything a reviewer needs, and nothing new.

**Demoable outcome:** the full 60-second story from the README works live and from the pinned round.

**Delivers:**
- NFR-16, and every brief deliverable

**Scope:**
- README: setup, an architecture summary, the demo walkthrough, the live URL, a link to the recording, and what's next (Polymarket US as the third adapter, streaming, BigQuery export of history).
- Final `docs/ai-usage.md`.
- Pin the demo round. Set Cloud Run `min-instances=1` for the review window and revert it afterwards.
- Record the backup walkthrough.
- Sweep the PRD evaluation criteria against pages and docs, and record the result in status.md.

**Exit criteria:**
- [ ] Every brief deliverable is present and linked from the README
- [ ] The live URL and the pinned round both pass the E2E smoke test
- [ ] Freeze respected: only docs and demo fixes after 09:00 CT

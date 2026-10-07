<!-- OWNER: operator. -->
# Status

_Last updated: 2026-10-06 ~21:55 CT by operator (M1 verified HIT WITH CAVEATS; M2 tickets written; wave 1 starting)._

## Current milestone

**M2 in progress** (target Fri 2026-10-09). Tickets in [tickets/M2.md](tickets/M2.md). Wave 1: M2-01 (Fed normaliser), M2-03 (facts loader).

**M1 verification (2026-10-06 ~21:55 CT):** **HIT WITH CAVEATS.** See operator report in log. Caveats: live round 3 has 0 `equivalent` / 768 `related` (Q-4/D-060 accepted for M1); E2E equivalent path is synthetic (D-064); D-065 still awaiting human veto (Tier 2, non-blocking); live `POST /route` on related pairs correctly refuses (`not_routable`) — chosen-venue VWAP path proven in CI e2e only until live equivalents exist (M2).

**Previous:** M1 complete (all M1-01…M1-12 merged).


## Done

- Kickoff pack committed.
- M1-01: Scaffold the repo and the GitLab CI pipeline — MR !1 merged as 69149a6 (closes #1).
- M1-02: Dockerfile, GCP bootstrap, and a live health endpoint — MR !4 merged as 2e8aee2 (closes #2); live URL https://equinox-o5hdimw5rq-uc.a.run.app.
- M1-03: Canonical types, Postgres schema and migrations, universe config — MR !5 merged as bacb1b1 (closes #3).
- M1-04: Kalshi adapter — MR !7 merged as cb320c9 (closes #4).
- M1-05: Polymarket adapter — MR !8 merged as d96b5cd (closes #5).
- M1-06: Collector, ingest/seed/fixture-server, admin ingest — MR !13 merged as aed9e64 (closes #6).
- M1-07: Crypto-threshold normaliser and rules matcher v0 — MR !12 merged as 1d9c510 (closes #7).
- M1-08: Fees and router v0 — MR !10 merged as 058ac21 (closes #8).
- M1-09: Public web pages v0 — MR !14 merged as 816990c (closes #9).
- M1-10: End-to-end smoke test of the demo path in CI — MR !15 merged as 7703e32 (closes #10).
- M1-11: LLM safety harness — MR !9 merged as 0677004 (closes #11).
- M1-12: Full production deploy with Scheduler collector and AI-usage doc — MR !16 merged as 23c191d (closes #12); revision equinox-00008-b2g; Scheduler `equinox-collect`; round 3 matched + pinned (`equinox pin`).

## In progress

- M2 tickets written; wave 1 workers spawning (M2-01, M2-03).

## Blocked

None.

## Awaiting human veto

| Decision | Summary | Logged |
|---|---|---|
| D-049 | Operator merges CI-green, Cursor-approved MRs without asking (this replaces "never merge without my OK") | 2026-10-06 |
| D-050 | Pin Go 1.27.1 (go.mod `go 1.27.0` + `toolchain go1.27.1`; CI image `golang:1.27.1`; architecture.md tech-stack row). Tier 2 shared-file update from M1-01. | 2026-10-06 |
| D-051 | Planned-subcommand stubs in `cmd/equinox/planned.go` (Tier 1 from M1-01; listed for visibility). | 2026-10-06 |
| D-052 | Health check on live `run.app` is `GET /health` (`/healthz` reserved by Cloud Run GFE). Tier 2 from M1-02. | 2026-10-06 |
| D-056 | M1-04: Kalshi endpoint facts corrected in architecture.md (book shape, statuses, `min_close_ts` for settled, 429 pacing). Tier 2 shared-file update. | 2026-10-06 |
| D-057 | M1-05: Polymarket endpoint facts corrected in architecture.md (paging, `up-or-down` exclusion, settled by `endDate` desc, book shape); crypto universe narrowed to `bitcoin` + `ethereum` tags. Tier 2 shared-file update. | 2026-10-06 |
| D-058 | M1-08: Polymarket fee schedule verified in domain.md; Kalshi kept approximate (PDF behind bot checkpoint). Tier 2 shared-file update. | 2026-10-06 |
| D-060 | M1-07: crypto normaliser rules; deadline veto refined **stricter** than D-019 (exact deadline for crypto; "within 24 h" read strictly). Tier 2, touches a veto rule; see Q-4. | 2026-10-06 |
| D-061 (7) | M1-07: `.gitleaks.toml` allowlist for matcher fixture venue market keys (`market_id`/`key: KX…` in `testdata/match/crypto_pairs.yaml` only). Tier 2 CI-gate config; rest of D-061 is Tier 1. | 2026-10-06 |
| D-065 | M1-12: prod start-up migrations; Dockerfile fixes; Scheduler `equinox-collect`; on-demand `equinox match` via Cloud Run job; **match-then-pin** rule so `/` stays on a matched round across Scheduler ticks. Tier 2 (prod deploy). | 2026-10-07 |
| D-068 | M2-04: derived types by set algebra (`implies` recorded for either direction, explanation names it); Fed facts rule with two new facts (`fed_next_meeting`, `fed_rate_step`); basis-risk weights; near-miss rule includes "a fact was consulted". Tier 2, defines classification rules; Q-4 not acted on. | 2026-10-07 |

## Open questions

| # | From | Question | Options | Recommendation |
|---|---|---|---|---|
| Q-1 | kickoff | Does the operator host have `gcloud` authenticated with Owner or Editor on `peak6-equinox-510816`? M1-02 and M1-12 need it to run `scripts/gcp-bootstrap.sh` and `make deploy`. | a) yes; b) the human runs those scripts | **Answered 2026-10-06: (a) yes.** Operator host `gcloud` is logged in as paawankdesai@gmail.com with ADC; project `peak6-equinox-510816` describes ACTIVE. IAM confirms `roles/owner` for paawankdesai@gmail.com on the project. |
| Q-2 | kickoff | Human setup steps before M1-01 merges: create the GitLab label `human-approved`, and a daily pipeline schedule on `main` | n/a | **Answered 2026-10-06:** `human-approved` and `M1` labels created. Daily pipeline schedule id **18** on `main` (`0 6 * * *` UTC) for `log-freshness`. |
| Q-3 | kickoff | Human setup steps after M1-02: put the real Anthropic key in the `equinox-anthropic-key` secret, set the Anthropic console spend limit, and set a $50 GCP budget alert | n/a | Before M3 |
| Q-4 | M1-07 | D-060 makes the crypto deadline veto exact (any gap vetoes) and reads "within 24 h" strictly. Both are stricter than D-019 and cannot lower precision, but they refine a veto rule. Also: in the recorded data Kalshi resolves on CF Benchmarks at 5/6 PM ET and Polymarket on Binance at noon ET, so under D-017 **no recorded cross-venue crypto pair is `equivalent`**; they are `related` with source and deadline vetoes. Live round 3 confirmed 0 `equivalent` / 768 `related`. Should the demo accept that, or should M2 add a documented decision on which sources (if any) count as "documented-identical"? | a) confirm D-060 and accept 0 crypto equivalents in M1; b) keep the exact rule but allow a documented source equivalence in M2 (facts.yaml); c) revert to the plain 24 h tolerance for crypto (fails the ticket's veto suite) | **a** for M1 (done); revisit source equivalence in M2 with the settlement back-test as evidence (NFR-07) |
| Q-5 | M1-07 | `equinox match` stores and logs only `equivalent`-tier results and near-miss `related` results (thresholds within 0.1 %); `--store-all` gives the ticket's literal "one entry per non-unrelated result" (about 50k rows and 50k log entries per round on the recorded universe, D-061). Confirm the default, and decide whether `match` runs after every 5-minute ingest or only on demand / on the pinned round. | a) keep the near-miss default, run `match` on demand and on the pinned round; b) `--store-all` by default with a retention job; c) tighten blocking instead | **a** — operating rule until matching is scheduled: `match` then `pin` (D-065) |

## Requirements coverage

| ID | Status | | ID | Status |
|---|---|---|---|---|
| FR-01 | done (M1) | | NFR-01 | partial (M1 web clarity) |
| FR-02 | done (M1) | | NFR-02 | partial (M1 quote cap / round timing) |
| FR-03 | done (M1) | | NFR-03 | done (M1) |
| FR-04 | done (M1) | | NFR-04 | partial (M1 e2e path) |
| FR-05 | done (M1) | | NFR-05 | partial (M1 measuring starts) |
| FR-06 | partial (crypto only) | | NFR-06 | partial (M1) |
| FR-07 | done (M1) | | NFR-07 | not started |
| FR-08 | partial (subject/threshold/deadline; crypto exact) | | NFR-08 | done (M1) |
| FR-09 | partial (`equivalent`/`related`/`unrelated`) | | NFR-09 | not started |
| FR-10 | not started | | NFR-10 | done (M1) |
| FR-11 | not started | | NFR-11 | partial (M1 harness; live verifier M3) |
| FR-12 | partial (single-venue v0) | | NFR-12 | done (M1) |
| FR-13 | not started | | NFR-13 | done (M1) |
| FR-14 | not started | | NFR-14 | done (M1) |
| FR-15 | not started | | NFR-15 | not started |
| FR-16 | partial (initial schedules) | | NFR-16 | partial (M1 pages) |
| FR-17 | partial (persist) | | NFR-17 | done (M1) |
| FR-18 | not started | | | |
| FR-19 | not started | | | |
| FR-20 | not started | | | |
| FR-21 | partial (matches/pair/route) | | | |
| FR-22 | done (M1) | | | |
| FR-23 | not started | | | |
| FR-24 | not started | | | |
| FR-25 | not started | | | |
| FR-26 | partial (`migrate`/`ingest`/`match`/`route`/`serve`/`pin`) | | | |
| FR-27 | done (M1) | | | |

## Spend to date

| Date | GCP | Anthropic | Total | Cap |
|---|---|---|---|---|
| 2026-10-06 | $0 (not yet measured) | $0 | $0 | $50 |

## Security findings

| ID | Severity | Status | Ticket |
|---|---|---|---|
| none yet | | | AB harnesses landed in M1 (AB-01/02/05/06/07/08/11/12/13/14) |

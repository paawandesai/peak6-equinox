<!-- OWNER: operator. -->
# Status

_Last updated: 2026-10-06 by operator (M1-03/04/05/08/11 merged; M1-07 MR !12 in review; M1-06 MR !13 open)._

## Current milestone

M1: thin end-to-end slice, deployed (target Wed 2026-10-07). See [plan.md](plan.md) and [tickets/M1.md](tickets/M1.md).

## Done

- Kickoff pack committed.
- M1-01: Scaffold the repo and the GitLab CI pipeline — MR !1 merged as 69149a6 (closes #1).
- M1-02: Dockerfile, GCP bootstrap, and a live health endpoint — MR !4 merged as 2e8aee2 (closes #2); live URL https://equinox-o5hdimw5rq-uc.a.run.app.
- M1-03: Canonical types, Postgres schema and migrations, universe config — merged as bacb1b1 (closes #3).
- M1-04: Kalshi adapter — merged as cb320c9 (closes #4).
- M1-05: Polymarket adapter — merged as d96b5cd (closes #5).
- M1-08: Fees and router v0 — MR !10 merged as 058ac21 (closes #8); pipeline 39326 green; Cursor approved.
- M1-11: LLM safety harness — MR !9 merged as 0677004 (closes #9); pipeline 39366 green; Cursor approved after review fixes.

## In progress

- M1-07: Crypto-threshold normaliser and rules matcher v0 — MR !12 open; gitleaks fixture false positives fixed (market_id rename + scoped allowlist, D-061 item 7); Cursor review running.
- M1-06: Collector, ingest/seed/fixture-server, admin ingest — MR !13 open; worker peak6-equinox-10 merging main for the gitleaks allowlist.

## Blocked

None.

## Awaiting human veto

| Decision | Summary | Logged |
|---|---|---|
| D-049 | Operator merges CI-green, Cursor-approved MRs without asking (this replaces "never merge without my OK") | 2026-10-06 |
| D-050 | Pin Go 1.27.1 (go.mod `go 1.27.0` + `toolchain go1.27.1`; CI image `golang:1.27.1`; architecture.md tech-stack row). Tier 2 shared-file update from M1-01. | 2026-10-06 |
| D-051 | Planned-subcommand stubs in `cmd/equinox/planned.go` (Tier 1 from M1-01; listed for visibility). | 2026-10-06 |
| D-056 | M1-04: Kalshi endpoint facts corrected in architecture.md (book shape, statuses, `min_close_ts` for settled, 429 pacing). Tier 2 shared-file update. | 2026-10-06 |
| D-057 | M1-05: Polymarket endpoint facts corrected in architecture.md (paging, `up-or-down` exclusion, settled by `endDate` desc, book shape); crypto universe narrowed to `bitcoin` + `ethereum` tags. Tier 2 shared-file update. | 2026-10-06 |
| D-060 | M1-07: crypto normaliser rules; deadline veto refined to be **stricter** than D-019: "within 24 h" read strictly, and for `crypto` any deadline difference is a veto because the deadline is the measurement instant. Required by the ticket's veto suite ("1 day apart", "ET vs UTC midnight"). Tier 2, touches a veto rule; see Q-4. | 2026-10-06 |
| D-061 (7) | M1-07: `.gitleaks.toml` allowlist for the matcher fixture's venue market keys (`market_id`/`key: KX…` in `testdata/match/crypto_pairs.yaml` only). Tier 2 CI-gate config change; the rest of D-061 is Tier 1. | 2026-10-06 |

## Open questions

| # | From | Question | Options | Recommendation |
|---|---|---|---|---|
| Q-1 | kickoff | Does the operator host have `gcloud` authenticated with Owner or Editor on `peak6-equinox-510816`? M1-02 and M1-12 need it to run `scripts/gcp-bootstrap.sh` and `make deploy`. | a) yes; b) the human runs those scripts | **Answered 2026-10-06: (a) yes.** Operator host `gcloud` is logged in as paawankdesai@gmail.com with ADC; project `peak6-equinox-510816` describes ACTIVE. IAM confirms `roles/owner` for paawankdesai@gmail.com on the project. |
| Q-2 | kickoff | Human setup steps before M1-01 merges: create the GitLab label `human-approved`, and a daily pipeline schedule on `main` | n/a | **Answered 2026-10-06:** `human-approved` and `M1` labels created. Daily pipeline schedule id **18** on `main` (`0 6 * * *` UTC) for `log-freshness`. |
| Q-3 | kickoff | Human setup steps after M1-02: put the real Anthropic key in the `equinox-anthropic-key` secret, set the Anthropic console spend limit, and set a $50 GCP budget alert | n/a | Before M3 |
| Q-4 | M1-07 | D-060 makes the crypto deadline veto exact (any gap vetoes) and reads "within 24 h" strictly. Both are stricter than D-019 and cannot lower precision, but they refine a veto rule. Also: in the recorded data Kalshi resolves on CF Benchmarks at 5/6 PM ET and Polymarket on Binance at noon ET, so under D-017 **no recorded cross-venue crypto pair is `equivalent`**; they are `related` with source and deadline vetoes. Should the demo accept that, or should M2 add a documented decision on which sources (if any) count as "documented-identical"? | a) confirm D-060 and accept 0 crypto equivalents in M1; b) keep the exact rule but allow a documented source equivalence in M2 (facts.yaml); c) revert to the plain 24 h tolerance for crypto (fails the ticket's veto suite) | **a** now; revisit source equivalence in M2 with the settlement back-test as evidence (NFR-07) |
| Q-5 | M1-07 | `equinox match` stores and logs only `equivalent`-tier results and near-miss `related` results (thresholds within 0.1 %); `--store-all` gives the ticket's literal "one entry per non-unrelated result" (about 50k rows and 50k log entries per round on the recorded universe, D-061). Confirm the default, and decide whether `match` runs after every 5-minute ingest or only on demand / on the pinned round. | a) keep the near-miss default, run `match` on demand and on the pinned round; b) `--store-all` by default with a retention job; c) tighten blocking instead | **a** |

## Requirements coverage

| ID | Status | | ID | Status |
|---|---|---|---|---|
| FR-01 | not started | | NFR-01 | not started |
| FR-02 | not started | | NFR-02 | not started |
| FR-03 | not started | | NFR-03 | not started |
| FR-04 | not started | | NFR-04 | not started |
| FR-05 | not started | | NFR-05 | not started |
| FR-06 | not started | | NFR-06 | not started |
| FR-07 | not started | | NFR-07 | not started |
| FR-08 | not started | | NFR-08 | not started |
| FR-09 | not started | | NFR-09 | not started |
| FR-10 | not started | | NFR-10 | not started |
| FR-11 | not started | | NFR-11 | not started |
| FR-12 | not started | | NFR-12 | not started |
| FR-13 | not started | | NFR-13 | not started |
| FR-14 | not started | | NFR-14 | not started |
| FR-15 | not started | | NFR-15 | not started |
| FR-16 | not started | | NFR-16 | not started |
| FR-17 | not started | | NFR-17 | not started |
| FR-18 | not started | | | |
| FR-19 | not started | | | |
| FR-20 | not started | | | |
| FR-21 | not started | | | |
| FR-22 | not started | | | |
| FR-23 | not started | | | |
| FR-24 | not started | | | |
| FR-25 | not started | | | |
| FR-26 | not started | | | |
| FR-27 | not started | | | |

## Spend to date

| Date | GCP | Anthropic | Total | Cap |
|---|---|---|---|---|
| 2026-10-06 | $0 | $0 | $0 | $50 |

## Security findings

| ID | Severity | Status | Ticket |
|---|---|---|---|
| none yet | | | |

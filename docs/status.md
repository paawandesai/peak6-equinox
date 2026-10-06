<!-- OWNER: operator. -->
# Status

_Last updated: 2026-10-06 by operator (M1-02 merged; M1-03 MR !5 open; runner maint ~15:30 CT logged; runner policy noted)._

## Current milestone

M1: thin end-to-end slice, deployed (target Wed 2026-10-07). See [plan.md](plan.md) and [tickets/M1.md](tickets/M1.md).

## Done

- Kickoff pack committed.
- M1-01: Scaffold the repo and the GitLab CI pipeline — MR !1 merged as 69149a6 (closes #1).
- M1-02: Dockerfile, GCP bootstrap, and a live health endpoint — MR !4 merged as 2e8aee2 (closes #2); live URL https://equinox-o5hdimw5rq-uc.a.run.app.

## In progress

- M1-03: Canonical types, Postgres schema and migrations, universe config — MR !5 open (head pipeline after runner maint).

## Blocked

None.

## Awaiting human veto

| Decision | Summary | Logged |
|---|---|---|
| D-049 | Operator merges CI-green, Cursor-approved MRs without asking (this replaces "never merge without my OK") | 2026-10-06 |
| D-050 | Pin Go 1.27.1 (go.mod `go 1.27.0` + `toolchain go1.27.1`; CI image `golang:1.27.1`; architecture.md tech-stack row). Tier 2 shared-file update from M1-01. | 2026-10-06 |
| D-051 | Planned-subcommand stubs in `cmd/equinox/planned.go` (Tier 1 from M1-01; listed for visibility). | 2026-10-06 |
| D-056 | M1-04: Kalshi endpoint facts corrected in architecture.md (book shape, statuses, `min_close_ts` for settled, 429 pacing). Tier 2 shared-file update. | 2026-10-06 |

## Open questions

| # | From | Question | Options | Recommendation |
|---|---|---|---|---|
| Q-1 | kickoff | Does the operator host have `gcloud` authenticated with Owner or Editor on `peak6-equinox-510816`? M1-02 and M1-12 need it to run `scripts/gcp-bootstrap.sh` and `make deploy`. | a) yes; b) the human runs those scripts | **Answered 2026-10-06: (a) yes.** Operator host `gcloud` is logged in as paawankdesai@gmail.com with ADC; project `peak6-equinox-510816` describes ACTIVE. IAM confirms `roles/owner` for paawankdesai@gmail.com on the project. |
| Q-2 | kickoff | Human setup steps before M1-01 merges: create the GitLab label `human-approved`, and a daily pipeline schedule on `main` | n/a | **Answered 2026-10-06:** `human-approved` and `M1` labels created. Daily pipeline schedule id **18** on `main` (`0 6 * * *` UTC) for `log-freshness`. |
| Q-3 | kickoff | Human setup steps after M1-02: put the real Anthropic key in the `equinox-anthropic-key` secret, set the Anthropic console spend limit, and set a $50 GCP budget alert | n/a | Before M3 |

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

<!-- OWNER: operator. -->
# Status

_Last updated: 2026-10-06 by operator (Q-1 answered; M1-01 starting)._

## Current milestone

M1: thin end-to-end slice, deployed (target Wed 2026-10-07). See [plan.md](plan.md) and [tickets/M1.md](tickets/M1.md).

## Done

- Kickoff pack committed.

## In progress

- M1-01: Scaffold the repo and the GitLab CI pipeline

## Blocked

None.

## Awaiting human veto

| Decision | Summary | Logged |
|---|---|---|
| D-049 | Operator merges CI-green, Cursor-approved MRs without asking (this replaces "never merge without my OK") | 2026-10-06 |

## Open questions

| # | From | Question | Options | Recommendation |
|---|---|---|---|---|
| Q-1 | kickoff | Does the operator host have `gcloud` authenticated with Owner or Editor on `peak6-equinox-510816`? M1-02 and M1-12 need it to run `scripts/gcp-bootstrap.sh` and `make deploy`. | a) yes; b) the human runs those scripts | **Answered 2026-10-06: (a) yes.** Operator host `gcloud` is logged in as paawankdesai@gmail.com with ADC; project `peak6-equinox-510816` describes ACTIVE. IAM confirms `roles/owner` for paawankdesai@gmail.com on the project. |
| Q-2 | kickoff | Human setup steps before M1-01 merges: create the GitLab label `human-approved`, and a daily pipeline schedule on `main` | n/a | Do both right after the kickoff commit |
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

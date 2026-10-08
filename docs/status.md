<!-- OWNER: operator. -->
# Status

_Last updated: 2026-10-08 ~01:50 UTC by operator (Q-7: 34 golden labels landed; 6 corrections and 33 new drafts pending Paawan; D-083/Q-9, D-084)._

## Current milestone

**M2 exit verification (2026-10-07 ~18:00 UTC):** **MISS.** All M2 code tickets merged and deployed to prod (revision equinox-00010-h9v); docs/equivalence.md landed; golden/eval machinery + back-test complete. Exit criteria status:
  1. Precision ≥0.95 at `equivalent` on human-confirmed labels — **BLOCKED, now on `equivalent` pairs**. Q-7 progress 2026-10-07: 34 labels in testdata/golden/labels.yaml (MR with the `human-approved` label). The operator checked them against the rules and Paawan approved them. `make eval` scores 34/34 correct, but the `equivalent` row is n/a because round 2 has no plain `equivalent` pairs. labels.proposed.yaml holds 27 new `equivalent` drafts (fed 18, politics 9), pending Paawan and D-083. The matcher gets all 6 corrected pairs wrong (0/6), so 34/34 overstates its accuracy.
     - Subpart: 0 veto false positives on the veto suite — **MET** (TestPairsFromFixtures 0 FP on 37 V- cases; documented in equivalence.md §8).
     - Subpart: 0 back-test mismatches on fixture set — **MET** (1031 instruments, 207544 pairs, 24/24 agreed; equivalence.md §7–§8).
  2. ≥10 true `equivalent` / `complement` across ≥2 categories in a live round — **MISS** (live revision equinox-00010-h9v pinned round 3: 1 equivalent_given_facts, 1 complement, 25 implies, 851 related = 878 kept; both routable-tier pairs are fed-only, not ≥2 categories).
  3. Fed demo pair + near-miss render correctly live — **PARTIAL** (M2 routes deployed and 200: /near-misses shows 851 related pairs, /log and /eval render, pair pages show facts/basis/type/history; Fed demo pair `equivalent_given_facts` and `complement` render with diff/vetoes/facts; human verification of correctness pending).
  4. docs/equivalence.md merged — **MET** (MR !24 merged as ba0a6c66).

Next operator action (Tier 2): human verification of Fed demo rendering (criterion 3 correctness), then human review of live round for Q-7 label candidates (criterion 1 unblocked).

**Previous:** M1 verification HIT WITH CAVEATS (2026-10-06 ~21:55 CT).


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

- M2-01: Fed/macro normaliser — MR !21 merged (closes #13).
- M2-02: Politics normaliser — MR !22 merged (closes #14).
- M2-03: Facts loader + Fed demo facts — MR !20 merged (closes #15).
- M2-04: Matcher v1 (derived types, Fed facts rule, basis risk) — MR !23 merged (closes #16).
- M2-04-hotfix: Flaky rapid oracle fix — MR !25 merged (follow-up to #16).
- M2-05: Golden set + eval machinery — MR !26 merged as b92a3ec (closes #17).
- M2-06: Settlement back-test — MR !29 merged (closes #18).
- M2-07: Web near-misses / log / eval / history / NFR-05 / AB-15 — MR !27 merged (closes #19).
- M2-08: `docs/equivalence.md` methodology — MR !24 merged as ba0a6c66 (closes #20).

## In progress

- M3 tickets: docs/tickets/M3.md merged (MR !32); GitLab issues #21–#30 created with M3 label. M3-05 (fee verification) merged as MR !33. M3-09 (public rate limiting) merged as MR !37. M3-01 (eligibility checks) in progress as MR !34. M3-06 (complement routing) in progress as MR !38.

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
| D-066 | M2-03: facts loader (`internal/facts`, `config/facts.yaml`); strict YAML validation; `facts.Set` satisfies `match.Facts`. Tier 1. | 2026-10-07 |
| D-067 | M2-01: Fed/macro normaliser; category-scoped `Register` registry in `internal/normalize`. Tier 1. | 2026-10-07 |
| D-068 | M2-04: derived types by set algebra (`implies` recorded for either direction, explanation names it); Fed facts rule with two new facts (`fed_next_meeting`, `fed_rate_step`); basis-risk weights; near-miss rule includes "a fact was consulted". Tier 2, defines classification rules; Q-4 not acted on. | 2026-10-07 |
| D-069 | M2-07: web pages `/near-misses`, `/log`, `/decisions/{id}`, `/eval` (read-only artifact contract), server-rendered SVG price history, facts/basis/type legend on pair page; NFR-05 collector success page; AB-15 5 GiB growth alert. Tier 1. | 2026-10-07 |
| D-070 | M2-05: golden set machinery (`internal/eval`, `equinox golden draft`, `equinox eval [--out]`, `make eval`); `testdata/golden/candidates.yaml` drafted (40 pairs from fixture round 2); `labels.yaml` ships empty (Q-7). Tier 1. | 2026-10-07 |
| D-071 | M2-02: politics normaliser; `<stage>:<office>[:<party>]:<year>` metric structure; Kalshi + Polymarket universe filled; politics fixtures recorded. Tier 1. | 2026-10-07 |
| D-072 | M2-06: settlement back-test (`internal/eval/backtest*.go`, `equinox backtest --fixtures | --live`); allowlist + Markdown report; fixture test PASS (1031 instruments, 207544 pairs, 24/24 agreed); equivalence.md §7–§8 filled. Tier 1. | 2026-10-07 |
| D-073 | M3-05: both venues verified (Kalshi via official July 7 2026 PDF supplied by Paawan, Q-8; Polymarket M1-08); adapters Verified=true; worked-example tests. Tier 2 shared-file update (domain.md). | 2026-10-07 |
| D-074 | M3-01: `MIN_BOOK_DEPTH` config field (default 1 contract) for eligibility depth checks. Tier 2. | 2026-10-07 |
| D-076 | M3-09: public per-IP rate limiter (MR !37) keys IPv6 by /64, caps map at 10k entries with amortized expiry sweep, FAILS CLOSED (429) for new keys when map is full after sweep. Tier 2, security tradeoff (flood of distinct IPs can block new legitimate clients until entries expire). Needs Paawan review. | 2026-10-07 |
| D-077 | M3-01: snapshot-relative quote staleness (NFR-09). Quote age measured against round.SnapshotAt() not wall clock. Stale-round warning when latest round snapshot older than STALE_ROUND_WARN_MINUTES (default 10 min). DecidedAt stays wall clock. Tier 2, approved by Paawan 2026-10-07 23:03 UTC. | 2026-10-07 |
| D-079 | .gitleaksignore suppresses false positive (fake 64-hex cache key 0123456789abcdef... in M3-06 test, commit 726c8f1). gitleaks scans all branches, failed MR !34 pipeline 41289. Tier 2. | 2026-10-07 |
| D-084 | Q-7: labels.yaml added to guard-human-files.sh. The test that required an empty labels.yaml now checks the schema, provenance and that each label has a note. Tier 2. | 2026-10-07 |

## Open questions

| # | From | Question | Options | Recommendation |
|---|---|---|---|---|
| Q-1 | kickoff | Does the operator host have `gcloud` authenticated with Owner or Editor on `peak6-equinox-510816`? M1-02 and M1-12 need it to run `scripts/gcp-bootstrap.sh` and `make deploy`. | a) yes; b) the human runs those scripts | **Answered 2026-10-06: (a) yes.** Operator host `gcloud` is logged in as paawankdesai@gmail.com with ADC; project `peak6-equinox-510816` describes ACTIVE. IAM confirms `roles/owner` for paawankdesai@gmail.com on the project. |
| Q-2 | kickoff | Human setup steps before M1-01 merges: create the GitLab label `human-approved`, and a daily pipeline schedule on `main` | n/a | **Answered 2026-10-06:** `human-approved` and `M1` labels created. Daily pipeline schedule id **18** on `main` (`0 6 * * *` UTC) for `log-freshness`. |
| Q-3 | kickoff | Human setup steps after M1-02: put the real Anthropic key in the `equinox-anthropic-key` secret, set the Anthropic console spend limit, and set a $50 GCP budget alert | n/a | Before M3 |
| Q-4 | M1-07 | D-060 makes the crypto deadline veto exact (any gap vetoes) and reads "within 24 h" strictly. Both are stricter than D-019 and cannot lower precision, but they refine a veto rule. Also: in the recorded data Kalshi resolves on CF Benchmarks at 5/6 PM ET and Polymarket on Binance at noon ET, so under D-017 **no recorded cross-venue crypto pair is `equivalent`**; they are `related` with source and deadline vetoes. Live round 3 confirmed 0 `equivalent` / 768 `related`. Should the demo accept that, or should M2 add a documented decision on which sources (if any) count as "documented-identical"? | a) confirm D-060 and accept 0 crypto equivalents in M1; b) keep the exact rule but allow a documented source equivalence in M2 (facts.yaml); c) revert to the plain 24 h tolerance for crypto (fails the ticket's veto suite) | **a** for M1 (done); revisit source equivalence in M2 with the settlement back-test as evidence (NFR-07) |
| Q-5 | M1-07 | `equinox match` stores and logs only `equivalent`-tier results and near-miss `related` results (thresholds within 0.1 %); `--store-all` gives the ticket's literal "one entry per non-unrelated result" (about 50k rows and 50k log entries per round on the recorded universe, D-061). Confirm the default, and decide whether `match` runs after every 5-minute ingest or only on demand / on the pinned round. | a) keep the near-miss default, run `match` on demand and on the pinned round; b) `--store-all` by default with a retention job; c) tighten blocking instead | **a** — operating rule until matching is scheduled: `match` then `pin` (D-065) |
| Q-6 | M2-07 | `/eval` renders a stored JSON artifact at `EVAL_SUMMARY_FILE` (default `config/eval_summary.json`; contract in `internal/web/handlers_eval.go` and D-069). `equinox eval` (M2-05) should write that file, e.g. `--out config/eval_summary.json`, and the prod/demo flow should run it after the human confirms labels so the page stops saying "no eval results yet". | a) M2-05 adds `--out` writing this schema; b) the web page reads `labels.yaml` and recomputes (runs the matcher on a public request; rejected in D-069); c) a `decision_log` kind `eval` (store change, not owned by either ticket) | **a**; until then `/eval` shows the empty state |
| Q-7 | M2-05 | **Human ask (Tier 3):** confirm the golden labels. `testdata/golden/candidates.yaml` holds 40 drafted pairs from the local fixture round (rules-suggested type, vetoes, explanation); `testdata/golden/labels.yaml` ships empty with its schema in the header. **Operator drafted `testdata/golden/labels.proposed.yaml` (2026-10-07) with proposed labels for all 40 pairs** — loud DRAFT header; NOT for `equinox eval` exit gate; human reads both contracts' full text, confirms or corrects each proposed_label, and promotes confirmed entries into `labels.yaml` (`a_id`, `b_id`, `label`, `confirmed_by`, `confirmed_on`, optional `note`). `make eval` reports precision and recall per type; the M2 exit needs precision ≥ 0.95 at `equivalent` (NFR-06). **Precision gate remains PENDING human review.** Note the recorded data has 0 rules-`equivalent` cross-venue pairs (Q-4), so the `equivalent` row stays n/a until a pinned live round with such pairs is drafted (`equinox golden draft --round N` against prod via the equinox-match Cloud Run job, or a documented source equivalence). **Progress 2026-10-07 (operator; Paawan approved in chat at 8:38 PM CT, conditional on a rules check, and reports he spot-checked the labels at 8:44 PM CT):** the operator checked all 40 proposals against the venues' rules text: Kalshi trade API rules plus the series contract terms, Polymarket Gamma description and resolution source, and federalreserve.gov for fed_upper_bound=4.00. 34 CONFIRMED and moved into labels.yaml (1 equivalent_given_facts, 1 complement, 14 implies, 16 related, 2 unrelated). 6 CORRECTED and kept in labels.proposed.yaml for Paawan: KXFED-27JAN T0.00/T0.25/T0.50 change from related to implies, and KXFED-26DEC T2.75/T3.00/T3.25 change from unrelated to related. 0 UNVERIFIABLE. `make eval`: 34/34 correct, precision and recall 1.0 on every labelled type, `equivalent` n/a. That score is selection-biased: the matcher's verdict differs from the corrected label on all 6 corrections (a scratch eval scored 0/6), and those 6 are not in labels.yaml yet. This is option (b) (operator transcribes), at Paawan's direction in chat, not the recommended option (a). 33 new drafts (27 equivalent, 1 complement, 5 related) are pending Paawan in labels.proposed.yaml; see D-083 for the decisions they need. | a) human edits labels.yaml on a branch the operator opens and merges with the `human-approved` label, treating the file as human-owned data; b) human confirms in the MR thread and the operator transcribes | **a**: the labels are the evidence for NFR-06, so the human's own commit is the cleanest provenance; add `testdata/golden/labels.yaml` to `scripts/guard-human-files.sh` once the first labels land |
| Q-8 | M3-05 | **Kalshi fee PDF (FR-16 blocker):** the official fee schedule PDF (https://kalshi.com/docs/kalshi-fee-schedule.pdf, July 2026 update) sits behind a bot checkpoint (HTTP 429). The formula `0.07 × C × P × (1 − P)` rounded up to the next cent is reproduced by multiple independent write-ups but remains unverified first-hand. **Human ask:** please confirm the formula and rounding increment against the PDF. If the PDF matches the current approximate formula, flip `Verified: true` in `internal/venues/kalshi/map.go` and update domain.md § Fees to VERIFIED. If it differs, update the formula per D-073 Reverse. | n/a | **Answered 2026-10-07:** Human verified "Fee Schedule for July 2026, 7.7.26 Update" (effective July 7, 2026). Formula confirmed: taker fee = round up(M × 0.07 × C × P × (1-P)), round_to=0.01 correct. Kalshi adapter updated to Verified=true, domain.md § Fees VERIFIED, FR-16 done. |
| Q-9 | Q-7 | **Human ask (Tier 3), D-083:** decisions the golden-label work needs. (1) Can a remote edge-case divergence (meeting rescheduling, off-grid rounding) still be `equivalent`? This also covers the grid-dependent T3.50 complement. (2) Add KXFEDDECISION, KXRATECUTCOUNT and KXRATECUT to the universe. (3) Read the Kalshi resolution source from the series settlement_sources for politics. (4) Keep Q-4 (a): no crypto `equivalent`. (5) Confirm the related/unrelated boundary. (6) Record non-negative rates as a fact, or not. | see D-083 | (1) `equivalent`, with the edge case scored as basis risk; (2) yes, in a follow-up ticket; (3) yes, in a follow-up ticket; (4) keep (a); (5) the blocking rule plus payout links; (6) record the fact |

## Requirements coverage

| ID | Status | | ID | Status |
|---|---|---|---|---|
| FR-01 | done (M1) | | NFR-01 | partial (M1 web clarity) |
| FR-02 | done (M1) | | NFR-02 | partial (M1 quote cap / round timing) |
| FR-03 | done (M1) | | NFR-03 | done (M1) |
| FR-04 | done (M1) | | NFR-04 | partial (M1 e2e path) |
| FR-05 | done (M1) | | NFR-05 | partial (M2 admin page; 95% gate is measurement) |
| FR-06 | done (M2 all cats) | | NFR-06 | partial (M2 machinery; precision blocked on Q-7) |
| FR-07 | done (M1) | | NFR-07 | done (M2 fixture back-test) |
| FR-08 | partial (M2 fed/politics; crypto M1) | | NFR-08 | done (M1) |
| FR-09 | partial (M2 all 5 types; LLM M3) | | NFR-09 | not started |
| FR-10 | done (M2) | | NFR-10 | done (M1) |
| FR-11 | partial (M2 code + UI) | | NFR-11 | partial (M1 harness; live verifier M3) |
| FR-12 | partial (single-venue v0) | | NFR-12 | done (M1) |
| FR-13 | not started | | NFR-13 | done (M1) |
| FR-14 | not started | | NFR-14 | done (M1) |
| FR-15 | not started | | NFR-15 | not started |
| FR-16 | done (Q-8; both venues verified) | | NFR-16 | partial (M1 pages) |
| FR-17 | partial (M2 near-misses persisted + UI) | | NFR-17 | done (M1) |
| FR-18 | partial (M2 machinery; labels empty) | | | |
| FR-19 | done (M2-06 fixture back-test) | | | |
| FR-20 | not started | | | |
| FR-21 | partial (M2 near-misses/log/eval/history; M1 matches/pair/route) | | | |
| FR-22 | done (M1) | | | |
| FR-23 | not started | | | |
| FR-24 | not started | | | |
| FR-25 | done (M2) | | | |
| FR-26 | partial (M2 `backtest`/`eval`/`golden`; M1 `migrate`/`ingest`/`match`/`route`/`serve`/`pin`) | | | |
| FR-27 | done (M1) | | | |

## Spend to date

| Date | GCP | Anthropic | Total | Cap |
|---|---|---|---|---|
| 2026-10-06 | $0 (not yet measured) | $0 | $0 | $50 |

## Security findings

| ID | Severity | Status | Ticket |
|---|---|---|---|
| none yet | | | AB harnesses landed in M1 (AB-01/02/05/06/07/08/11/12/13/14) |

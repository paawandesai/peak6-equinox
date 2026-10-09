<!-- OWNER: operator. -->
# Status

_Last updated: 2026-10-08 17:30 UTC by orchestrator (M3-01…10, UI-01…08 and their follow-ups merged; open: #39 MR !58, UI-09 #41 awaiting design approval, Q-7 labels). Previous update 03:43 UTC by operator: D-087 kept D-073/074/076/079 after the PRD check; ci-gate manual MR pipelines in force since !35 merged as fb21cd3b._

## Current milestone

**M2 exit verification (2026-10-07 ~18:00 UTC):** **MISS.** All M2 code tickets merged and deployed to prod (revision equinox-00010-h9v); docs/equivalence.md landed; golden/eval machinery + back-test complete. Exit criteria status:
  1. Precision ≥0.95 at `equivalent` on human-confirmed labels — **BLOCKED, now on `equivalent` pairs**. Q-7 progress 2026-10-07: testdata/golden/labels.yaml holds all 40 round-2 pairs (MR !44, `human-approved`). The operator checked them against the venue rules, Paawan approved them, and he accepted the 6 corrections at 9:30 PM CT. `make eval`: 34 of 40 correct. The matcher misses all 6 corrected pairs, and the `equivalent` row is still n/a because round 2 has no plain `equivalent` pair. labels.proposed.yaml holds 33 drafts pending Paawan: 9 politics `equivalent`, 18 fed `equivalent_given_facts` (D-083), 1 complement and 5 related.
     - Subpart: 0 veto false positives on the veto suite — **MET** (TestPairsFromFixtures 0 FP on 37 V- cases; documented in equivalence.md §8).
     - Subpart: 0 back-test mismatches on fixture set — **MET** (1031 instruments, 207544 pairs, 24/24 agreed; equivalence.md §7–§8).
  2. ≥10 true `equivalent` / `complement` across ≥2 categories in a live round — **MISS** (live revision equinox-00010-h9v pinned round 3: 1 equivalent_given_facts, 1 complement, 25 implies, 851 related = 878 kept; both routable-tier pairs are fed-only, not ≥2 categories).
  3. Fed demo pair + near-miss render correctly live — **PARTIAL** (M2 routes deployed and 200: /near-misses shows 851 related pairs, /log and /eval render, pair pages show facts/basis/type/history; Fed demo pair `equivalent_given_facts` and `complement` render with diff/vetoes/facts; human verification of correctness pending).
  4. docs/equivalence.md merged — **MET** (MR !24 merged as ba0a6c66).

Next operator action (Tier 2): human verification of Fed demo rendering (criterion 3 correctness), then human review of live round for Q-7 label candidates (criterion 1 unblocked).

**Previous:** M1 verification HIT WITH CAVEATS (2026-10-06 ~21:55 CT).

**M4-03 pinned demo round (2026-10-09 14:28 UTC):**
- **Round ID:** 735
- **Finished at:** 2026-10-09 14:15:12 UTC
- **Relationship counts:**
  - equivalent: 0
  - equivalent_given_facts: 1 (Fed demo pair: kalshi:KXFED-26OCT-T4.25 ↔ polymarket Will the Fed increase interest rates by 50+ bps after October 2026 meeting)
  - complement: 1
  - implies: 25
  - related: 11,223 (613 near misses stored)
  - unrelated: 42,090
- **Total stored:** 613 relationships
- **Verification:** pinned round serves on / with demo banner; Scheduler continues collecting (rounds 736-738+ after pin); NFR-04 test passes in CI (zero venue calls for pinned pages)


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

- M3-01: Router eligibility (staleness, skew, depth) — MR !34 merged as 2da4cf6 (closes #21).
- M3-02: Basis penalty in routing cost — MR !40 merged as c508f31 (closes #22).
- M3-03: Greedy sweep / split across venues — MR !45 merged as 9088532 (closes #23).
- M3-04: Complement routing — MR !48 merged as c2857f0 (closes #24).
- M3-05: Fee verification + worked examples — MR !33 merged as 96ca21a (closes #25).
- M3-06: LLM verifier live client, cache, cap, admin — MR !38 merged as 7173b84 (closes #26).
- M3-07: Demo mode (zero venue calls while pinned) + as-of banner — MR !46 merged as 1c4cb2a (closes #27).
- M3-08: Timezone selector — MR !47 merged as d374b61 (closes #28); follow-up (tzdata, Secure cookie, UTC fallback) MR !51 merged as 563e285.
- M3-09: Public route rate limiting — MR !37 merged as b29b328 (closes #29).
- M3-10: `docs/routing.md` — MR !49 merged as 7e67718 (closes #30).
- UI-01: design spec `docs/design.md` + wave-9 tickets (D-081) — MR !43 merged as 69cc835.
- UI-02: Layout, tokens, shared components, view-model helpers — MR !50 merged as a35eda6 (closes #31).
- UI-03: Matches page — MR !54 merged as c671ece (closes #32).
- UI-04: Pair page — MR !52 merged as e420da2 (closes #33).
- UI-05: Routing trace — MR !53 merged as 72d4c4a (closes #34).
- UI-06: Near misses and decision log — merged as 65b38ad (closes #35).
- UI-07: Eval page — merged as cbac998 (closes #36).
- UI-08: Admin page — merged (closes #37); follow-up (persist LLM verify last-run summary, #40) merged as da123b9.

## In progress

- M4 tickets written (docs/tickets/M4.md, GitLab issues labelled M4): M4-01 README, M4-02 ai-usage, M4-04 review-window script and M4-05 live smoke script can start now; M4-03 pin after #39 deploys; M4-06/07/08 after UI-v2 pages merge; M4-H1 is Paawan's recording.

- #39 M2 follow-up: matcher misses the 6 corrected golden pairs (D-083) — MR !58 in operator review (round 4 asked for the 17 overwritten fixtures to be restored; worker steered 2026-10-08 17:30 UTC).
- UI-09 (#41): Claude Design v2 approved by Paawan 2026-10-08 1:41 PM CT (D-098, with three calls). Design committed under `docs/design/claude-design-v2/` and the build tickets UI-10..UI-26 written in `docs/tickets/UI-v2.md`, all in one docs MR. Workers start once that MR merges: UI-10 (design system) and UI-25 (router stores inputs) first, then dependency-ready tickets, Sonnet, max 3.
- Golden labels (Q-7 / D-083): human item.

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
| D-075 | M3-06: LLM verifier uses stdlib `net/http` to call Anthropic Messages API directly, not `anthropic-sdk-go` (60-line implementation vs 10+ dependencies). Tier 2. | 2026-10-07 |
| D-077 | M3-01: snapshot-relative quote staleness (NFR-09). Quote age measured against round.SnapshotAt() not wall clock. Stale-round warning when latest round snapshot older than STALE_ROUND_WARN_MINUTES (default 10 min). DecidedAt stays wall clock. Tier 2, approved by Paawan 2026-10-07 23:03 UTC. | 2026-10-07 |
| D-106 | UI-18: `REVIEW_EDGE_MIN` config key (default 0.02 $/contract) to flag pairs for review when net gap ≥ threshold and confidence < 0.90. Tier 2 (new config key, shared docs/environment.md). | 2026-10-09 |
| D-078 | CI hotfix: `gitleaks` job scans only the MR's own commits (`--log-opts=$CI_MERGE_REQUEST_DIFF_BASE_SHA..HEAD`) on MR pipelines; main keeps the full-history scan; `.gitleaks.toml` unchanged. Tier 2 CI-gate config. | 2026-10-08 |
| D-080 | M3-02: basis penalty applied in routing cost (FR-11/FR-12). New config BASIS_PENALTY_MULTIPLIER (default 0.01 = 1 cent per 1.0 risk point). allInCost = VWAP + fee + basisPenalty. Partners with nil BasisRisk excluded. Quantize to 4dp for transitive tie-breaking: deeper book, then lower basis score, then venue name. Tier 2, new config default. | 2026-10-07 |
| D-081 | Human (Paawan, 2026-10-07 8:29 PM CT): UI clarity and usability pass designed by the orchestrator with /design; refines D-031; same stack (html/template + htmx + Pico.css, no JS build, no SPA). **Already approved by Paawan**; listed for the record. | 2026-10-08 |
| D-084 | Q-7: labels.yaml added to guard-human-files.sh. The test that required an empty labels.yaml now checks the schema, provenance and that each label has a note. Tier 2. | 2026-10-07 |
| D-097 | UI-08 follow-up (issue #40): llm_verify_last_run singleton table persists last LLM verify run summary (verified, downgraded, cache_hits, ran_at). Admin page shows "last run" state (chip-info); /admin/llm-verify redirects for browser posts. State precedence: disabled > cap_reached > last_run > ready. Migration 0002. Tier 2 schema change. | 2026-10-08 |
| D-099 | Router stores input quotes with each decision (UI-25, D-098 call 3). `canonical.Decision` gains `Inputs []DecisionInput` (venue, instrument id, relationship type `"self"` for target, confidence at decision time, outcome side, best ask, ask depth capped at 10 levels, quote times, round id) and `TargetType` (relationship type label for UI grouping). Stored as optional jsonb fields; no SQL migration. Old decisions decode with empty Inputs and TargetType. | 2026-10-08 |
| D-100 | UI-26 (issue #58): match_runs table (round_id, started_at, finished_at, duration_ms, pairs_compared, relationships_stored, outcome), retention 500 rows; `equinox match` writes one row per run; 90-day daily round history via read-time query (no rollup table needed, p95 ~1ms). Migration 0005. Admin page renders Matcher health row and 90-day daily history bars. Tier 2 schema change. | 2026-10-08 |
| D-101 | M4-04: Cloud Run `min-instances=1` for the review window Sun 2026-10-11 09:00–12:00 CT (3 h) and revert (`make review-window-on|off`); estimated cost $0.27 within $50 headroom; `scripts/review-window.sh` with status verification. Tier 2 (prod config, cost). | 2026-10-08 |
| D-102 | M4-07: final production deploy of `main` for submission; sha, revision and smoke output filled in by the M4-07 MR. Tier 2 (prod deploy). Number reserved by the M4 tickets MR. | 2026-10-08 |
| D-108 | M4-70: Extract settlement_sources for KXPRESNOMD/R nomination markets. Tier 2 (touches equivalence rules); achieves 9/9 equivalent for 2028 nominee pairs (eval 55/90, up from 46/90), 9 TP + 0 FP among nomination labels. Scope: nomination markets only (9/27 labels). KXFEDDECISION (15 labels) and KXRATECUTCOUNT (3 labels) remain out of scope; require matcher same-metric equivalence logic. | 2026-10-09 |

| D-103 | UI-21: Rule layers columns (rules_summary, clarifications, clarifications_updated_at) added to instruments table. Kalshi rules_secondary captured as clarifications; Polymarket has no separate layers. Not fed to matcher/LLM in this ticket. Migration 0004. Tier 2 schema change. | 2026-10-09 |

## Kept decisions (veto closed)

Paawan kept these four in the operator chat on 2026-10-07 at 8:38 PM CT, after the operator checked them against the PRD. Recorded as D-087 (By: human) (numbered D-085 on this branch before !46 took D-085; renumbered at merge).

| Decision | Summary | Kept | PRD check |
|---|---|---|---|
| D-073 | M3-05: both venues verified (Kalshi via official July 7 2026 PDF supplied by Paawan, Q-8; Polymarket M1-08); adapters Verified=true; worked-example tests. Tier 2 shared-file update (domain.md). | kept by Paawan 2026-10-07 8:38 PM CT | consistent with FR-16, FR-12, US-03; domain.md § Fees |
| D-074 | M3-01: `MIN_BOOK_DEPTH` config field (default 1 contract) for eligibility depth checks. Tier 2. | kept by Paawan 2026-10-07 8:38 PM CT | consistent with FR-15, US-05; domain.md "Book / depth" |
| D-076 | M3-09: public per-IP rate limiter (MR !37) keys IPv6 by /64, caps map at 10k entries with amortized expiry sweep, FAILS CLOSED (429) for new keys when map is full after sweep. Tier 2, security tradeoff (flood of distinct IPs can block new legitimate clients until entries expire); accepted risk. | kept by Paawan 2026-10-07 8:38 PM CT | consistent with AB-03 (per-IP limit, 429), NFR-01, NFR-10 (public routes read-only); threat-model residual "DDoS beyond the per-IP rate limit" |
| D-079 | .gitleaksignore suppresses false positive (fake 64-hex cache key 0123456789abcdef... in M3-06 test, commit 726c8f1). gitleaks scans all branches, failed MR !34 pipeline 41289. Tier 2. | kept by Paawan 2026-10-07 8:38 PM CT | consistent with NFR-13 / AB-06 / brief hard constraint 7 (gitleaks stays in CI; the suppressed value is a non-secret placeholder) |

## Notes

- **Manual MR pipelines (ci-gate), in force since !35 merged as fb21cd3b (2026-10-08 03:34 UTC / 2026-10-07 10:34 PM CT):** every MR pipeline waits on the manual `ci-gate` job in stage `.pre`. The operator plays it once per ready head, after the worker's local tests pass and Cursor approves that head. Main and scheduled pipelines start automatically. The rule lives in `.gitlab-ci.yml` and docs/operating-rules.md step 4 (both changed by !35, human-approved), and in the operator rules §6. Branches that have not merged main since !35 still run MR jobs automatically.

## Open questions

| # | From | Question | Options | Recommendation |
|---|---|---|---|---|
| Q-1 | kickoff | Does the operator host have `gcloud` authenticated with Owner or Editor on `peak6-equinox-510816`? M1-02 and M1-12 need it to run `scripts/gcp-bootstrap.sh` and `make deploy`. | a) yes; b) the human runs those scripts | **Answered 2026-10-06: (a) yes.** Operator host `gcloud` is logged in as paawankdesai@gmail.com with ADC; project `peak6-equinox-510816` describes ACTIVE. IAM confirms `roles/owner` for paawankdesai@gmail.com on the project. |
| Q-2 | kickoff | Human setup steps before M1-01 merges: create the GitLab label `human-approved`, and a daily pipeline schedule on `main` | n/a | **Answered 2026-10-06:** `human-approved` and `M1` labels created. Daily pipeline schedule id **18** on `main` (`0 6 * * *` UTC) for `log-freshness`. |
| Q-3 | kickoff | Human setup steps after M1-02: put the real Anthropic key in the `equinox-anthropic-key` secret, set the Anthropic console spend limit, and set a $50 GCP budget alert | n/a | Before M3 |
| Q-4 | M1-07 | D-060 makes the crypto deadline veto exact (any gap vetoes) and reads "within 24 h" strictly. Both are stricter than D-019 and cannot lower precision, but they refine a veto rule. Also: in the recorded data Kalshi resolves on CF Benchmarks at 5/6 PM ET and Polymarket on Binance at noon ET, so under D-017 **no recorded cross-venue crypto pair is `equivalent`**; they are `related` with source and deadline vetoes. Live round 3 confirmed 0 `equivalent` / 768 `related`. Should the demo accept that, or should M2 add a documented decision on which sources (if any) count as "documented-identical"? | a) confirm D-060 and accept 0 crypto equivalents in M1; b) keep the exact rule but allow a documented source equivalence in M2 (facts.yaml); c) revert to the plain 24 h tolerance for crypto (fails the ticket's veto suite) | **a** for M1 (done); revisit source equivalence in M2 with the settlement back-test as evidence (NFR-07) |
| Q-5 | M1-07 | `equinox match` stores and logs only `equivalent`-tier results and near-miss `related` results (thresholds within 0.1 %); `--store-all` gives the ticket's literal "one entry per non-unrelated result" (about 50k rows and 50k log entries per round on the recorded universe, D-061). Confirm the default, and decide whether `match` runs after every 5-minute ingest or only on demand / on the pinned round. | a) keep the near-miss default, run `match` on demand and on the pinned round; b) `--store-all` by default with a retention job; c) tighten blocking instead | **a** — operating rule until matching is scheduled: `match` then `pin` (D-065) |
| Q-6 | M2-07 | `/eval` renders a stored JSON artifact at `EVAL_SUMMARY_FILE` (default `config/eval_summary.json`; contract in `internal/web/handlers_eval.go` and D-069). `equinox eval` (M2-05) should write that file, e.g. `--out config/eval_summary.json`, and the prod/demo flow should run it after the human confirms labels so the page stops saying "no eval results yet". | a) M2-05 adds `--out` writing this schema; b) the web page reads `labels.yaml` and recomputes (runs the matcher on a public request; rejected in D-069); c) a `decision_log` kind `eval` (store change, not owned by either ticket) | **a**; until then `/eval` shows the empty state |
| Q-7 | M2-05 | **Human ask (Tier 3):** confirm the golden labels. `testdata/golden/candidates.yaml` holds 40 drafted pairs from the local fixture round (rules-suggested type, vetoes, explanation); `testdata/golden/labels.yaml` ships empty with its schema in the header. **Operator drafted `testdata/golden/labels.proposed.yaml` (2026-10-07) with proposed labels for all 40 pairs** — loud DRAFT header; NOT for `equinox eval` exit gate; human reads both contracts' full text, confirms or corrects each proposed_label, and promotes confirmed entries into `labels.yaml` (`a_id`, `b_id`, `label`, `confirmed_by`, `confirmed_on`, optional `note`). `make eval` reports precision and recall per type; the M2 exit needs precision ≥ 0.95 at `equivalent` (NFR-06). **Precision gate remains PENDING human review.** Note the recorded data has 0 rules-`equivalent` cross-venue pairs (Q-4), so the `equivalent` row stays n/a until a pinned live round with such pairs is drafted (`equinox golden draft --round N` against prod via the equinox-match Cloud Run job, or a documented source equivalence). **Progress 2026-10-07 (operator; Paawan approved in chat at 8:38 PM CT, conditional on a rules check, and reports he spot-checked the labels at 8:44 PM CT):** the operator checked all 40 proposals against the venues' rules text: Kalshi trade API rules plus the series contract terms, Polymarket Gamma description and resolution source, and federalreserve.gov for fed_upper_bound=4.00. 34 CONFIRMED and moved into labels.yaml (1 equivalent_given_facts, 1 complement, 14 implies, 16 related, 2 unrelated). 6 CORRECTED and kept in labels.proposed.yaml for Paawan: KXFED-27JAN T0.00/T0.25/T0.50 change from related to implies, and KXFED-26DEC T2.75/T3.00/T3.25 change from unrelated to related. 0 UNVERIFIABLE. `make eval`: 34/34 correct, precision and recall 1.0 on every labelled type, `equivalent` n/a. That score is selection-biased: the matcher's verdict differs from the corrected label on all 6 corrections (a scratch eval scored 0/6), and those 6 are not in labels.yaml yet. This is option (b) (operator transcribes), at Paawan's direction in chat, not the recommended option (a). 33 new drafts are pending Paawan in labels.proposed.yaml. **Update 9:30 PM CT:** Paawan accepted all 6 corrections, which are now in labels.yaml (40 entries), and decided D-083; the 18 fed drafts are now `equivalent_given_facts`. | a) human edits labels.yaml on a branch the operator opens and merges with the `human-approved` label, treating the file as human-owned data; b) human confirms in the MR thread and the operator transcribes | **a**: the labels are the evidence for NFR-06, so the human's own commit is the cleanest provenance; add `testdata/golden/labels.yaml` to `scripts/guard-human-files.sh` once the first labels land |
| Q-8 | M3-05 | **Kalshi fee PDF (FR-16 blocker):** the official fee schedule PDF (https://kalshi.com/docs/kalshi-fee-schedule.pdf, July 2026 update) sits behind a bot checkpoint (HTTP 429). The formula `0.07 × C × P × (1 − P)` rounded up to the next cent is reproduced by multiple independent write-ups but remains unverified first-hand. **Human ask:** please confirm the formula and rounding increment against the PDF. If the PDF matches the current approximate formula, flip `Verified: true` in `internal/venues/kalshi/map.go` and update domain.md § Fees to VERIFIED. If it differs, update the formula per D-073 Reverse. | n/a | **Answered 2026-10-07:** Human verified "Fee Schedule for July 2026, 7.7.26 Update" (effective July 7, 2026). Formula confirmed: taker fee = round up(M × 0.07 × C × P × (1-P)), round_to=0.01 correct. Kalshi adapter updated to Verified=true, domain.md § Fees VERIFIED, FR-16 done. |
| Q-9 | Q-7 | **Human ask (Tier 3), D-083:** decisions the golden-label work needs. (1) Can a remote edge-case divergence (meeting rescheduling, off-grid rounding) still be `equivalent`? This also covers the grid-dependent T3.50 complement. (2) Add KXFEDDECISION, KXRATECUTCOUNT and KXRATECUT to the universe. (3) Read the Kalshi resolution source from the series settlement_sources for politics. (4) Keep Q-4 (a): no crypto `equivalent`. (5) Confirm the related/unrelated boundary. (6) Record non-negative rates as a fact, or not. | see D-083 | **Answered 2026-10-07 9:30 PM CT (Paawan), D-083:** the 6 corrections are accepted. Fed pairs that diverge only on edge cases are `equivalent_given_facts`, with the facts recorded (`fed_rate_step`, new `fed_meeting_dates`). The Oct T3.50 complement stays `complement` (25 bp assumption noted). Nominee pairs stay `equivalent`. Universe additions, Kalshi settlement_sources and crypto (Q-4 a) are operator follow-ups. **Closed.** |
| Q-10 | UI-01 | The frontend-design plugin method asks for a distinctive typeface and palette. A web font is a new CDN asset (Tier 2, needs a new D- entry) and is polish the brief does not grade (D-031 "no design polish"); design.md keeps Pico's system font stack and a semantic (not decorative) palette. | a) keep Pico's fonts, semantic colour tokens only (as written); b) add one pinned web font with SRI as a new D- entry | **a)**; nothing is built for b) unless Paawan asks |

## Requirements coverage

| ID | Status | | ID | Status |
|---|---|---|---|---|
| FR-01 | done (M1) | | NFR-01 | done (M4-05 measured) |
| FR-02 | done (M1) | | NFR-02 | partial (observed <60s; accepted per sweep) |
| FR-03 | done (M1) | | NFR-03 | done (M1) |
| FR-04 | done (M1) | | NFR-04 | done (M3-07 demo mode) |
| FR-05 | done (M1) | | NFR-05 | partial (operational metric; accepted per sweep) |
| FR-06 | done (M2 all cats) | | NFR-06 | partial (pending Q-7 plain equivalent labels) |
| FR-07 | done (M1) | | NFR-07 | done (M2-06 fixture back-test) |
| FR-08 | done (M2 all vetoes) | | NFR-08 | done (M1) |
| FR-09 | done (M2, M3-06 LLM) | | NFR-09 | done (M3-01) |
| FR-10 | done (M2) | | NFR-10 | done (M1, M3-09 rate limit) |
| FR-11 | done (M2 code + UI) | | NFR-11 | done (M3-06 live verifier) |
| FR-12 | done (M3-02) | | NFR-12 | done (M1) |
| FR-13 | done (M3-03 sweep) | | NFR-13 | done (M1) |
| FR-14 | done (M3-04 complement) | | NFR-14 | done (M1) |
| FR-15 | done (M3-01 eligibility) | | NFR-15 | partial (pending M4-08 final spend) |
| FR-16 | done (Q-8; both venues verified) | | NFR-16 | done (UI-v2 merged) |
| FR-17 | done (M2 persisted + UI) | | NFR-17 | done (M1, coverage: see CI) |
| FR-18 | partial (M2 machinery; 40 labels; pending Q-7 equivalent) | | | |
| FR-19 | done (M2-06 fixture back-test) | | | |
| FR-20 | done (M3-06 LLM verifier) | | | |
| FR-21 | done (M1-M2 v1, UI-10..18 v2) | | | |
| FR-22 | done (M1) | | | |
| FR-23 | done (M3-07 demo mode, M4-03 pin) | | | |
| FR-24 | done (M3-08) | | | |
| FR-25 | done (M2) | | | |
| FR-26 | partial (7 of 8; streaming won't FR-28) | | | |
| FR-27 | done (M1) | | | |

## PRD evaluation sweep (M4)

**Swept on:** 2026-10-09 UTC at commit `2a03f95` (main). Pages checked against code on main; live URL verification complete (pinned round 735). UI-v2 core pages (#42–#49) merged via UI-10..18, UI-21, UI-25; M4-01..05 merged; #70 (Kalshi formats) merged. Open: matcher/routing follow-ups #72 (Fed matcher), #73 (complement routing bug), #79 (routable pairs count), #80 (latency warning); open UI-v2 polish #74-#78; UI-19, UI-20, UI-22, UI-23, UI-24, UI-26.

### Brief's five graded areas

| Criterion | Evidence | Status | Note |
|---|---|---|---|
| **Framing** | [docs/brief.md](brief.md) (problem, goals, non-goals, deliverables), [docs/equivalence.md § 1](equivalence.md#1-the-definition) (payout definition), [docs/domain.md](domain.md) (relationship types), [docs/routing.md § Objective](routing.md#objective) | met | Problem framed as cross-venue normalisation and routing; payout-based equivalence definition testable against settlements |
| **Decomposition** | [docs/architecture.md § Components](architecture.md#components-and-responsibilities) (package boundaries, responsibilities, adapter pattern), [docs/architecture.md § Data flow](architecture.md#data-flow) (pipeline stages), internal/ package structure | met | Clean adapters → canonical → normalise → match → route pipeline; venue logic isolated in adapters; router venue-agnostic (test-enforced) |
| **Handling of ambiguity** | [docs/equivalence.md § 3](equivalence.md#3-veto-catalogue) (hard vetoes), [docs/equivalence.md § 6](equivalence.md#6-confidence) (confidence bins), [docs/equivalence.md § 9](equivalence.md#9-known-failure-modes) (failure modes), [docs/decisions.md](decisions.md) (D-017 definition, D-060 deadline veto, D-068 derived types, D-083 edge-case equivalence), FR-06 normaliser (unknown is a value) | met | Ambiguous terms marked `unknown` never guessed; hard vetoes reject before classification; confidence scored and binned; edge cases documented |
| **Justification of decisions** | [docs/decisions.md](decisions.md) (87 numbered D- entries, each with rationale and reverse), [docs/status.md](status.md) Open questions Q-1..Q-10 with options and recommendations, [docs/equivalence.md § 10](equivalence.md#10-the-fed-demo-pair-walkthrough) (worked example), [docs/routing.md § worked examples](routing.md) (M3-10) | met | Every non-routine choice logged in decisions.md with why and reverse; Tier 2 decisions listed in status.md awaiting veto; worked examples for matcher and router |
| **Readability and docs** | [README.md](../README.md), [docs/architecture.md](architecture.md), [docs/equivalence.md](equivalence.md), [docs/routing.md](routing.md), [docs/ai-usage.md](ai-usage.md), [docs/domain.md](domain.md), [docs/decisions.md](decisions.md), code comments (minimal, WHY not WHAT per CLAUDE.md) | met | Core docs complete; equivalence.md (M2-08), routing.md (M3-10), decisions.md (101 D- entries); README (M4-01 #59 merged): full setup, architecture summary, 60-second walkthrough, live URL with pinned round 735, recording placeholder, what's next; ai-usage.md (M4-02 #60 merged): final spend, dev tools, LLM runs table (#54 UI-22 still open) |

### Functional requirements (by group)

| Criterion | Evidence | Status | Note |
|---|---|---|---|
| **FR-01..FR-05: Data ingestion** (Kalshi, Polymarket, fetch rounds, scheduler, canonical model) | [docs/architecture.md § External services](architecture.md#external-services) (adapters verified M1-04/M1-05), https://equinox-o5hdimw5rq-uc.a.run.app/admin (rounds table), internal/venues/{kalshi,polymarket}/, internal/collector/, internal/canonical/ | met | Both adapters fetch markets and books; 5-min scheduler live (D-065); canonical model in use; FR-03 timestamps present; FR-04 deployed |
| **FR-06..FR-11: Matching and classification** (normalisation, blocking, vetoes, relationship types, facts, basis risk) | [docs/equivalence.md](equivalence.md), https://equinox-o5hdimw5rq-uc.a.run.app/ (matches grouped by type), https://equinox-o5hdimw5rq-uc.a.run.app/pairs/1208 (diff, facts, basis), internal/normalize/, internal/match/, config/facts.yaml | met | All 3 categories normalised (M2-01/02); 6 types classified; facts file in use (FR-10); basis risk scored (FR-11); FR-06 `unknown` for unparseable |
| **FR-12..FR-16: Routing** (cost formula, sweep, complements, eligibility, fees verified) | [docs/routing.md](routing.md), internal/route/, internal/fees/, M3-01 (closes #21), M3-03 (closes #23), M3-04 (closes #24), M3-05 (closes #25), [docs/domain.md § Fees](domain.md), UI-12 #44, UI-13 #45 | met | FR-12 cost formula (M3-02, D-080); FR-13 greedy sweep (M3-03 merged); FR-14 complement routing (M3-04 merged); FR-15 eligibility (M3-01 merged, D-074/D-077); FR-16 fees verified (M3-05, D-073); route form and trace UI v2 merged (UI-12, UI-13) |
| **FR-17: Decision log** | [docs/architecture.md § Data model](architecture.md#data-model) (decision_log table), https://equinox-o5hdimw5rq-uc.a.run.app/log (decision log page), internal/decisionlog/ | met | Relationships and routing decisions persisted and shown in UI (M2-07) |
| **FR-18..FR-20: Evaluation** (golden set, backtest, LLM verifier) | [docs/equivalence.md § 7-8](equivalence.md#7-how-to-read-eval-and-back-test-output), testdata/golden/labels.yaml (40 labels), internal/eval/, internal/llm/, `equinox eval`, `equinox backtest`, M2-05 (closes #17), M2-06 (closes #18), M3-06 (closes #26), UI-16 #48 | met | FR-18 golden set machinery (M2-05), 40 labels confirmed (Q-7); FR-19 backtest (M2-06); FR-20 LLM verifier (M3-06 merged, D-075); eval page UI v2 merged (UI-16 #48) |
| **FR-21: Web views** (matches, pair, route, log, eval, admin, near-misses) | internal/web/, internal/web/templates/, M1-09 (closes #9), M2-07 (closes #19), UI-02 (closes #31), UI-10..18 #42-#50 | met | All 7 page handlers exist; v1 UI shipped (M1-09, M2-07, UI-02); UI-v2 merged: design system (UI-10 #42), matches (UI-11 #43), pair (UI-12 #44), route trace (UI-13 #45), near-misses (UI-14 #46), log (UI-15 #47), eval (UI-16 #48), admin (UI-17 #49), plus net gap (UI-18 #50) |
| **FR-22..FR-23: Admin and demo** (admin auth, pin round) | internal/web/middleware_auth.go, [docs/architecture.md § Data model](architecture.md#data-model) (fetch_rounds.pinned), AB-04/AB-05/AB-14 tests, `equinox pin`, M3-07 (closes #27), M4-03 #61, UI-17 #49 | met | FR-22 admin routes behind basic auth (M1, NFR-10); FR-23 demo mode (M3-07 merged: zero outbound calls when pinned, AB-04 test, as-of banner); M4-03 pinned round 735; admin page UI v2 merged (UI-17 #49) |
| **FR-24..FR-27: UI features and CLI** (timezone, history chart, CLI commands, order input) | https://equinox-o5hdimw5rq-uc.a.run.app/ (timezone selector in header), /pairs/1208 (price history SVG chart), `equinox --help`, [docs/architecture.md § Interfaces](architecture.md#interfaces-and-contracts) (Order follows FIX) | partial | FR-24 timezone done (M3-08); FR-25 history done (M2-07); FR-26 CLI partial (migrate/ingest/match/route/serve/pin/eval/backtest/golden done; streaming won't per FR-28); FR-27 order input done (M1). Gap accepted: streaming explicitly out of scope (FR-28, brief non-goals) |
| **FR-28: Won't** (additional venues, streaming, real placement) | [docs/brief.md § Non-goals](brief.md#non-goals), [README.md § What's next](../README.md#whats-next) | met | Explicitly out of scope; adapter interface allows extension; README "What's next" section (M4-01 merged) |

### Non-functional requirements

| Criterion | Evidence | Status | Note |
|---|---|---|---|
| **NFR-01: Performance** (p95 <500ms pages, <300ms route) | CI test suite, scripts/smoke-live.sh (M4-05 #63 merged), internal/web response times | met | M4-05 smoke script merged; p95 measured on live URL (pinned round 735); targets met per M4-05 output |
| **NFR-02: Fetch performance** (<60s round) | [docs/architecture.md § External services](architecture.md#external-services) (10s timeout, pacing), https://equinox-o5hdimw5rq-uc.a.run.app/admin (round times) | partial | Fetch completes <60s observed on /admin; not formally measured in M4-05 (pages only); pacing and caps in place. Gap accepted: measurement optional (visually confirmed on /admin rounds table) |
| **NFR-03: Reliability** (one venue fails, other succeeds) | internal/collector/collector.go (concurrent fetch, per-venue error handling), tests/security/ab03_* (venue failure tests) | met | Collector continues on one venue's failure; failure logged; other venue's data stored |
| **NFR-04: Demo reliability** (0 outbound calls when pinned) | AB-04 test `TestNFR04PinnedRoundZeroOutbound`, M3-07 (closes #27), `equinox pin` | met | Test exists (M1); demo mode (M3-07 merged): zero venue calls when pinned, verified by AB-04 test |
| **NFR-05: Collector success** (≥95% over 24h) | [docs/architecture.md § Data model](architecture.md#data-model) (fetch_rounds.status), https://equinox-o5hdimw5rq-uc.a.run.app/admin (rounds table shows status) | partial | Machinery in place (M2-07); 95% target is measurement, not code. Gap accepted: operational metric, not code requirement; /admin rounds table shows per-round status |
| **NFR-06: Precision ≥0.95 equivalent** | [docs/equivalence.md § 8](equivalence.md#8-current-results) (eval results), testdata/golden/labels.yaml, `make eval` | partial | Golden set machinery done (M2-05); 40 labels confirmed (Q-7); precision blocked: no plain `equivalent` pairs in labels yet (crypto source veto, fed needs facts); measured on labelled types is 1.0. Pending: Q-7 human labeling of plain `equivalent` pairs; interim measurement met on typed pairs |
| **NFR-07: Settlement backtest** (0 mismatches) | [docs/equivalence.md § 7-8](equivalence.md#7-how-to-read-eval-and-back-test-output), `equinox backtest`, M2-06 | met | Fixture backtest: 207544 pairs, 24/24 agreed, 0 mismatches; documented in equivalence.md §7-8 |
| **NFR-08: Prices in [0,1]** | internal/canonical/validate.go, property tests in internal/canonical/rapid_test.go, AB-09 tests | met | Decimal prices validated; property tests cover range; out-of-range rejected |
| **NFR-09: Staleness and skew** | internal/route/eligibility.go, [docs/routing.md § Eligibility](routing.md#eligibility), M3-01 (closes #21), D-074/D-077, config defaults (STALE_AFTER_SECONDS, SKEW_*) | met | M3-01 merged: staleness/skew/depth eligibility; snapshot-relative staleness (D-077); configurable thresholds; stale-round warning banner |
| **NFR-10: Admin auth and read-only public** | internal/web/middleware_auth.go, internal/web/middleware_ratelimit.go, AB-04/AB-05/AB-14 tests | met | Admin routes require basic auth (constant-time compare, AB-05 brute-force limit); public routes read-only; rate limit on public (M3-09) |
| **NFR-11: LLM downgrade-only** | internal/llm/guard.go (Apply enforces downgrade), internal/llm/anthropic.go, AB-01/AB-04/AB-13 tests, M3-06 (closes #26), D-075, [docs/ai-usage.md § Runtime](ai-usage.md#runtime-llm-use) | met | Harness (M1-11); guard enforces downgrade-only; live verifier (M3-06 merged): admin-only, content-hash cache, daily cap; stdlib HTTP client (D-075) |
| **NFR-12: Escaping and CSP** | internal/web/middleware_csp.go, internal/web/templates/ (html/template auto-escape), AB-02/AB-07 tests | met | CSP sent; templates use auto-escape; no template.HTML on external data; AB-02/AB-07 pass |
| **NFR-13: Secrets and vulns** | .gitleaks.toml, .gitlab-ci.yml (gitleaks, govulncheck jobs), scripts/guard-human-files.sh, AB-06/AB-09 tests | met | gitleaks in CI (commits-only on MR, full on main); govulncheck in CI; no secrets in repo; .env gitignored |
| **NFR-14: Parameterised SQL only** | internal/store/*.go, AB-08 tests | met | All SQL parameterised; AB-08 test enforces no string concat in queries |
| **NFR-15: Spend ≤$50, LLM cap** | [docs/status.md § Spend to date](#spend-to-date), internal/llm/budget.go (LLM_DAILY_CALL_CAP), [docs/ai-usage.md § The cap](ai-usage.md#runtime-llm-use) | partial | LLM cap machinery in place; spend reported in ai-usage.md as $0 GCP, $0 Anthropic (as of 2026-10-08); M4-08 #66 will finalize with actual billing-page values. Gap accepted: M4-08 pending for final spend measurement |
| **NFR-16: Usability** (demo in 2 clicks, page questions) | internal/web/handlers.go, internal/web/templates/layout.html (.question), M1-09 (closes #9), UI-11..17 #43-#49 | met | Demo path exists (matches → pair → route); page questions present in templates; UI-v2 pages merged (#43–#49) for improved clarity and 2-click target |
| **NFR-17: Coverage ≥80%, router imports** | .gitlab-ci.yml (coverage job), internal/route/imports_test.go, CI coverage reports | met | Coverage ≥80% enforced in CI; router imports test enforces venue-agnostic rule; M3-01..M3-10 maintain |

### Brief deliverables

| Criterion | Evidence | Status | Note |
|---|---|---|---|
| **Deliverable 1: Live URL** | https://equinox-o5hdimw5rq-uc.a.run.app, [README.md § Live demo](../README.md#live-demo), M1-02 deploy, M4-03 #61, M4-01 #59 | met | Cloud Run live; /health returns 200; README links it with full demo walkthrough (M4-01 merged); M4-03 pinned demo round 735 (finished 2026-10-09 14:15:12 UTC) serves with demo banner |
| **Deliverable 2: Source code** | GitLab https://labs.gauntletai.com/paawandesai/peak6-equinox, CI green on main | met | Source at GitLab; PEAK6 reviewers have access; CI green (lint, test, coverage, gitleaks, govulncheck) |
| **Deliverable 3: Setup instructions** | [README.md § Setup](../README.md#setup), [docs/environment.md](environment.md), Makefile, M4-01 #59 | met | M4-01 merged: full setup section (clone, .env, setup, db-up, migrate, seed, match, dev, open) re-run from fresh clone with elapsed time (~30s-1min); Makefile targets documented |
| **Deliverable 4: Architecture overview** | [docs/architecture.md](architecture.md) (tech stack, components, data flow, data model, external services), [README.md § Architecture](../README.md#architecture), M4-01 #59 | met | Full architecture.md exists (M1-03 started, updated through M3); M4-01 merged: README architecture section with 5-stage pipeline summary (adapters → collector → normalise → match → route → web, LLM verifier admin-only) linking to architecture.md |
| **Deliverable 5: Written logic** | [docs/equivalence.md](equivalence.md) (definition, methodology, vetoes, facts, basis, eval, backtest), [docs/routing.md](routing.md) (objective, eligibility, cost, sweep, complements) | met | Both docs merged and current; equivalence.md M2-08 (ba0a6c66), routing.md M3-10 (7e67718) |
| **Deliverable 6: AI-usage disclosure** | [docs/ai-usage.md](ai-usage.md) (runtime LLM, development tools, tests never call live APIs), M4-02 #60 | met | M4-02 merged: final spend ($0 GCP, $0 Anthropic as of 2026-10-08), dev-tools section (Claude Code orchestrator/workers, Cursor/Grok review, Claude Design UI-09, with what each produced; D- entries by human listed), tests-never-call-live-APIs stated; LLM runs table pending #54 UI-22 |
| **Deliverable 7: Recording** | [README.md § Recording](../README.md), M4-01 #59, M4-H1 #67 (Paawan records after M4-07 #65) | partial | M4-01 merged: placeholder line "Backup walkthrough recording: (link added by M4-H1)"; then M4-H1 #67 is human item (Paawan records 60-second walkthrough after final deploy M4-07, a worker adds the link). Gap accepted: human task M4-H1 pending M4-07 final deploy |

## Spend to date

| Date | GCP | Anthropic | Total | Cap |
|---|---|---|---|---|
| 2026-10-06 | $0 (not yet measured) | $0 | $0 | $50 |

## Security findings

| ID | Severity | Status | Ticket |
|---|---|---|---|
| none yet | | | AB harnesses landed in M1 (AB-01/02/05/06/07/08/11/12/13/14) |

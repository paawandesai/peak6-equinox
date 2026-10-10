<!-- OWNER: operator. -->
# Status

_Last updated: 2026-10-10 by the operator (submission cleanup, D-132): main at 5ed9ea8 plus this docs MR; production revision `peak6-equinox-00009-8kx` built from 5ed9ea8. Previous: 2026-10-10 by the M4-08 worker._

## Current state (submission)

**Freeze Sun 2026-10-11 09:00 CT; submit by 11:59 AM CT.** After freeze, only docs and demo fixes.

- **Live:** https://peak6-equinox-287867547175.us-central1.run.app, revision `peak6-equinox-00009-8kx` at main `5ed9ea8` (`/version`).
- **Pinned round 904:** 12 routable pairs (11 `equivalent_given_facts` + 1 `complement`, all Fed, confidence ≥ 0.90; D-114). Pairs come from round 904; prices and routing use the newest collected quotes (D-117).
- **Collector:** every 2 minutes (D-130; FR-04 says 5). From the Cloud Run request logs, 2026-10-10 04:18–21:02 UTC: 299 of 301 Scheduler rounds returned 200 (2 returned 409, round already running), median 51.8 s, worst 54.7 s.
- **Merged since M4:**
  - !128: live quotes on a pinned round (D-117).
  - M6 demo-path items !129–!138 (D-118 to D-127; tickets in [tickets/M6.md](tickets/M6.md)).
  - !139: plain-language UI redesign (D-128).
  - This docs MR (D-130 to D-132).
- **Deferred from M6:** P8 (politics universe and re-pin), P9 (`/status` page), P10 (`/markets`, `/compare`). Each needs a new public surface or a live re-pin, after the review window.
- **Admin credential rotated 2026-10-10** after it was printed into an operator transcript (D-131, [ai-usage.md](ai-usage.md)).
- **Known limits** are listed in one place: [README § Known limits](../README.md#known-limits).

## Earlier milestone checkpoints (historical, superseded by the section above)


**M4 demo and submission (target freeze Sun 2026-10-11 09:00 CT, submit by 11:00 CT):** Exit criteria from plan.md § M4:
  1. Every brief deliverable present and linked from README — **MET** (M4-01 !59 merged: live URL, setup, architecture summary, 60-second walkthrough, recording placeholder pending M4-H1 #67; deliverables 2/5/6 already merged)
  2. Live URL and pinned round both pass E2E smoke test — **done except spend line (pending: billing page)**: deployed 2026-10-10 04:36 UTC: main 41b8202 (!97 at 7ea4b21 plus /eval hotfix !108) as the NEW Cloud Run service `peak6-equinox` revision `peak6-equinox-00003-rbj`, https://peak6-equinox-287867547175.us-central1.run.app (Paawan 2026-10-09 9:15 PM CT; same image source, env, secrets, Cloud SQL, service account and settings as the old `equinox` service, which was deleted after verification). Fresh prod round 904 (collected by the new service), equinox-match run, round 904 pinned: **12 routable pairs** (11 `equivalent_given_facts` + 1 `complement`, all confidence >= 0.90). `scripts/smoke-live.sh` passed against the new URL. Measured p95: GET / 726 ms (above the NFR-01 500 ms page target; flagged), POST /route 267 ms. All public routes returned 200 (/, /near-misses, /pairs/2390, the /route POST, /eval, /log, /health, /version commit 41b8202). Spend line (status.md:277) PENDING: requires manual GCP billing page and Anthropic console access.
  3. After freeze, only docs and demo fixes — **pending** (freeze is 2026-10-11 09:00 CT)

**M2 exit verification (2026-10-07 ~18:00 UTC):** **MISS.** All M2 code tickets merged and deployed to prod (revision equinox-00010-h9v); docs/equivalence.md landed; golden/eval machinery + back-test complete. Exit criteria status:
  1. Precision ≥0.95 at `equivalent` on human-confirmed labels — **BLOCKED, now on `equivalent` pairs**. Q-7 progress 2026-10-07: testdata/golden/labels.yaml holds all 40 round-2 pairs (MR !44, `human-approved`). The operator checked them against the venue rules, Paawan approved them, and he accepted the 6 corrections at 9:30 PM CT. `make eval`: 34 of 40 correct. The matcher misses all 6 corrected pairs, and the `equivalent` row is still n/a because round 2 has no plain `equivalent` pair. labels.proposed.yaml holds 33 drafts pending Paawan: 9 politics `equivalent`, 18 fed `equivalent_given_facts` (D-083), 1 complement and 5 related.
     - Subpart: 0 veto false positives on the veto suite — **MET** (TestPairsFromFixtures 0 FP on 37 V- cases; documented in equivalence.md §8).
     - Subpart: 0 back-test mismatches on fixture set — **MET** (1031 instruments, 207544 pairs, 24/24 agreed; equivalence.md §7–§8).
  2. ≥10 true `equivalent` / `complement` across ≥2 categories in a live round — **MISS** (live revision equinox-00010-h9v pinned round 3: 1 equivalent_given_facts, 1 complement, 25 implies, 851 related = 878 kept; both routable-tier pairs are fed-only, not ≥2 categories).
  3. Fed demo pair + near-miss render correctly live — **PARTIAL** (M2 routes deployed and 200: /near-misses shows 851 related pairs, /log and /eval render, pair pages show facts/basis/type/history; Fed demo pair `equivalent_given_facts` and `complement` render with diff/vetoes/facts; human verification of correctness pending).
  4. docs/equivalence.md merged — **MET** (MR !24 merged as ba0a6c66).

Next operator action (Tier 2): human verification of Fed demo rendering (criterion 3 correctness), then human review of live round for Q-7 label candidates (criterion 1 unblocked).

**Previous:** M1 verification HIT WITH CAVEATS (2026-10-06 ~21:55 CT).

**M4-03 pinned demo round (2026-10-10 04:40 UTC):**
- **Round ID:** 904
- **Finished at:** 2026-10-10 (exact time from round metadata)
- **Relationship counts:**
  - equivalent: 0
  - equivalent_given_facts: 11
  - complement: 1
  - implies: (not counted in this verification)
  - related: (near misses count not recorded)
  - unrelated: (not counted)
- **Total routable pairs (conf >= 0.90):** 12 (11 equivalent_given_facts + 1 complement, all Fed)
- **Verification:** pinned round serves on https://peak6-equinox-287867547175.us-central1.run.app with 12 routable pairs; `scripts/smoke-live.sh` passed; NFR-04 test passes in CI (zero venue calls for pinned pages)


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

- M5-01: Home page p95 under 500 ms — MR !111 merged (closes #83).
- M5-02: Pair page under 500 ms — MR !112 merged (closes #84).
- M5-03: Timezone JS fix — MR !110 merged (closes #85).
- M5-04: Admin dead end explanation — MR !115 merged (closes #86).
- M5-05: Start here demo moment — MR !116 merged (closes #87).
- M5-06: Glossary plain language — MR !118 merged (closes #88).
- M5-07: Pair page jump links — MR !121 merged (closes #89).
- M5-08: Route result summary — MR !120 merged (closes #90).
- M5-09: Layout question line once, Glossary in nav, tagline — MR !123 merged (closes #91).
- M5-10: Near-misses headings and per-group cap — MR !122 merged (closes #92).
- M5-11: Decision log reviewer-first view — MR !113 merged (closes #93).
- M5-12a: Eval page readability — MR !119 merged (closes #94).

- !128 (`fix/stale-age-label`): live quotes on a pinned round, snapshot-relative ages. Merged as 1828be2; deployed `peak6-equinox-00005-jw5` (D-117).
- M6 P1–P7, P11, P12: guided Start here, one freshness rule, clarity, pinned labels, near-miss contrast, not-routed lead, log fold, /eval context, /about. !129–!137 into integration/m6-demo-path; !138 merged as 355838c; deployed `peak6-equinox-00006-r98` (D-118 to D-127).
- ui/plain-language (!139): plain-language redesign of decisions, pairs, /, glossary, /about. Merged as 5ed9ea8; deployed `peak6-equinox-00009-8kx` (D-128).
- Submission cleanup: admin password rotated (D-131). Merged branches deleted on GitLab. This docs-alignment MR (D-130, D-132).

## In progress

- M4-H1 backup recording (#67): human item. The README "Recording" line is still a placeholder.
- Nothing else is in progress. M6 P8, P9 and P10 are deferred (see Current state).

## Blocked

None.

## Awaiting human veto

The current list: the Tier 2 operator entries from D-117 to D-132 that are still open. Tier 1 entries in that range (D-118, D-120, D-121, D-122, D-126, D-132) are logged and need no veto. D-128 (the !139 redesign) was kept by Paawan, recorded as D-129 (By: human), and is listed under Kept decisions. The Tier 2 entries listed here before (D-049 to D-116, M1–M5) stand as logged in [decisions.md](decisions.md); any reversal would be a new entry there.

| Decision | Summary | Logged |
|---|---|---|
| D-117 | !128: a pinned round keeps its matched pairs, but prices and routing use the newest collected quote per instrument, with quote age measured against the newest round's snapshot. This deviates from FR-23's acceptance line; Paawan accepted it. Banner relabelled; hourly auto-repin Scheduler job created paused. | 2026-10-10 |
| D-119 | M6-P3: one quote-freshness rule for the Matches market column, the pair page and the route trace. Age is measured against the routing reference, and a quote is stale over `STALE_AFTER_SECONDS`, the router's own test. | 2026-10-10 |
| D-123 | M6-P4: a `related` near miss is listed under "fact did not close" when it has no veto or its stored facts include `fed_upper_bound`. | 2026-10-10 |
| D-124 | M6-P4: the stored "deadlines X apart (within W)" clause is rewritten at display time only, so it says "within" only when true. The matcher's text is unchanged during freeze. | 2026-10-10 |
| D-125 | M6-P2: the not-routed lead sentence and the "Try the other side" hint are built only from the stored decision and the quotes the router read. | 2026-10-10 |
| D-127 | M6 process: item MRs !129–!137 merged into the integration branch on local gates. The full CI pipeline ran once, on the integration MR !138, before main. | 2026-10-10 |
| D-130 | The collector runs every 2 minutes, not the 5 of FR-04 and D-016 (schedule changed 2026-10-10 15:37 UTC). Kept as is; the bootstrap script and `/admin` text still say 5. | 2026-10-10 |
| D-131 | Admin password rotated after the Scheduler Basic-auth header was printed into an operator session. New secret version, header updated in the same step, old version disabled, ingest 200 and `/admin` 401 verified. | 2026-10-10 |

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
| Q-11 | M6-P12 | `LLM_ENABLED=false` (set by `make deploy`) is only checked against the API key at config load; the admin LLM verifier still runs whenever `ANTHROPIC_API_KEY` is mounted. /about states only the facts. | a) gate the verifier on `LLM_ENABLED` (logic change, after freeze); b) leave it and say so in docs/ai-usage.md | **b now, a after the review window.** |
| Q-12 | M6-P11 | The committed `config/eval_summary.json` has no confusion data, so /eval shows no confusion matrix and a typed `/eval/cell?...` URL returns 404 (no page links to it). | a) regenerate eval_summary.json from the run's database (numbers could move); b) leave as is | **b** through the review window; a after. |
| Q-13 | submission cleanup | The home page lists only the first 1,000 stored relationships of a round, in id order (`maxMatchRows` in `internal/web/handlers_matches.go`, `ORDER BY id LIMIT` in `internal/store/relationships.go`). Round 904 is unaffected (its 12 routable pairs show), but the recorded fixture round stores 2,196 near misses first, so locally Routable shows 0 and Start here shows only the near-miss card. | a) query routable types separately from near misses (code change); b) document it (README § Run locally gives a SQL query to find a pair) | **b now** (docs-only, freeze); **a after** the review window. |

## Requirements coverage

Regenerated 2026-10-10 from main at 5ed9ea8. **Built** means the code is merged and evidenced; **partial** means part of the requirement is missing or unproven, with the reason in the note; **not built** means absent. No requirement is not built, apart from FR-28, which is out of scope.

| ID | Status | Evidence | Note |
|---|---|---|---|
| FR-01 | built | `internal/venues/kalshi/` (client.go, map.go), `testdata/fixtures/kalshi/` | Events and markets for the configured series, MVE parlays excluded, one order book per market. |
| FR-02 | built | `internal/venues/polymarket/`, `testdata/fixtures/polymarket/` | Gamma events and markets for the configured tags, CLOB book per outcome token. |
| FR-03 | built | `internal/collector/collector.go`, `migrations/0001_init.sql` (`fetch_rounds`, `raw_responses`, `quotes`) | Both venues fetched concurrently under one round ID; `fetched_at` and venue timestamp on every quote. |
| FR-04 | built, with a deviation | Cloud Scheduler `equinox-collect`, `scripts/gcp-bootstrap.sh`, D-130 | Runs every **2** minutes, not 5 (D-130). 299 of 301 rounds returned 200 on 2026-10-10. |
| FR-05 | partial | `internal/canonical/` (instrument.go, quote.go) | `Instrument` and `Quote` are venue-independent. There is no canonical cross-venue `Event` record: instruments keep the venue's own event ID (`VenueEventID`), and pairs are grouped by relationship type instead. |
| FR-06 | built | `internal/normalize/` (crypto.go, fed.go, politics.go) | An unparseable term is recorded as `unknown`. |
| FR-07 | built | `internal/match/block.go` | Same category, deadlines within ±7 days, overlapping subject; the Fed meeting-dates fact can place two deadlines on one meeting (D-124). |
| FR-08 | built | `internal/match/veto.go`, `testdata/match/*_pairs.yaml`, `TestPairsFromFixtures` | Every veto recorded; crypto deadlines must be identical (D-060, stricter than 24 h). |
| FR-09 | built | `internal/match/classify.go`, `internal/match/diff.go` | Types, confidence and contract diff; [equivalence.md](equivalence.md). |
| FR-10 | built | `config/facts.yaml`, `internal/facts/` | Each fact has value, source and as-of date and is shown on the pair page. |
| FR-11 | built | `internal/match/basis.go` | Components: cutoff gap, source difference, edge rules. |
| FR-12 | built | `internal/route/route.go`, `internal/fees/`, [routing.md](routing.md) | All-in cost = book-walk VWAP + fee + basis penalty (D-080); live decision #96. |
| FR-13 | built | `internal/route/sweep.go` | Greedy split when one book cannot fill. |
| FR-14 | built | `internal/route/` (complement routes), live decision #97 | Buys the complement when it is cheaper all-in; refuses a sure-loss complement (decision #98). |
| FR-15 | built | `internal/route/eligibility.go` | Stale, skew, depth and minimum confidence, each with a reason. |
| FR-16 | built | `internal/venues/*/map.go` fee schedules, [domain.md § Fees](domain.md) | Both venues verified against official docs (D-073, Q-8). |
| FR-17 | built | `internal/decisionlog/`, `/log`, `/decisions/{id}`, `/decisions/{id}/json` | Every verdict and routing decision is stored with its explanation. |
| FR-18 | built | `testdata/golden/labels.yaml` (95 labels), `internal/eval/`, `config/eval_summary.json` | Precision and recall per type ([equivalence.md §8](equivalence.md#8-current-results)). |
| FR-19 | partial | `internal/eval/backtest.go`, `config/backtest_summary.json` | Run on recorded fixtures only: 29 of 29 checked verdicts agreed. A live back-test against the venues has not been run. |
| FR-20 | partial | `internal/llm/` (anthropic.go, guard.go, cache.go, budget.go), `POST /admin/llm-verify`, `tests/security/ab01_*`, `ab13_*` | Built and tested with mocks: downgrade-only, cached, capped, admin only. **Never run in production**, because no API key is mounted there. The rules+LLM eval has not been run. `LLM_ENABLED` does not gate it (Q-11). |
| FR-21 | built | `internal/web/` handlers and templates; live URL | Matches with near misses, pair page (diff and history), route form and result, log, eval, admin, plus /about and /glossary (D-121, D-128). |
| FR-22 | built | `internal/web/middleware_auth.go`, `internal/web/admin.go`, `tests/security/ab05_*`, `ab12_*`, `ab14_*` | Ingest, pin and unpin, and LLM verify, all behind basic auth. |
| FR-23 | partial (accepted deviation) | `equinox pin`, `internal/web/live_quotes.go`, `tests/security/nfr04_demo_zero_outbound_test.go`, D-117 | The pinned round serves every page with zero venue calls. But prices and routing use the newest collected quotes, not the pinned round's own, so pages are not wholly "as of" the round. Paawan accepted this deviation (D-117); the banner says so. |
| FR-24 | built | `internal/web/timezone.go`, `internal/web/handlers_timezone.go` | ET (default), CT, UTC and the browser's local time; display only. |
| FR-25 | built | `internal/web/history.go` | Mid prices per venue and the spread, from stored rounds. |
| FR-26 | built | `cmd/equinox/` | `ingest`, `match`, `route`, `eval`, `backtest`, `serve`, `migrate`, plus `seed`, `pin`, `golden`, `fixture-server`, `record-fixtures` and `version`. |
| FR-27 | built | `internal/canonical/order.go`, route form on `/pairs/{id}` | Side, outcome, quantity (contracts or USD notional), market or limit, limit price, IOC or FOK; simulated only. |
| FR-28 | won't | [brief.md § Non-goals](brief.md#non-goals) | Out of scope by design. |

| ID | Status | Evidence | Note |
|---|---|---|---|
| NFR-01 | partial | `tests/security/nfr01_latency_test.go` (seeded test DB), live samples | The test passes locally (p95 a few ms). Live, on 2026-10-10: pair pages took 2.2–3.3 s on revision 00006. After the redesign (00009), a 50-request warm sample gave client-side p95 0.20 s for `/pairs/2406` and 0.38 s for `/`. `POST /route` p95 was 267 ms at the M4-07 deploy. The earlier slowness was not isolated, and cold starts are slower, so the target is not proven. |
| NFR-02 | built, met | `internal/collector/`, Cloud Run request logs | Production rounds: median 51.8 s, worst 54.7 s (2026-10-10), under 60 s. |
| NFR-03 | built | `internal/collector/collector.go`, `TestRunOneVenueFailsIsPartial` | One venue's failure leaves a `partial` round with the other venue stored; it shows on `/admin`. |
| NFR-04 | built | `TestNFR04PinnedRoundZeroOutbound` (`tests/security/nfr04_demo_zero_outbound_test.go`) | Still holds under D-117: live quotes are read from Postgres, not from the venues. |
| NFR-05 | built; measured over 16.7 h only | Cloud Run request logs (not `fetch_rounds`) | 299 of 301 Scheduler rounds succeeded (99.3%) between 04:18 and 21:02 UTC on 2026-10-10, the service's first day. Not yet a full 24 h window, and not measured from `fetch_rounds` as the NFR states. |
| NFR-06 | met | `config/eval_summary.json`, `TestPairsFromFixtures` | `equivalent` precision 1.0000 (9 of 9) on 95 human-confirmed labels; 0 veto-suite false positives. |
| NFR-07 | met on fixtures | `config/backtest_summary.json` | 0 mismatches on 29 checked verdicts (208,064 pairs). A live run has not been made (FR-19). |
| NFR-08 | built | `internal/canonical/quote.go`, rapid property tests in `internal/canonical/quote_test.go` and `order_test.go` | Out-of-range prices are rejected with a quality flag. |
| NFR-09 | built | `internal/route/eligibility.go`, `internal/web/live_quotes.go`, D-077, D-119 | 120 s snapshot-relative staleness, 5 s and 30 s skew, and the stale-round banner (all configurable). |
| NFR-10 | built | `internal/web/middleware_auth.go`, `middleware_ratelimit.go`, `tests/security/ab05_*`, `ab14_*` | Constant-time compare, brute-force lockout; public routes read-only. The admin password was rotated on 2026-10-10 (D-131). |
| NFR-11 | built | `internal/llm/guard.go`, `tests/security/ab01_*`, `ab04_*`, `ab13_*` | No public route can call the LLM; the output is schema-checked and downgrade-only. |
| NFR-12 | built | `internal/web/middleware_csp.go`, `html/template`, `tests/security/ab02_*`, `ab07_*` | Strict CSP with no inline script or style; venue payloads are size-capped. |
| NFR-13 | built | `.gitleaks.toml`, `.gitlab-ci.yml` (gitleaks, govulncheck), `tests/security/ab06_*` | No secret in the repo (gitleaks clean on 2026-10-10). One credential was printed into an operator transcript, not the repo or logs, and rotated (D-131). |
| NFR-14 | built | `internal/store/`, `tests/security/ab08_*` | Parameterised SQL only. |
| NFR-15 | partial | `internal/llm/budget.go` (`LLM_DAILY_CALL_CAP`) | The daily LLM call cap is built. **Spend is not measured**: the GCP billing page and the Anthropic console have not been read (see Spend to date). |
| NFR-16 | built | `/` Start here cards (D-118, D-128), one question line per page | The demo moment is 1 click from `/` (Start here card 1) and 2 to a routing result. |
| NFR-17 | built | `scripts/check-coverage.sh`, CI coverage job, `internal/route/imports_test.go` | Coverage 92.5% of statements over `./internal/...` on 2026-10-10 (gate 80%); the router imports no venue package (test-enforced). |

## PRD evaluation sweep (M4)

**Swept on:** 2026-10-10 UTC at the M4 merge (ffcf7a6). **Refreshed** 2026-10-10 for submission (D-132) against main at 5ed9ea8 and the live URL https://peak6-equinox-287867547175.us-central1.run.app (pinned round 904). The earlier service URL `equinox-o5hdimw5rq-uc.a.run.app` and round 735 no longer exist.

### Brief's five graded areas

| Criterion | Evidence | Status | Note |
|---|---|---|---|
| **Framing** | [docs/brief.md](brief.md) (problem, goals, non-goals, deliverables), [docs/equivalence.md § 1](equivalence.md#1-the-definition) (payout definition), [docs/domain.md](domain.md) (relationship types), [docs/routing.md § Objective](routing.md#objective) | met | Problem framed as cross-venue normalisation and routing; payout-based equivalence definition testable against settlements |
| **Decomposition** | [docs/architecture.md § Components](architecture.md#components-and-responsibilities) (package boundaries, responsibilities, adapter pattern), [docs/architecture.md § Data flow](architecture.md#data-flow) (pipeline stages), internal/ package structure | met | Clean adapters → canonical → normalise → match → route pipeline; venue logic isolated in adapters; router venue-agnostic (test-enforced) |
| **Handling of ambiguity** | [docs/equivalence.md § 3](equivalence.md#3-veto-catalogue) (hard vetoes), [docs/equivalence.md § 6](equivalence.md#6-confidence) (confidence bins), [docs/equivalence.md § 9](equivalence.md#9-known-failure-modes) (failure modes), [docs/decisions.md](decisions.md) (D-017 definition, D-060 deadline veto, D-068 derived types, D-083 edge-case equivalence), FR-06 normaliser (unknown is a value) | met | Ambiguous terms marked `unknown` never guessed; hard vetoes reject before classification; confidence scored and binned; edge cases documented |
| **Justification of decisions** | [docs/decisions.md](decisions.md) (entries through D-132, each with rationale and reverse; unused numbers explained in place), [docs/status.md](status.md) Open questions Q-1..Q-13 with options and recommendations, [docs/equivalence.md § 10](equivalence.md#10-the-fed-demo-pair-walkthrough) (worked example), [docs/routing.md § worked examples](routing.md) (M3-10) | met | Every non-routine choice logged in decisions.md with why and reverse; Tier 2 decisions listed in status.md awaiting veto; worked examples for matcher and router |
| **Readability and docs** | [README.md](../README.md), [docs/architecture.md](architecture.md), [docs/equivalence.md](equivalence.md), [docs/routing.md](routing.md), [docs/ai-usage.md](ai-usage.md), [docs/domain.md](domain.md), [docs/decisions.md](decisions.md), code comments (minimal, WHY not WHAT per CLAUDE.md) | met | Core docs aligned with the code on 2026-10-10 (D-132). README: live URL, four-card walkthrough, three example decisions, local run from fixtures, known limits; recording still a placeholder (M4-H1). ai-usage.md: LLM production status, spend not measured, the D-131 rotation |

FR and NFR rows: see **Requirements coverage** above, which replaces the per-group sweep tables of M4-06.

### Brief deliverables

| Criterion | Evidence | Status | Note |
|---|---|---|---|
| **Deliverable 1: Live URL** | https://peak6-equinox-287867547175.us-central1.run.app, [README.md § Live demo](../README.md#live-demo), D-114, D-128 | met | Cloud Run revision `peak6-equinox-00009-8kx` (main 5ed9ea8); pinned round 904 with 12 routable pairs; `/health` 200; walkthrough re-run after each deploy on 2026-10-10 |
| **Deliverable 2: Source code** | GitLab https://labs.gauntletai.com/paawandesai/peak6-equinox, CI green on main | met | Source at GitLab; PEAK6 reviewers have access; CI green (lint, test, coverage, gitleaks, govulncheck) |
| **Deliverable 3: Setup instructions** | [README.md § Run locally](../README.md#run-locally-from-recorded-fixtures), [docs/environment.md](environment.md), Makefile | met | Re-run from a fresh clone on 2026-10-10 (migrate, seed ~45 s, match ~7 s, serve); corrected the nonexistent `make match` and the seed time; the local Routable-0 limit is documented (Q-13) |
| **Deliverable 4: Architecture overview** | [docs/architecture.md](architecture.md) (tech stack, components, data flow, data model, external services), [README.md § Architecture](../README.md#architecture), M4-01 #59 | met | Full architecture.md exists (M1-03 started, updated through M3); M4-01 merged: README architecture section with 5-stage pipeline summary (adapters → collector → normalise → match → route → web, LLM verifier admin-only) linking to architecture.md |
| **Deliverable 5: Written logic** | [docs/equivalence.md](equivalence.md) (definition, methodology, vetoes, facts, basis, eval, backtest), [docs/routing.md](routing.md) (objective, eligibility, cost, sweep, complements) | met | Both docs merged and current; equivalence.md M2-08 (ba0a6c66), routing.md M3-10 (7e67718) |
| **Deliverable 6: AI-usage disclosure** | [docs/ai-usage.md](ai-usage.md) | met | Runtime verifier and its production status (not run in prod; Q-11), development tools including the 2026-10-10 Claude Code operator sessions, the D-131 credential rotation, and spend (not measured) |
| **Deliverable 7: Recording** | [README.md § Recording](../README.md), M4-01 #59, M4-H1 #67 (Paawan records after M4-07 #65) | partial | M4-01 merged: placeholder line "Backup walkthrough recording: (link added by M4-H1)"; then M4-H1 #67 is human item (Paawan records 60-second walkthrough after final deploy M4-07, a worker adds the link). Gap accepted: human task M4-H1 pending M4-07 final deploy |

## Spend to date

| Date | GCP | Anthropic | Total | Cap |
|---|---|---|---|---|
| 2026-10-10 | not measured (billing page not read; budget-alert status could not be checked from the operator host) | not measured; the production service has no API key mounted, so it made no model calls | not measured | $50 |
| 2026-10-06 | $0 (not yet measured) | $0 | $0 | $50 |

**Note:** D-101 estimates $0.27 for the 3 h `min-instances=1` review window (Sun 2026-10-11 09:00–12:00 CT). The 2-minute collector (D-130) costs more Cloud Run CPU time and stores more rows than the 5-minute plan assumed. Measuring spend needs the GCP billing page and the Anthropic console (human access).

## Security findings

All abuse cases have mitigation tests in `tests/security/`. No open findings.

| ID | Threat | Mitigation test(s) | Status |
|---|---|---|---|
| AB-01 | Prompt injection in venue data tries to make LLM upgrade or create relationships | `TestAB01_PromptInjectionSuite`, `TestAB01_UnrelatedNeverBecomesRelated` | mitigated (M1-11 !9, AB-01) |
| AB-02 | Hostile venue payloads (malformed JSON, huge bodies, nested structures, NaN/negative prices) | `TestAB02_Kalshi*` (5 tests), `TestAB02_Polymarket*` (10 tests) | mitigated (M1-04 !7, M1-05 !8, AB-02) |
| AB-03 | DoS via request flood | `TestAB03_PublicRateLimit`, `TestAB03_RateLimitPerIP`, `TestAB03_RateLimitWindowExpires`, `TestAB03_XFFRightmost`, `TestAB03_AdminExempt` | mitigated (M3-09 !37, D-076, AB-03) |
| AB-04 | Public route triggers unbounded LLM spend | `TestAB04_PublicRoutesNeverCallLLM`, `TestAB04_AdminRouteCanCallLLM`, `TestAB04_AdminRouteWithMockAPI`, `TestNFR04PinnedRoundZeroOutbound` | mitigated (M1-11 !9, M3-06 !38, M3-07 !46, AB-04, NFR-04) |
| AB-05 | Admin brute-force attack | `TestAB05_AdminBruteForceLockout`, `TestAB05_CredentialsUseConstantTimeCompare`, `TestAB05_LockoutUsesForwardedIPOnlyBehindCloudRun` | mitigated (M1-09 !14, AB-05) |
| AB-06 | Secrets in repo or logs | `TestAB06_GitignoreIgnoresEnv`, `TestAB06_IsEnvFile`, `TestAB06_NoTrackedEnvFiles`, `TestAB06_VersionHasNoSecrets`, CI gitleaks job | mitigated (M1-01 !1, D-061, D-078, #80 !96 version test, AB-06) |
| AB-07 | XSS via hostile venue text | `TestAB07_HostileTextRendersEscaped` | mitigated (M1-09 !14, AB-07) |
| AB-08 | SQL injection or hostile IDs | `TestAB08_StoreQueriesAreConstant`, `TestAB08_HostilePairIDsInPath`, `TestAB08_HostileRouteFormIDs`, `TestAB08_HostileIDsThroughStoreLookups` | mitigated (M1-03 !5, AB-08) |
| AB-09 | Known-vulnerable dependencies | CI govulncheck job | mitigated (M1-01 !1, AB-09) |
| AB-11 | LLM cache poisoning or upgrade via cache | `TestAB11_CacheKeyUniqueness`, `TestAB11_CacheKeyIgnoresPresentation`, `TestAB11_CachedVerdictNeverUpgrades`, `TestAB11_CachedVerifierNeverUpgrades` | mitigated (M3-06 !38, AB-11) |
| AB-12 | CSRF on admin mutations | `TestAB12_AdminMutationsArePostOnly`, `TestAB12_AdminMutationRejectsForeignOrigin`, `TestAB12_OriginCheckNeedsAuthFirst` | mitigated (M1-09 !14, AB-12) |
| AB-13 | LLM output schema violation or output-based upgrade | `TestAB13_ValidOutputStillGuarded`, `TestAB13_SchemaFailuresLeaveRulesVerdict`, `TestAB13_VerifierSurfacesDecodeErrors` | mitigated (M1-11 !9, M3-06 !38, AB-13) |
| AB-14 | Public route mutation (create/update/delete data or trigger actions) | `TestAB14_PublicRoutesRejectMutations`, `TestAB14_RouteIsTheOnlyPublicPost`, `TestAB14_AdminIsTheOnlyMutatingSurface` | mitigated (M1-09 !14, M2-07 !27, AB-14) |
| AB-15 | Unbounded collector growth exhausts DB or budget | `TestAB15_AlertAboveThreshold`, `TestAB15_NoAlertAtOrBelowThreshold`, `TestAB15_IngestTriggersCheck`, `TestAB15_UnauthenticatedSeesNothing` (5 GiB alert on /admin) | mitigated (M2-07 !27, D-069, AB-15; D-100/D-113 bounded retention) |

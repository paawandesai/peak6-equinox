<!-- OWNER: shared. Operator may update to match reality; every change is logged in docs/decisions.md. -->
# Decisions

The operator appends new entries at the bottom and never edits or deletes `By: human` entries. To reverse a human decision, the operator adds a new entry that references the old one, and that entry needs human approval.

Entry format: `ID · By · Tier (operator entries only) · Date`, then **Decision**, **Why**, **Rejected**, **Reverse**.

## Conflicts resolved at kickoff (most recent decision wins)

| Topic | Earlier | Final | Entry |
|---|---|---|---|
| Storage | JSON snapshot files, no DB (2026-10-06 AM) | Cloud SQL Postgres + 5-min collector | D-016 |
| Repo host | GitHub `m-paawandesai/equinox` | GitLab `labs.gauntletai.com/paawandesai/peak6-equinox` | D-037 |
| Reviewer | Codex | Cursor (run by AO) | D-039 |
| Orchestration | AO orchestrator session plans and spawns | Grok operator hosts and drives AO; AO spawns Claude Code workers and the Cursor reviewer | D-039 |
| Scheduling | "Steps only, no timings" | Milestones carry target dates scheduled backward from the deadline, with no hour-level timings | D-040 |
| Matching v1 | Hybrid rules + LLM from the start | Rules only first, then Anthropic layered on | D-003 |
| Merge authority | "Never merge without my OK" (playbook) | Operator merges CI-green, reviewer-approved MRs (Tier 1). The human may veto. | D-049 |

## Entries

**D-001 · human · 2026-10-06** · **Decision:** Go for all application code. **Why:** the PEAK6 brief names Go and GCP as their internal stack. **Rejected:** Python (faster for the LLM work, weaker signal). **Reverse:** not reversible this week.

**D-002 · human · 2026-10-06** · **Decision:** GCP project `peak6-equinox-510816`, region `us-central1`. **Why:** the partner's cloud; billing is on. **Rejected:** local-only (the brief allows it, but a live URL is wanted). **Reverse:** redeploy elsewhere; config only.

**D-003 · human · 2026-10-06** · **Decision:** matching is deterministic rules first, then an Anthropic LLM layered on as a verifier. **Why:** a reproducible, testable baseline, with the LLM measured as a delta against it. **Rejected:** LLM-first matching; rules only forever. **Reverse:** disable with `LLM_ENABLED=false`.

**D-004 · human · 2026-10-06** · **Decision:** a web view, not just a CLI. **Why:** it makes the demo moment visible in seconds. **Rejected:** a CLI with markdown reports. **Reverse:** n/a.

**D-005 · human · 2026-10-06** · **Decision:** venues are Kalshi and Polymarket (international) only. **Why:** together they hold about 100% of the volume of the top three venues (Jul 2026: Kalshi 74.5%, Polymarket Intl 15.6%, Polymarket US 9.9%), and both have public read APIs with no auth (verified 2026-10-06). **Rejected:** a third venue (Polymarket US, Rothera, ForecastEx, Crypto.com, CME), which is listed as future work. **Reverse:** add an adapter package.

**D-006 · human · 2026-10-06** · **Decision:** the market universe is Fed/macro, crypto price thresholds, and politics/elections. Sports are excluded. **Why:** known cross-venue overlap and three distinct matching difficulties (structure, thresholds and dates, naming). **Rejected:** sports, which are high-volume but a naming swamp. **Reverse:** edit `config/universe/<venue>.yaml`.

**D-007 · human · 2026-10-06** · **Decision:** the canonical unit is the binary contract. Multi-outcome events are treated as groups of binaries, and scalar or range markets are skipped with a logged reason. **Why:** both venues' tradable unit is already binary (Polymarket multi-outcome = negRisk bundles). **Rejected:** a general outcome-vector model. **Reverse:** extend `canonical`.

**D-008 · human · 2026-10-06** · **Decision:** the primary demo is the live public URL. The fallback is a pinned stored round in the same app, plus a recorded walkthrough. **Why:** it must work for a live walkthrough and for async review. **Rejected:** local-only demo. **Reverse:** n/a.

**D-009 · human · 2026-10-06** · **Decision:** split static reference data (`Instrument`, with `Terms`) from market data (`Quote`, a timestamped book), grouped by `Event`. **Why:** the market standard (security master vs market data, as in FIX). **Rejected:** a single flat market record. **Reverse:** schema migration.

**D-010 · human · 2026-10-06** · **Decision:** prices are probabilities in [0,1] per $1 payout. Buys execute at the ask, sells at the bid, and mid is display only. The Kalshi YES ask is derived as 1 − best NO bid. **Why:** market standard; Kalshi publishes bid-only books per side. **Rejected:** routing on mid or last price. **Reverse:** n/a.

**D-011 · human · 2026-10-06** · **Decision:** fee schedules are adapter-supplied data (a named formula plus parameters), verified against official venue docs during the build, with the source URL and date recorded in domain.md and covered by worked-example tests. **Why:** keeps the router venue-agnostic while staying honest about cost. **Rejected:** ignoring fees. **Reverse:** n/a.

**D-012 · human · 2026-10-06** · **Decision:** treat USD (Kalshi) and USDC (Polymarket) as 1:1, documented as an assumption. **Why:** the standard simplification for a simulation. **Rejected:** modelling depeg. **Reverse:** add an FX term in `fees`/`route`.

**D-013 · human · 2026-10-06** · **Decision:** store all times in UTC and keep the venue's stated cutoff timezone as a field. The UI has a display timezone selector that defaults to ET. **Why:** venue rules are written in ET, and a timezone mismatch is a basis-risk input. **Rejected:** storing local time. **Reverse:** n/a.

**D-014 · human · 2026-10-06** · **Decision:** every quote carries `fetched_at` (and the venue timestamp where one is provided). Both venues are fetched concurrently per round. Cross-venue skew above 5 s warns and above 30 s excludes; quotes older than 120 s are ineligible on live data. **Why:** comparing quotes from different moments is a false comparison (the NBBO-style consolidated-quote standard). **Rejected:** ignoring time alignment; websockets (Kalshi streaming needs keys). **Reverse:** env thresholds.

**D-015 · human · 2026-10-06** · **Decision:** a "snapshot" is a stored fetch round. Live mode uses the latest successful round, demo and eval pin a round, and raw responses are kept so data can be re-normalised. **Why:** a deterministic demo and reproducible eval. **Rejected:** separate snapshot files. **Reverse:** n/a.

**D-016 · human · 2026-10-06** · **Decision:** Cloud SQL Postgres 16 (smallest instance, about $10/month), with a collector every 5 minutes via Cloud Scheduler. Docker Postgres locally. **Why:** Cloud Run's disk is ephemeral; the history of cross-venue prices is a feature; the data is relational plus a time series; it's on the partner's cloud. **Rejected:** JSON files (they lose history), Firestore (no joins), BigQuery (wrong fit for transactional reads and writes; future export), SQLite + GCS (breaks with more than one instance), AlloyDB (overkill). **Reverse:** swap `store` implementation. *Supersedes the earlier no-DB proposal.*

**D-017 · human · 2026-10-06** · **Decision:** two contracts are **equivalent** if and only if buying YES on both pays out identically in every reachable world state, meaning the proposition, resolution window, resolution source and edge-case handling all match. The relationship types are `equivalent`, `equivalent_given_facts`, `complement`, `implies`, `related` and `unrelated` (see domain.md). **Why:** a payout definition is testable, conservative, and maps directly onto routing interchangeability. **Rejected:** a similarity score threshold. **Reverse:** human only.

**D-018 · human · 2026-10-06** · **Decision:** the pipeline runs normalise → block → hard vetoes → soft checks → classify + diff → basis risk → (optional) LLM verify. Normalisation gets its own work per category. **Why:** every step is explainable, and match quality is capped by extraction quality. **Rejected:** end-to-end embeddings. **Reverse:** n/a.

**D-019 · human · 2026-10-06** · **Decision:** the hard vetoes are a different subject, a different threshold or comparator, deadlines more than 24 h apart, and unresolvable polarity. Each has table-driven adversarial tests. **Why:** false positives are the worst failure. **Rejected:** soft-only scoring. **Reverse:** human only.

**D-020 · human · 2026-10-06** · **Decision:** optimise for precision. Targets are precision ≥ 0.95 at `equivalent` and 0 false positives in the veto suite; recall is reported. **Why:** a false equivalent routes money to the wrong bet, whereas a miss only loses an option. **Rejected:** F1 optimisation. **Reverse:** human only.

**D-021 · human · 2026-10-06** · **Decision:** external facts (for example the current fed funds upper bound) live in a versioned `config/facts.yaml`, each with a value, source and as-of date. Every use is shown in the trace. **Why:** conditional equivalence is made explicit, not hidden. **Rejected:** hard-coded facts. **Reverse:** n/a.

**D-022 · human · 2026-10-06** · **Decision:** the LLM (a) adjudicates `uncertain` pairs and (b) second-opinions every rules `equivalent`, and it may **only downgrade**. Verdicts are cached by content hash, it is admin-triggered only, and the public site never causes a call. **Why:** adding the LLM can only reduce false positives, and spend is controlled. **Rejected:** letting the LLM create matches. **Reverse:** human only.

**D-023 · human · 2026-10-06** · **Decision:** the golden set is about 40 real pairs. Candidates are drafted by tooling, and the human confirms the labels. It is complemented by a settlement back-test on resolved markets. **Why:** the user required validation on real data; contracts that settled differently are objective false positives. **Rejected:** synthetic pairs only. **Reverse:** n/a.

**D-024 · human · 2026-10-06** · **Decision:** the order model follows FIX NewOrderSingle: side, outcome, quantity (contracts or USD notional), order type (market/limit), limit price, and TIF (IOC/FOK), all simulated. **Why:** market standard, and it matches Kalshi's order API. **Rejected:** an ad-hoc order shape. **Reverse:** n/a.

**D-025 · human · 2026-10-06** · **Decision:** the full differentiator: typed relationships, a contract diff, basis-risk-aware routing, and routing via complement contracts. **Why:** an options firm thinks in basis risk and synthetic exposure. **Rejected:** the "lite" version without complement routes. **Reverse:** cut-order item 1.

**D-026 · human · 2026-10-06** · **Decision:** routing minimises the all-in cost per $1 payout, which is book-walk VWAP + fees + resolution-risk penalty, after eligibility filters (staleness, skew, depth, confidence). **Why:** every term is explainable in one line. **Rejected:** best-ask only. **Reverse:** n/a.

**D-027 · human · 2026-10-06** · **Decision:** a greedy level-by-level sweep across venues by all-in marginal cost when one book can't fill the order. **Why:** standard smart order routing, the same idea as Reg NMS sweeps. **Rejected:** an optimiser. **Reverse:** n/a.

**D-028 · human · 2026-10-06** · **Decision:** `internal/route` imports only `canonical`, `fees`, decimal and stdlib, enforced by a test. **Why:** the brief requires no venue assumptions in routing. **Rejected:** convention only. **Reverse:** human only.

**D-029 · human · 2026-10-06** · **Decision:** every matching and routing decision is persisted with its explanation, and this file is maintained for the whole build. **Why:** auditability is graded. **Rejected:** none. **Reverse:** n/a.

**D-030 · human · 2026-10-06** · **Decision:** the package layout is as in architecture.md. **Why:** it mirrors the brief's required separation, and each package is one worker's turf. **Rejected:** a flat package. **Reverse:** refactor.

**D-031 · human · 2026-10-06** · **Decision:** the UI uses `html/template` + htmx + Pico.css, and must be clear and easy, with no design polish. **Why:** the brief says UI polish isn't graded, but clarity helps reviewers. **Rejected:** React/SPA. **Reverse:** n/a.

**D-032 · human · 2026-10-06** · **Decision:** every PRD evaluation criterion maps to a page or doc, checked in M4. **Why:** the user required every criterion to be hit. **Reverse:** n/a.

**D-033 · human · 2026-10-06** · **Decision:** deploy via `make deploy` (`gcloud run deploy --source .`). CI is GitLab CI for checks; no CI-to-GCP deploy. **Why:** the fewest moving parts. **Rejected:** CD from CI with workload identity. **Reverse:** add a CD job.

**D-034 · human · 2026-10-06** · **Decision:** secrets live in GCP Secret Manager (mounted as env on Cloud Run) and in a gitignored `.env` locally. **Why:** standard practice. **Reverse:** n/a.

**D-035 · human · 2026-10-06** · **Decision:** the public view is fully open and read-only (including the routing simulation). `/admin/*` uses HTTP basic auth with a password in Secret Manager. **Why:** reviewers click freely, while mutations and spend stay protected. **Rejected:** a viewer password; user accounts. **Reverse:** add middleware.

**D-036 · human · 2026-10-06** · **Decision:** `log/slog` JSON logs, with a separate decision log in the DB. **Reverse:** n/a.

**D-037 · human · 2026-10-06** · **Decision:** the code lives on GitLab only, at `labs.gauntletai.com/paawandesai/peak6-equinox`, with MRs and GitLab CI. **Why:** the repo already exists there and PEAK6 reviewers have access. **Rejected:** GitHub. **Reverse:** n/a.

**D-038 · human · 2026-10-06** · **Decision:** Make targets are the command interface (see CLAUDE.md). **Reverse:** n/a.

**D-039 · human · 2026-10-06** · **Decision:** the Grok operator hosts and drives AO. AO spawns Claude Code workers (one ticket per worker, branch and worktree) and runs the Cursor reviewer on every MR. There is no fixed worker cap; parallelism is limited only by non-overlapping file ownership. **Why:** the user's chosen setup. **Rejected:** a Codex reviewer; an AO orchestrator session as the planner. **Reverse:** human only.

**D-040 · human · 2026-10-06** · **Decision:** milestones have target dates scheduled backward from the deadline, with no hour-level timings. **Why:** the newest instruction (kickoff pack) supersedes "no timings". **Reverse:** human only.

**D-041 · human · 2026-10-06** · **Decision:** the cut order is complement routing → LLM verifier → eval page → history chart → timezone selector. The contract diff, decision trace, eval CLI, docs and live URL are never cut. **Reverse:** human only.

**D-042 · human · 2026-10-06** · **Decision:** `docs/ai-usage.md` discloses runtime LLM use (where, why, guardrails, the eval delta) and development AI use (AO, Claude Code, Cursor, the Grok operator), and is kept current alongside this file. **Why:** a brief deliverable. **Reverse:** n/a.

**D-043 · human · 2026-10-06** · **Decision:** the deadline is Sun 2026-10-11 11:59 CT, with code freeze at 09:00 CT Sunday. **Reverse:** human only.

**D-044 · human · 2026-10-06** · **Decision:** the target persona is the PEAK6 Labs / NTAP reviewer, and the secondary persona is a broker trader. **Why:** Labs builds Apex's NTAP, which includes prediction markets. **Reverse:** n/a.

**D-045 · human · 2026-10-06** · **Decision:** operator decision tiers as in operating-rules.md. Tier 2 logs without waiting unless the decision is critical. **Reverse:** human only.

**D-046 · human · 2026-10-06** · **Decision:** total budget ≤ $50 for Anthropic + GCP through 2026-10-11, on the cheapest Anthropic tier (`claude-haiku-4-5-20251001`), with a daily LLM call cap (default 200). **Why:** the LLM is a small part of the product. **Reverse:** human only.

**D-047 · human · 2026-10-06** · **Decision:** the CI gates are lint, vet/build, tests including `tests/security/`, coverage ≥ 80% over `./internal/...`, gitleaks, govulncheck, a human-file guard, and a daily log-freshness check. Property tests are used for parsers and financial math. **Why:** the kickoff pack requirements. **Reverse:** human only.

**D-048 · human · 2026-10-06** · **Decision:** submission is the GitLab repo plus the live URL in the README, with the recording linked as a backup. No public mirror is needed. **Why:** reviewers have Gauntlet GitLab access, and the UI is the reviewer experience. **Reverse:** n/a.

**D-049 · human · 2026-10-06** · **Decision:** the operator may merge MRs that are CI-green, Cursor-approved and don't touch human-owned files (Tier 1). Production deploys are Tier 2. **Why:** autonomous build; the human keeps a veto. **Rejected:** human approval on every merge. **Reverse:** edit operating-rules.md.

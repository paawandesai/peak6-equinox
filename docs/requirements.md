<!-- OWNER: human only. Operator may propose changes via MR; only the human approves them. -->
# Requirements

Terms are defined in [domain.md](domain.md), and components are described in [architecture.md](architecture.md).

## Personas

| ID | Persona | Goals | Pain points |
|---|---|---|---|
| P1 | **PEAK6 reviewer**: an NTAP architect or engineer evaluating the build | Judge whether cross-venue normalisation and routing is viable, and judge the candidate's reasoning | Glossy demos that hide hand-waving; fuzzy "match scores" with no definition; no way to inspect why a decision was made |
| P2 | **Broker trader / ops analyst**: the eventual NTAP user | See one event across venues and know where an order would be best executed and why | Venues name the same event differently; prices aren't comparable once fees and depth are counted; stale quotes |
| P3 | **Operator / admin**: Paawan or the operator bot | Keep data flowing, pin a demo snapshot, run LLM verification within budget | Silent ingest failures, runaway LLM spend, a demo that breaks when a venue API flakes |

## User stories

**US-01** As P1, I want to see every cross-venue pair the system found, grouped by relationship type, so that I can judge coverage and rigour at a glance.
- AC: The page lists pairs grouped as `equivalent`, `equivalent_given_facts`, `complement`, `implies`, `related`. Each row shows both venue titles, the type, the confidence and the data round.
- AC: A "near-misses" view lists pairs that were rejected by a hard veto, along with the veto that fired.

**US-02** As P1, I want a side-by-side contract diff for any pair so that I can see exactly why the system thinks two contracts are, or are not, the same bet.
- AC: The diff shows each structured term (subject, metric, comparator, threshold, deadline + timezone, resolution source, polarity, edge-case rules) for both contracts, marked as match, differ or unknown.
- AC: The diff shows any facts used (with source and as-of date), the basis-risk score with its components, and the method used (rules, or rules plus LLM verdict).

**US-03** As P2, I want to submit a hypothetical order and get a routing decision with a plain-English explanation, so that I understand why that venue (or split) was chosen.
- AC: The input fields are side, outcome, quantity (contracts or USD notional), order type, an optional limit price and a time-in-force.
- AC: The output includes the chosen legs, ranked alternatives with their per-term cost (book-walk VWAP, fees, basis penalty), any exclusions with reasons, and a one-paragraph summary.

**US-04** As P2, I want the router to consider equivalent and complement contracts on other venues, so that I get the cheapest route to the same exposure.
- AC: When buying NO on a complement contract is cheaper all-in than buying YES on the target, the decision uses it and the explanation says so.

**US-05** As P2, I want the system to refuse to compare quotes that are stale or far apart in time, so that I'm not misled by an unfair comparison.
- AC: A venue whose quote is older than the staleness limit, or whose skew against the other venue exceeds the maximum, is excluded with a reason. A smaller skew above the warning threshold produces a warning in the trace.

**US-06** As P1, I want measured accuracy for the matcher, so that I can trust its precision claims.
- AC: An eval page and the CLI command report precision and recall per relationship type on the golden set, for rules-only and for rules plus LLM, along with the results of the settlement back-test.

**US-07** As P1 or P2, I want to see how a matched pair's prices moved across venues over time, so that I can see cross-venue divergence.
- AC: The pair detail page shows a time series of each venue's mid price and the spread between them, built from stored fetch rounds.

**US-08** As P1, I want every matching and routing decision logged with its reasoning, so that I can audit the system.
- AC: A decision-log page lists recent decisions with their type, inputs, outcome and explanation, and each entry links to the related pair or order.

**US-09** As P3, I want to trigger an ingest, pin a demo round and run LLM verification behind admin auth, so that the public can't change data or spend money.
- AC: Every `/admin/*` route requires HTTP basic auth, and public routes cannot mutate anything or call the LLM.

**US-10** As P1 or P2, I want to choose the timezone used to display times, so that deadlines read naturally.
- AC: A selector switches between ET (the default), CT, UTC and the browser's local time. Only the display changes, and stored values stay in UTC.

**US-11** As P1, I want the demo to work even if a venue API is down, so that the walkthrough can't fail live.
- AC: When a demo round is pinned, every page and the routing simulation work from that stored round with no outbound venue calls, and the UI labels the data "as of" that round.

## Functional requirements

MoSCoW: M = Must, S = Should, C = Could, W = Won't (this build).

| ID | Requirement | Stories | Pri |
|---|---|---|---|
| FR-01 | Kalshi adapter: fetch events and markets for the configured series via the public API, excluding multivariate parlay (MVE) markets. Fetch the order book per market. | US-01, US-03 | M |
| FR-02 | Polymarket adapter: fetch events and markets via the Gamma API for the configured tags, and fetch order books per outcome token via the CLOB API | US-01, US-03 | M |
| FR-03 | Fetch round: fetch both venues concurrently, store raw responses and normalised rows under one round ID, and timestamp every quote with `fetched_at` (and the venue timestamp where one is provided) | US-05, US-07, US-11 | M |
| FR-04 | Scheduled collection every 5 minutes on the deployed system | US-07 | M |
| FR-05 | Canonical model: venue-independent `Instrument` (static terms) and `Quote` (book plus timestamps) records, grouped by canonical `Event` | US-01, US-02 | M |
| FR-06 | Normalisation: extract the structured terms from titles and rules text with deterministic rules for three categories (Fed/macro, crypto price thresholds, politics). An unparseable term becomes `unknown`, never a guess. | US-02 | M |
| FR-07 | Candidate blocking: compare only pairs in the same category whose deadlines are within ±7 days and whose subject entities overlap | US-01 | M |
| FR-08 | Hard vetoes: a different subject, a different threshold or comparator, deadlines more than 24 hours apart, or unresolvable polarity all block `equivalent`. Each veto is recorded. | US-01, US-02 | M |
| FR-09 | Relationship classification into the types in [domain.md](domain.md), with a confidence score and a contract diff | US-01, US-02 | M |
| FR-10 | Facts file: a versioned `config/facts.yaml` of external facts (each with value, source and as-of date) that the matcher may apply. Every fact used is shown. | US-02 | M |
| FR-11 | Basis-risk score for `equivalent` and `complement` pairs, built from documented components (cutoff gap, source difference, edge-case rule difference) | US-02, US-03 | M |
| FR-12 | Routing: compute the all-in cost per $1 payout for each eligible venue as book-walk VWAP + fees + basis penalty, then pick the best | US-03 | M |
| FR-13 | Routing: a greedy multi-venue sweep (split) when one book can't fill the order | US-03 | S |
| FR-14 | Routing via complement contracts | US-04 | S |
| FR-15 | Eligibility: exclude venues that are stale, exceed the skew limit, have insufficient depth, or fall below the minimum relationship confidence, with reasons | US-05 | M |
| FR-16 | A fee schedule per venue, supplied by the adapter as data and verified against official docs (the source URL and date it was checked are recorded in domain.md) | US-03 | M |
| FR-17 | Decision log: persist every relationship verdict and routing decision with its explanation, and show it in the UI | US-08 | M |
| FR-18 | Golden set: about 40 hand-labelled real pairs (labels confirmed by the human), plus an eval command reporting precision and recall per type | US-06 | M |
| FR-19 | Settlement back-test: run the matcher on settled contracts from both venues and report any `equivalent` pair that settled differently | US-06 | M |
| FR-20 | LLM verifier: the cheapest Claude tier reviews rules-`equivalent` pairs and adjudicates `uncertain` ones, and may only downgrade. Verdicts are cached by content hash and calls are budget-capped. Admin-triggered only. | US-06, US-09 | S |
| FR-21 | Web view: a matches page (with near-misses), a pair detail page (diff and price history), a routing form with its trace, the decision log, eval results, and an admin page | US-01 to US-10 | M |
| FR-22 | Admin routes behind HTTP basic auth: trigger ingest, pin or unpin the demo round, run LLM verification | US-09 | M |
| FR-23 | Demo mode: the pinned round serves every page with no venue calls | US-11 | M |
| FR-24 | Timezone selector for display | US-10 | S |
| FR-25 | Price history chart per pair | US-07 | S |
| FR-26 | A CLI (`equinox`) with the subcommands `ingest`, `match`, `route`, `eval`, `backtest`, `serve`, `migrate` | US-06, US-09 | M |
| FR-27 | Order input follows FIX NewOrderSingle fields (side, outcome, quantity, order type, limit price, time-in-force IOC/FOK), simulated | US-03 | M |
| FR-28 | Additional venues, websocket streaming, real order placement | none | W |

## Non-functional requirements

| ID | Category | Requirement (measurable) |
|---|---|---|
| NFR-01 | Performance | Server-side p95 under 500 ms for page renders from the current or pinned round. Routing simulation p95 under 300 ms. |
| NFR-02 | Performance | A full fetch round for both venues completes in under 60 s for the configured categories |
| NFR-03 | Reliability | One venue's failure never breaks a round. The other venue's data is still stored, and the failure is logged and shown on the admin page. |
| NFR-04 | Reliability | When the demo round is pinned, the app makes 0 outbound venue calls to serve pages or routing (verified by a test) |
| NFR-05 | Reliability | The collector achieves at least 95% successful 5-minute rounds over any 24-hour window after deploy (measured from `fetch_rounds`) |
| NFR-06 | Data accuracy | Precision at least **0.95** for `equivalent` on the golden set, and **0** false positives in the hard-veto test suite. Recall is reported, with no target. |
| NFR-07 | Data accuracy | Settlement back-test: **0** `equivalent` pairs that settled differently, with every exception investigated and logged |
| NFR-08 | Data accuracy | Every price in the system is a decimal probability in [0,1] per $1 payout. Inputs outside that range are rejected and flagged (property-tested). |
| NFR-09 | Data accuracy | Quotes more than 120 s older than their round's snapshot time are ineligible (age is measured against the snapshot, not the current time; D-077). Skew above 5 s warns and above 30 s excludes. Routing shows the snapshot's "as of" time, and live pages show a warning banner when the latest successful round is more than 10 minutes old (all configurable; see [environment.md](environment.md)). |
| NFR-10 | Security | Every `/admin/*` route requires basic auth with a constant-time compare and a rate limit; public routes are read-only (AB-04, AB-05, AB-14) |
| NFR-11 | Security | No LLM call can originate from a public request. The verifier output is schema-validated and the downgrade-only rule is enforced in code (AB-01, AB-04, AB-13) |
| NFR-12 | Security | All external data is rendered with auto-escaping and a strict CSP is sent. Venue payloads are size-limited and validated (AB-02, AB-07) |
| NFR-13 | Security | Zero secrets in the repo or logs (gitleaks in CI, log redaction). Zero known-vulnerable dependencies at high severity or above (govulncheck in CI) (AB-06, AB-09) |
| NFR-14 | Security | Only parameterised SQL (AB-08) |
| NFR-15 | Cost | Anthropic plus GCP spend ≤ $50 through 2026-10-11. LLM calls are capped per day via config (AB-04). |
| NFR-16 | Usability | A first-time reviewer can find the demo moment (pair diff → routing trace) from the landing page in 2 clicks or fewer, and every page states what question it answers in one line |
| NFR-17 | Maintainability | Test coverage of at least 80% of statements across `./internal/...` (see [CLAUDE.md](../CLAUDE.md)). The router package imports no venue package (enforced by a test). |

## Key metrics

| Metric | How measured | Target |
|---|---|---|
| Equivalent precision (golden set) | `equinox eval` | ≥ 0.95 |
| Hard-veto false positives | `go test ./internal/match/...` veto suite | 0 |
| Settlement back-test mismatches | `equinox backtest` | 0 |
| True equivalent and complement pairs found in live data | count in the latest round | ≥ 10 |
| Collector success rate (24 h) | `fetch_rounds` success / total | ≥ 95% |
| LLM delta | precision and recall, rules-only vs rules+LLM | reported |
| Spend to date | GCP billing + Anthropic console | ≤ $50 |
| Coverage | CI coverage job | ≥ 80% |

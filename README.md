<!-- OWNER: operator. -->
# Equinox

Cross-venue prediction-market aggregation and routing **simulation**, built for PEAK6 Labs. Equinox pulls markets and order books from Kalshi and Polymarket and normalises them into one venue-independent model. It classifies which contracts are economically the same bet, with a written definition and a contract diff, and it simulates routing a hypothetical order with a plain-English explanation. No real orders are ever placed.

## Live demo

**https://peak6-equinox-287867547175.us-central1.run.app**

The service runs on Cloud Run (us-central1) with Cloud SQL Postgres. A Cloud Scheduler job collects one fetch round from Kalshi and Polymarket every 2 minutes (D-131). The demo is **read-only and simulated**: no real orders are ever placed.

**What the pages show.** Pairs, relationship types and contract diffs come from **pinned round 904** (matched 2026-10-10), so the demo is stable. Prices, quote ages and routing use the **newest collected quote** for each instrument, and every price shows its age (D-117). The banner at the top of each page says this. Both come from the database: serving a page or a route makes zero venue API calls (NFR-04, `TestNFR04PinnedRoundZeroOutbound`).

Health check: [/health](https://peak6-equinox-287867547175.us-central1.run.app/health). Build: [/version](https://peak6-equinox-287867547175.us-central1.run.app/version). How it works and its limits, for a non-specialist: [/about](https://peak6-equinox-287867547175.us-central1.run.app/about).

## Demo walkthrough

Open the home page. The **Start here** box has four cards, which are the whole demo:

1. **See a match**, [/pairs/2406](https://peak6-equinox-287867547175.us-central1.run.app/pairs/2406). Kalshi's "Fed rate above 4.25% after Oct 28" and Polymarket's "Fed raises 50+ bps in October" pay out on the same outcome, given three stated facts: the current upper bound (4.00%), the 25 bp step and the date of the next meeting. The top card says so in words. **Technical details** below it hold the term-by-term diff, the facts with sources, the confidence arithmetic and the basis-risk components.
2. **Route an order** ([preset](https://peak6-equinox-287867547175.us-central1.run.app/pairs/2406?outcome=yes&qty=100&side=buy&target=kalshi%3AKXFED-26OCT-T4.25&tif=IOC&type=market#route)). This fills in "buy 100 YES on the Kalshi contract, market, IOC" on the same pair. Press **Route it**. The result leads with a verdict card and a comparison table in cents per contract, and the old step-by-step trace is under **Technical details**.
3. **See a rejection**, [/pairs/2166](https://peak6-equinox-287867547175.us-central1.run.app/pairs/2166). Kalshi's "Fed above 2.75%" for December and Polymarket's "Fed +25 bps" in December look alike but are not the same bet: December's change is measured from October's result, which is not known yet, so the Fed fact does not close the pair. It is listed under "Fact did not close" on [/near-misses](https://peak6-equinox-287867547175.us-central1.run.app/near-misses).
4. **Trade the opposite** ([preset](https://peak6-equinox-287867547175.us-central1.run.app/pairs/2390?outcome=no&qty=100&side=buy&target=kalshi%3AKXFED-26OCT-T3.50&tif=IOC&type=market#route)). This fills in "buy 100 NO on Kalshi's 'Fed above 3.50%'". Polymarket's "Fed cuts 50+ bps in October" is its complement, so buying YES there gives the same exposure.

**Three stored example decisions.** These were recorded on 2026-10-10 against round 904. A new route on the same pair uses the prices of that moment, so its numbers will differ. Each decision also has a raw JSON view at `/decisions/{id}/json`.

| Decision | Order | Outcome |
|---|---|---|
| [#96](https://peak6-equinox-287867547175.us-central1.run.app/decisions/96) | Buy 100 YES, Kalshi "Fed above 4.25%" (card 2) | **Routed to Polymarket**, "Fed raises 50+ bps in October": 0.40¢ price + 0.02¢ fee + 0.38¢ safety charge for basis risk = 0.80¢ per contract, $0.80 for 100, against $1.07 on Kalshi (1¢ + 0.07¢ fee). |
| [#97](https://peak6-equinox-287867547175.us-central1.run.app/decisions/97) | Buy 100 NO, Kalshi "Fed above 3.50%" (card 4) | **Routed to Polymarket through the complement**: YES on "Fed cuts 50+ bps in October" pays exactly when the Kalshi NO would. 0.20¢ + 0.01¢ + 0.28¢ = 0.49¢ per contract, $0.49 for 100, against $1.07 on Kalshi. |
| [#98](https://peak6-equinox-287867547175.us-central1.run.app/decisions/98) | Buy 100 YES, Kalshi "Fed above 3.50%" | **No trade.** Nobody on Kalshi is selling YES. The only true substitute would cost $1.0019 for a contract that pays at most $1, a sure loss. Four Polymarket contracts only cover some of the outcomes (`implies`), so they are never routed. Next step offered: buy NO on Kalshi at 1¢. |

**Then look around:**
- [/near-misses](https://peak6-equinox-287867547175.us-central1.run.app/near-misses): lookalikes the matcher rejected, grouped by the rule that fired.
- [/eval](https://peak6-equinox-287867547175.us-central1.run.app/eval): golden-set precision and recall, and the settlement back-test (numbers in [equivalence.md §8](docs/equivalence.md#8-current-results)).
- [/log](https://peak6-equinox-287867547175.us-central1.run.app/log): every routing decision and verdict.
- [/glossary](https://peak6-equinox-287867547175.us-central1.run.app/glossary): every term and code the pages use.

## Run locally from recorded fixtures

Requirements: Go 1.21 or newer (the toolchain pinned in `go.mod`, Go 1.27.2, downloads automatically), Docker with Compose, and Make. Clone into a path **without spaces**: `make setup` and `make lint` install tools under the repo path and break on a space.

```bash
git clone https://labs.gauntletai.com/paawandesai/peak6-equinox.git
cd peak6-equinox
cp .env.example .env          # local-only defaults; no real credentials needed
make setup                    # Go modules and pinned tools into ./bin
make db-up                    # Postgres 16 in Docker on 127.0.0.1:5432
make migrate                  # apply the schema (a few seconds)
make seed                     # load one recorded fixture round from testdata/fixtures, offline (~45 s)
set -a; . ./.env; set +a      # make loads .env for its targets; plain `go run` needs it exported
go run ./cmd/equinox match    # classify every cross-venue pair in that round (~7 s)
make dev                      # serve http://localhost:8080
```

Checked from a fresh clone on 2026-10-10: the fixture round has 2,530 instruments, and `match` finds 75 `equivalent`, 16 `equivalent_given_facts`, 1 `complement` and 31 `implies` among 611,260 pairs. `/near-misses`, `/eval`, `/log`, `/about` and the pair pages work as on the live site. One local difference: the home page lists only the first 1,000 stored relationships of a round, and in the fixture round the near misses fill that window, so **Routable shows 0 locally** (see Known limits). To open a routable pair, find its id:

```bash
docker compose exec postgres psql -U equinox -c "select id, type, confidence from relationships where type in ('equivalent','equivalent_given_facts','complement') order by confidence desc limit 5"
```

Then open `http://localhost:8080/pairs/<id>`. `make dev-fixtures` serves the recorded venue responses on port 8090 if you want to point the venue URLs in `.env` at it and run `equinox ingest` offline.

Before pushing, run `make check` (typecheck + lint + test + coverage + gitleaks + govulncheck); tests need `make db-up`. `make e2e` runs the demo-path smoke test. Every variable is described in [docs/environment.md](docs/environment.md).

## Architecture

Equinox is a **read-only simulation**: it pulls market data, classifies contracts and simulates routing, but never sends orders. Six parts:

1. **Adapters** (`internal/venues/kalshi`, `internal/venues/polymarket`) fetch instruments and order books from the public venue APIs and map them into a venue-independent canonical model (`internal/canonical`). Only these packages know venue schemas.
2. **Collector** (`internal/collector`) runs one fetch round (both venues concurrently) on each Scheduler call, every 2 minutes in production. It stores gzipped raw responses and the canonical rows in Postgres.
3. **Normaliser** (`internal/normalize`) extracts terms (subject, metric, threshold, deadline, source, polarity) from each contract's title and rules for three categories: crypto, Fed and politics. A term it cannot parse is recorded as `unknown`, never guessed.
4. **Matcher** (`internal/match`) runs **on demand** over one round (`equinox match`, or the `equinox-match` Cloud Run job in production), not after every ingest (D-065). It blocks, applies hard vetoes, classifies (`equivalent`, `equivalent_given_facts`, `complement`, `implies`, `related`), consults stated facts (`config/facts.yaml`), builds the contract diff and scores basis risk. The operator then pins the matched round (`equinox pin`).
5. **Router** (`internal/route`, venue-agnostic by test) simulates a hypothetical order. It checks eligibility (staleness, skew, depth, confidence), then prices each candidate all-in as book-walk VWAP + fee + basis penalty. It sweeps across venues when one book is too thin, and it considers complement contracts. Every decision carries an explanation and is logged.
6. **Web** (`internal/web`, server-rendered `html/template` + htmx, strict CSP) serves the public pages and the `/admin` pages behind basic auth. Pairs come from the pinned round, and prices and routing from the newest quotes (D-117).

The optional LLM verifier (`internal/llm`) is admin-triggered and downgrade-only, and it runs under a daily call cap. It has not run in production: no API key is mounted there ([docs/ai-usage.md](docs/ai-usage.md)).

**Full details:** [docs/architecture.md](docs/architecture.md)

## Known limits

- **Edge rules are scored, not vetoed.** Early-close, tie and void rules are not extracted, so they are never compared. Each matched pair pays a confidence and basis-risk cost instead ([equivalence.md §9](docs/equivalence.md#9-known-failure-modes)).
- **No mutually-exclusive type.** "Vance wins" and "Harris wins" cannot both pay, but their subjects differ, so the pair is `unrelated` and never routed.
- **The live round has no politics pairs.** All 12 routable pairs in round 904 are Fed (11 `equivalent_given_facts`, 1 `complement`). Politics `equivalent` pairs appear on the recorded fixtures, but widening the live politics universe was deferred (docs/tickets/M6.md, P8).
- **The settlement back-test ran on recorded fixtures only** (29 of 29 checked verdicts agreed). A live back-test against the venues has not been run.
- **Page speed is not proven against NFR-01 (p95 under 500 ms).** On 2026-10-10 pair pages measured 2.2–3.3 s on revision 00006. After the UI redesign (revision 00009), a 50-request warm sample measured client-side p95 0.20 s for a pair page and 0.38 s for `/`. The cause of the earlier slowness was not isolated, and a cold start (min-instances 0 outside the review window, D-101) is slower.
- **No operator status page.** `/status` (M6 P9) was not built; collector health is on `/admin` only.
- **The LLM verifier has not run in production**, and `LLM_ENABLED` does not gate it; only the missing key does (status.md Q-11).
- **The home page lists the first 1,000 stored relationships of a round.** Fine for round 904, but locally the fixture round shows Routable 0.
- **Collection runs every 2 minutes**, not the 5 minutes FR-04 states (D-131). Spend through 2026-10-10 is $1.15 of the $50 cap, all GCP; the app's Anthropic spend is $0 because prod has no key (status.md, D-134).

## Recording

Backup walkthrough recording: (link added by M4-H1)

## What's next

1. **Third venue adapter.** Implement the `Adapter` interface (`internal/venues`) and register it. The canonical model, normaliser, matcher and router are already venue-independent.
2. **WebSocket streaming.** Replace the polling Scheduler with a collector that subscribes to venue feeds.
3. **The limits above:** a mutually-exclusive type, edge-rule extraction, a live back-test and a paginated matches list.

## Documentation

- [docs/equivalence.md](docs/equivalence.md): when two contracts are the same bet (definition, vetoes, facts, basis risk, confidence, current eval numbers).
- [docs/routing.md](docs/routing.md): how an order is routed (eligibility, cost formula, sweep, complements, worked examples).
- [docs/decisions.md](docs/decisions.md): every non-routine decision (D-001 to D-133), with why and how to reverse it.
- [docs/architecture.md](docs/architecture.md): components, data flow, data model, external services, tech stack.
- [docs/ai-usage.md](docs/ai-usage.md): runtime LLM use, development AI tools, spend.
- [docs/status.md](docs/status.md): requirements coverage with evidence, decisions awaiting veto, open questions.
- [/about](https://peak6-equinox-287867547175.us-central1.run.app/about) on the live site: the same story in plain language.
- Project context: [docs/brief.md](docs/brief.md), [docs/requirements.md](docs/requirements.md), [docs/domain.md](docs/domain.md), [docs/threat-model.md](docs/threat-model.md), [docs/plan.md](docs/plan.md), [docs/design.md](docs/design.md), [docs/environment.md](docs/environment.md).
- Rules for coding agents: [CLAUDE.md](CLAUDE.md).

<!-- OWNER: operator. -->
# Equinox

Cross-venue prediction-market aggregation and routing **simulation**, built for PEAK6 Labs. Equinox pulls markets and order books from Kalshi and Polymarket and normalises them into one venue-independent model. It classifies which contracts are economically the same bet, with a written definition and a contract diff, and it simulates routing a hypothetical order with a plain-English explanation. No real orders are ever placed.

## Setup

Requirements: Go 1.21 or newer, Docker with Compose, and Make. The Go toolchain is pinned in `go.mod` and downloaded automatically.

From a fresh clone, setup takes approximately 30 seconds to 1 minute on a typical development machine:

```bash
git clone https://labs.gauntletai.com/paawandesai/peak6-equinox.git
cd peak6-equinox
cp .env.example .env          # set ADMIN_PASSWORD (at least 24 characters)
make setup                    # install toolchain and tools (~5s)
make db-up                    # start Postgres in Docker (~10s)
make migrate                  # apply schema migrations (~2s)
make seed                     # load offline fixture data (~3s)
make match                    # or: equinox match — classify pairs (~5s)
make dev                      # start web server on http://localhost:8080
```

Open **http://localhost:8080** to see the matches list, pair diffs, and routing simulation. The `/health` endpoint confirms the service is up.

Run `make check` (typecheck + lint + test + coverage + security scans) before every push. See `make help` for all available targets.

Full environment details: [docs/environment.md](docs/environment.md)

## Architecture

Equinox is a **read-only simulation** — it pulls market data, classifies contracts, and simulates routing decisions, but never sends orders to venues. The architecture flows through five main stages:

1. **Adapters** (`internal/venues/kalshi`, `internal/venues/polymarket`) fetch instruments and quotes from venue APIs and transform them into a venue-independent canonical model (`internal/canonical`).
2. **Collector** (`internal/collector`) runs one fetch round every 5 minutes (Cloud Scheduler), storing raw responses and canonical data in Postgres.
3. **Normaliser** (`internal/normalize`) extracts terms (thresholds, dates, outcomes) from each contract's title and rules, categorised by type (crypto, Fed, politics). Unparseable terms are recorded as `unknown`.
4. **Matcher** (`internal/match`) compares every contract pair within a round: applies blocking rules, hard vetoes, soft checks, then classifies the relationship (`equivalent`, `equivalent_given_facts`, `complement`, `implies`, `related`). It consults facts (`config/facts.yaml`), produces a contract diff, and scores basis risk. An optional LLM verifier (admin-only, downgrade-only) can review classification but never upgrades a relationship.
5. **Router** (`internal/route`) simulates routing a hypothetical order: checks eligibility (staleness, skew, depth), computes cost (VWAP + fees + basis penalty), performs a greedy sweep across venues, and optionally constructs complement routes. The decision trace explains each step and records the chosen venue (or why the order was not routed).
6. **Web** (`internal/web`) serves all public and admin pages: matches list, pair diffs, routing form, decision log, and evaluation metrics. Pages read from the pinned round (demo mode) if one is set, otherwise the latest successful round.

The LLM verifier (`internal/llm`) is admin-only, downgrade-only (it can only confirm or weaken a relationship, never strengthen it), and operates under a daily API call cap. All LLM verdicts are logged and displayed on pair pages and in the decision log.

**Full details:** [docs/architecture.md](docs/architecture.md)

## 60-second demo walkthrough

Follow these numbered steps to see the full classification → routing → decision pipeline. Each step corresponds to a page on the live URL (paths shown below).

1. **Matches list** (`/`) — See all cross-venue contract pairs from the current round, grouped by relationship type (Routable, Implies, Related). The "Routable" section shows pairs where routing is possible: `equivalent`, `equivalent_given_facts`, and `complement`. Pick a routable pair (e.g., the Fed demo pair) to explore.

2. **Pair diff** (`/pairs/{id}`) — View two contracts side-by-side with their normalised terms, vetoes (hard blocks that prevent routing), facts consulted (for `equivalent_given_facts`), and basis risk score. The diff table highlights what differs: thresholds, dates, outcomes. The confidence score and classification method (rules-based or LLM-verified) appear at the top.

3. **Routing form** (`/pairs/{id}#route`) — Submit a hypothetical order (direction: BUY/SELL, side: YES/NO, size: 100 contracts, order type: MARKET/LIMIT, time in force: IOC/GTC). Click "Route this order" to simulate the routing decision.

4. **Decision trace** (`/pairs/{id}#trace` after form submission) — See the router's step-by-step explanation: eligibility checks (quotes must be fresh, not stale; spread must be narrow enough; book depth must support the size), cost calculation (VWAP + venue fees + basis penalty), sweep across venues (single venue, split, or complement), and the final decision (chosen venue with fill price and total cost, or "not routed" with a reason like "no fresh quotes" or "spread too wide").

5. **Decision log** (`/log`) — Browse all routing decisions ever made, with filters by relationship type, routed/not-routed, and date range. Each row links to the decision detail page showing the full trace.

6. **Evaluation** (`/eval`) — Review the matcher's precision and recall on the golden label set (human-confirmed pairs), the settlement back-test results (how well the matcher's predictions align with actual contract resolutions), and the confidence distribution across relationship types.

**Note:** The walkthrough above describes the final UI (post-UI-v2 tickets #43, #44, #45). On-screen wording may differ slightly on earlier revisions; this walkthrough will be refreshed after those tickets merge.

## Live demo

**https://equinox-o5hdimw5rq-uc.a.run.app**

The live service is deployed on Cloud Run (us-central1) with Cloud SQL Postgres. A Cloud Scheduler job runs one fetch round against Kalshi and Polymarket every 5 minutes. The demo is **read-only and simulated** — no real orders are ever placed.

**Pinned demo round:** Round 537 as of 2026-10-08 17:46:04 EDT (to be updated by M4-03 after the matcher fix #39 merges and deploys). Pages serve from this stored round, so the demo is stable and makes zero venue API calls (confirmed by NFR-04 test).

Health check: [/health](https://equinox-o5hdimw5rq-uc.a.run.app/health) (Cloud Run reserves `/healthz`, see D-052).

## Recording

Backup walkthrough recording: (link added by M4-H1)

## What's next

Three natural extension points for Equinox:

1. **Third venue adapter:** Add Polymarket US elections as a third adapter by implementing the `Adapter` interface (`internal/venues`) and registering it in the adapter registry. The canonical model, normaliser, matcher, and router are already venue-independent.

2. **WebSocket streaming:** Replace the polling Scheduler with a real-time collector that subscribes to venue WebSocket feeds. The collector interface (`internal/collector`) is designed to support both batch (current) and streaming modes.

3. **BigQuery export:** Archive all rounds, relationships, and decisions to BigQuery for long-term analysis and compliance reporting. The store interface (`internal/store`) is abstracted to support multiple backends; a BigQuery writer would implement the same contract and run in parallel with Postgres.

## Documentation

Complete documentation is in the `docs/` directory:

- **Architecture and design:**
  - [docs/architecture.md](docs/architecture.md) — Components, data flow, interfaces, tech stack
  - [docs/design.md](docs/design.md) — UI design system, typography, color tokens, layout
  - [docs/equivalence.md](docs/equivalence.md) — How two contracts are classified as the same bet (vetoes, facts, basis risk, confidence)
  - [docs/routing.md](docs/routing.md) — How orders are routed across venues (eligibility, cost formula, sweep, complement routes)
  - [docs/ai-usage.md](docs/ai-usage.md) — Runtime LLM use (verifier) and development AI tools

- **Project context:**
  - [docs/brief.md](docs/brief.md) — Problem statement, goals, constraints, deliverables
  - [docs/requirements.md](docs/requirements.md) — Functional requirements (FR-01..FR-28), non-functional requirements (NFR-01..NFR-16), user stories (US-01..US-12)
  - [docs/domain.md](docs/domain.md) — Domain concepts: venues, instruments, terms, relationships, routing
  - [docs/status.md](docs/status.md) — Current milestone progress, requirements coverage, blockers, decisions awaiting review

- **Process and decisions:**
  - [docs/decisions.md](docs/decisions.md) — All significant decisions (D-001..D-102+), with rationale and reversal steps
  - [docs/environment.md](docs/environment.md) — Environment variables, local setup, deployment
  - [docs/threat-model.md](docs/threat-model.md) — Attack surfaces and abuse cases (AB-01..AB-15)

- **Development:**
  - [CLAUDE.md](CLAUDE.md) — Coding rules for AI agents (stack, conventions, testing, git workflow, definition of done)
  - [docs/plan.md](docs/plan.md) — Milestone breakdown (M1..M4), scope, exit criteria

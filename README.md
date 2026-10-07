<!-- OWNER: operator. -->
# Equinox

Cross-venue prediction-market aggregation and routing **simulation**, built for PEAK6 Labs. Equinox pulls markets and order books from Kalshi and Polymarket and normalises them into one venue-independent model. It classifies which contracts are economically the same bet, with a written definition and a contract diff, and it simulates routing a hypothetical order with a plain-English explanation. No real orders are ever placed.

- Why and what: [docs/brief.md](docs/brief.md)
- Requirements: [docs/requirements.md](docs/requirements.md)
- Architecture: [docs/architecture.md](docs/architecture.md)
- Decisions log: [docs/decisions.md](docs/decisions.md)

## Live demo

**https://equinox-o5hdimw5rq-uc.a.run.app** (health check: [/health](https://equinox-o5hdimw5rq-uc.a.run.app/health); Cloud Run reserves `/healthz`, see D-052). Cloud Run + Cloud SQL in `us-central1`; a Cloud Scheduler job runs one fetch round against Kalshi and Polymarket every 5 minutes (D-016). It is a simulation: nothing is ever sent to a venue.

What to click:
1. [/](https://equinox-o5hdimw5rq-uc.a.run.app/) lists the current round's cross-venue pairs grouped by relationship type, strongest first; pick a pair.
2. `/pairs/{id}` shows the two contracts side by side with the diff and the vetoes, the touch prices, and a routing form: submit a hypothetical order to see the fee-aware route and its explanation.
3. `/admin` (basic auth, operator only) lists the collector's recent rounds with per-venue counts and status, and runs a round on demand.

## Quickstart (local)

Requires Go 1.21 or newer (`go.mod` pins the toolchain, which Go downloads automatically), Docker with Compose, and Make.

```bash
cp .env.example .env      # set ADMIN_PASSWORD (at least 24 characters)
make setup && make db-up && make migrate
make seed                 # loads an offline fixture round
make dev                  # http://localhost:8080 (health check: /healthz)
make check                # typecheck + lint + test + cover + scan, before every push
```

`make seed` loads the recorded fixture round, so the pages have data offline; `equinox match` then classifies its pairs. `make db-down` stops Postgres. Run `make help` to list all targets.

Full environment details are in [docs/environment.md](docs/environment.md).

## Demo walkthrough

_Filled in as milestones ship:_
- M1: crypto pair → diff → route
- M2: Fed pair given facts, plus a near-miss, plus the eval
- M3: basis-aware routing with a split and a complement route
- M4: the 60-second story and the recording

## Docs to come

- `docs/equivalence.md` (M2)
- `docs/routing.md` (M3)
- [docs/ai-usage.md](docs/ai-usage.md) (shipped: runtime LLM use and development AI use)

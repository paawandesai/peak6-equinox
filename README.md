<!-- OWNER: operator. -->
# Equinox

Cross-venue prediction-market aggregation and routing **simulation**, built for PEAK6 Labs. Equinox pulls markets and order books from Kalshi and Polymarket and normalises them into one venue-independent model. It classifies which contracts are economically the same bet, with a written definition and a contract diff, and it simulates routing a hypothetical order with a plain-English explanation. No real orders are ever placed.

- Why and what: [docs/brief.md](docs/brief.md)
- Requirements: [docs/requirements.md](docs/requirements.md)
- Architecture: [docs/architecture.md](docs/architecture.md)
- Decisions log: [docs/decisions.md](docs/decisions.md)

## Live demo

_URL added in M1-02._

## Quickstart (local)

Requires Go 1.21 or newer (`go.mod` pins the toolchain, which Go downloads automatically), Docker with Compose, and Make.

```bash
cp .env.example .env      # set ADMIN_PASSWORD (at least 24 characters)
make setup && make db-up && make migrate
make seed                 # loads an offline fixture round
make dev                  # http://localhost:8080 (health check: /healthz)
make check                # typecheck + lint + test + cover + scan, before every push
```

Until their tickets land, `make migrate` and `make seed` print "not implemented yet (ticket Mx-yy)" and exit 0. `make db-down` stops Postgres. Run `make help` to list all targets.

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
- `docs/ai-usage.md` (M1 onward)

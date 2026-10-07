<!-- OWNER: shared. Operator may update to match reality; every change is logged in docs/decisions.md. -->
# Environment

## Environment variables

| Name | Purpose | Required | Example |
|---|---|---|---|
| `APP_ENV` | `local`, `ci`, `staging`, or `prod` | yes | `local` |
| `PORT` | HTTP listen port (Cloud Run sets 8080) | no (default 8080) | `8080` |
| `DATABASE_URL` | Postgres DSN | yes | `postgres://equinox:equinox@localhost:5432/equinox?sslmode=disable` |
| `DATABASE_URL_TEST` | Postgres DSN for tests (each package gets its own schema) | tests only | `postgres://equinox:equinox@localhost:5432/equinox_test?sslmode=disable` |
| `ADMIN_USER` | Basic-auth user for `/admin/*` | yes | `admin` |
| `ADMIN_PASSWORD` | Basic-auth password, at least 24 random characters | yes | `change-me-local-only-000000000` |
| `KALSHI_BASE_URL` | Kalshi public API base | no | `https://api.elections.kalshi.com/trade-api/v2` |
| `POLYMARKET_GAMMA_URL` | Polymarket metadata API base | no | `https://gamma-api.polymarket.com` |
| `POLYMARKET_CLOB_URL` | Polymarket order-book API base | no | `https://clob.polymarket.com` |
| `UNIVERSE_DIR` | Directory with one universe file per venue (series and tags per category) | no | `config/universe` |
| `FACTS_FILE` | External facts | no | `config/facts.yaml` |
| `EVAL_SUMMARY_FILE` | JSON summary written by `equinox eval` and shown read-only on `/eval` (absent = "no eval yet") | no | `config/eval_summary.json` |
| `HTTP_TIMEOUT_SECONDS` | Per-request timeout to venues | no | `10` |
| `MAX_BODY_BYTES` | Venue response cap | no | `10485760` |
| `STALE_AFTER_SECONDS` | Quote staleness limit (live) | no | `120` |
| `SKEW_WARN_SECONDS` | Cross-venue skew warning | no | `5` |
| `SKEW_MAX_SECONDS` | Cross-venue skew exclusion | no | `30` |
| `MIN_ROUTE_CONFIDENCE` | Minimum relationship confidence for routing | no | `0.9` |
| `MIN_BOOK_DEPTH` | Minimum total book depth (contracts) for eligibility | no | `1` |
| `PUBLIC_RATE_LIMIT_PER_MIN` | Per-IP limit on public routes | no | `60` |
| `LLM_ENABLED` | Enables the verifier | no (default `false`) | `false` |
| `ANTHROPIC_API_KEY` | Anthropic key; the verifier is disabled if empty | only if `LLM_ENABLED=true` | `sk-ant-REPLACE_ME` |
| `LLM_MODEL` | Model ID | no | `claude-haiku-4-5-20251001` |
| `LLM_DAILY_CALL_CAP` | Maximum LLM calls per UTC day | no | `200` |
| `LLM_MAX_OUTPUT_TOKENS` | Per-call output cap | no | `400` |
| `LOG_LEVEL` | `debug`, `info`, `warn`, or `error` | no | `info` |

Venue base URLs are overridden in tests to point at `httptest` fixture servers. For offline development, `make dev-fixtures` serves the recorded fixtures on port 8090; point the app at it with `KALSHI_BASE_URL=http://127.0.0.1:8090/kalshi`, `POLYMARKET_GAMMA_URL=http://127.0.0.1:8090/polymarket/gamma` and `POLYMARKET_CLOB_URL=http://127.0.0.1:8090/polymarket/clob` (the command prints these). `make seed` does the same in-process, so it needs no running fixture server.

## Where real secrets live

- **Prod and staging:** GCP Secret Manager in `peak6-equinox-510816`. The secrets `equinox-database-url` (`equinox-staging-database-url` for staging), `equinox-admin-password` and `equinox-anthropic-key` are mounted as env on Cloud Run by `make deploy` and `make deploy-staging`. The Anthropic key is mounted only once it has a version (D-053). Only the human creates or rotates them, using `scripts/gcp-bootstrap.sh`.
- **Local:** `.env`, which is gitignored and never committed.
- **CI:** none. CI uses the Postgres service with throwaway credentials and never needs real secrets.
- **Spend limits:** the human sets a monthly limit in the Anthropic console and a $50 GCP budget alert, both manual steps listed in `scripts/gcp-bootstrap.sh`.

## Local services and ports

| Service | Port | Started by |
|---|---|---|
| Postgres 16 (`docker-compose.yml`, db `equinox` and `equinox_test`, user and password `equinox`) | 5432 | `make db-up` |
| Equinox web | 8080 | `make dev` |
| Fixture venue server (tests and `make dev-fixtures` only) | 8090 | `make dev-fixtures` |

## Seed and test data

- `testdata/fixtures/kalshi/*.json` and `testdata/fixtures/polymarket/*.json` hold real public responses, recorded once with `equinox record-fixtures` (a dev command that calls the live public APIs and is never run in tests). There is no personal data in them.
- `make seed` loads one fixture round into the local DB, so pages work offline.
- `testdata/golden/labels.yaml` is human-confirmed (see [plan.md](plan.md) M2).

## Run the full system locally

```bash
cp .env.example .env          # edit ADMIN_PASSWORD
make setup && make db-up && make migrate
make seed                     # offline fixture round
make dev                      # http://localhost:8080
# optional, live public data:
go run ./cmd/equinox ingest   # one live fetch round
```

## Staging

Staging is a Cloud Run revision tagged `staging` with no traffic (`make deploy-staging`). It uses the same project, a separate database `equinox_staging` on the same Cloud SQL instance, and `LLM_ENABLED=false`. Red-team and performance checks run only here or locally (see [threat-model.md](threat-model.md#red-team-rules-of-engagement)).

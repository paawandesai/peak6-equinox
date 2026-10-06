-- Initial schema: every table in docs/architecture.md § Data model.
-- Times are timestamptz (stored UTC). Prices and confidences are numeric in [0,1].

-- +goose Up

-- One concurrent fetch of both venues. status is 'running' until the round finishes.
CREATE TABLE fetch_rounds (
    id           bigserial   PRIMARY KEY,
    started_at   timestamptz NOT NULL,
    finished_at  timestamptz,
    status       text        NOT NULL DEFAULT 'running'
                 CHECK (status IN ('running', 'ok', 'partial', 'failed')),
    venue_status jsonb       NOT NULL DEFAULT '{}'::jsonb,
    pinned       boolean     NOT NULL DEFAULT false
);
-- At most one pinned round.
CREATE UNIQUE INDEX fetch_rounds_one_pinned ON fetch_rounds (pinned) WHERE pinned;
CREATE INDEX fetch_rounds_status_id ON fetch_rounds (status, id DESC);

-- Raw venue response bodies, gzipped, kept for audit and re-processing.
CREATE TABLE raw_responses (
    id          bigserial   PRIMARY KEY,
    round_id    bigint      NOT NULL REFERENCES fetch_rounds (id) ON DELETE CASCADE,
    venue       text        NOT NULL CHECK (venue IN ('kalshi', 'polymarket')),
    endpoint    text        NOT NULL,
    http_status integer     NOT NULL,
    body_gz     bytea       NOT NULL,
    fetched_at  timestamptz NOT NULL
);
CREATE INDEX raw_responses_round ON raw_responses (round_id);

-- Canonical instruments, upserted each round. id is "<venue>:<key>".
CREATE TABLE instruments (
    id               text        PRIMARY KEY CHECK (id ~ '^(kalshi|polymarket):[A-Za-z0-9][A-Za-z0-9._-]{0,127}$'),
    venue            text        NOT NULL CHECK (venue IN ('kalshi', 'polymarket')),
    venue_market_id  text        NOT NULL,
    venue_event_id   text        NOT NULL DEFAULT '',
    category         text        NOT NULL CHECK (category IN ('crypto', 'fed', 'politics')),
    title            text        NOT NULL,
    rules_text       text        NOT NULL DEFAULT '',
    terms            jsonb       NOT NULL,
    polarity         text        NOT NULL CHECK (polarity IN ('positive', 'negative', 'unknown')),
    close_time       timestamptz,
    expiration_time  timestamptz,
    status           text        NOT NULL CHECK (status IN ('open', 'closed', 'settled', 'unknown')),
    fee_schedule     jsonb       NOT NULL,
    quality          jsonb       NOT NULL DEFAULT '[]'::jsonb,
    first_seen_round bigint      NOT NULL REFERENCES fetch_rounds (id),
    last_seen_round  bigint      NOT NULL REFERENCES fetch_rounds (id),
    CHECK (split_part(id, ':', 1) = venue)
);
CREATE INDEX instruments_venue_category ON instruments (venue, category);
CREATE INDEX instruments_last_seen ON instruments (last_seen_round);

-- One YES-side book per instrument per round.
CREATE TABLE quotes (
    round_id      bigint      NOT NULL REFERENCES fetch_rounds (id) ON DELETE CASCADE,
    instrument_id text        NOT NULL REFERENCES instruments (id),
    yes_bids      jsonb       NOT NULL,
    yes_asks      jsonb       NOT NULL,
    best_bid      numeric     CHECK (best_bid BETWEEN 0 AND 1),
    best_ask      numeric     CHECK (best_ask BETWEEN 0 AND 1),
    mid           numeric     CHECK (mid BETWEEN 0 AND 1),
    fetched_at    timestamptz NOT NULL,
    venue_ts      timestamptz,
    quality       jsonb       NOT NULL DEFAULT '[]'::jsonb,
    PRIMARY KEY (round_id, instrument_id)
);
CREATE INDEX quotes_instrument ON quotes (instrument_id, round_id DESC);

-- Classified pairs per round. Every row carries an explanation.
CREATE TABLE relationships (
    id          bigserial PRIMARY KEY,
    round_id    bigint    NOT NULL REFERENCES fetch_rounds (id) ON DELETE CASCADE,
    a_id        text      NOT NULL REFERENCES instruments (id),
    b_id        text      NOT NULL REFERENCES instruments (id),
    type        text      NOT NULL CHECK (type IN ('equivalent', 'equivalent_given_facts', 'complement',
                                                    'implies', 'related', 'unrelated')),
    confidence  numeric   NOT NULL CHECK (confidence BETWEEN 0 AND 1),
    method      text      NOT NULL CHECK (method IN ('rules', 'rules+llm')),
    diff        jsonb     NOT NULL DEFAULT '[]'::jsonb,
    vetoes      jsonb     NOT NULL DEFAULT '[]'::jsonb,
    facts_used  jsonb     NOT NULL DEFAULT '[]'::jsonb,
    basis_risk  jsonb,
    explanation text      NOT NULL CHECK (btrim(explanation) <> ''),
    UNIQUE (round_id, a_id, b_id),
    CHECK (a_id <> b_id)
);
CREATE INDEX relationships_round_type ON relationships (round_id, type);
CREATE INDEX relationships_b ON relationships (round_id, b_id);

-- LLM verdict cache, keyed by SHA-256 of both instruments' text + prompt version + model.
CREATE TABLE llm_verdicts (
    cache_key      text        PRIMARY KEY CHECK (cache_key ~ '^[0-9a-f]{64}$'),
    model          text        NOT NULL,
    prompt_version text        NOT NULL,
    verdict        jsonb       NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now()
);

-- LLM spend per UTC day, for the daily call cap.
CREATE TABLE llm_usage (
    day           date    PRIMARY KEY,
    calls         integer NOT NULL DEFAULT 0 CHECK (calls >= 0),
    input_tokens  bigint  NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens bigint  NOT NULL DEFAULT 0 CHECK (output_tokens >= 0)
);

-- Simulated routing decisions ("order" is quoted: ORDER is a reserved word).
CREATE TABLE routing_decisions (
    id         bigserial   PRIMARY KEY,
    round_id   bigint      NOT NULL REFERENCES fetch_rounds (id) ON DELETE CASCADE,
    "order"    jsonb       NOT NULL,
    decision   jsonb       NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX routing_decisions_round ON routing_decisions (round_id);

-- Append-only audit log of relationship, routing, LLM and ingest decisions.
CREATE TABLE decision_log (
    id         bigserial   PRIMARY KEY,
    kind       text        NOT NULL CHECK (kind IN ('relationship', 'routing', 'llm', 'ingest')),
    ref        text        NOT NULL DEFAULT '',
    summary    text        NOT NULL CHECK (btrim(summary) <> ''),
    payload    jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX decision_log_kind_created ON decision_log (kind, created_at DESC, id DESC);
CREATE INDEX decision_log_ref ON decision_log (ref);

-- +goose Down
DROP TABLE decision_log;
DROP TABLE routing_decisions;
DROP TABLE llm_usage;
DROP TABLE llm_verdicts;
DROP TABLE relationships;
DROP TABLE quotes;
DROP TABLE instruments;
DROP TABLE raw_responses;
DROP TABLE fetch_rounds;

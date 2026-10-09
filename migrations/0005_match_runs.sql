-- Add match_runs table to persist match run history for the Admin Matcher health row
-- (UI-26 phase 1: issue #58). Retention is capped at 500 rows.

-- +goose Up

-- One row per match run (manual or scheduled).
CREATE TABLE match_runs (
    id                   bigserial   PRIMARY KEY,
    round_id             bigint      NOT NULL REFERENCES fetch_rounds (id) ON DELETE CASCADE,
    started_at           timestamptz NOT NULL,
    finished_at          timestamptz NOT NULL,
    duration_ms          integer     NOT NULL CHECK (duration_ms >= 0),
    pairs_compared       integer     NOT NULL CHECK (pairs_compared >= 0),
    relationships_stored integer     NOT NULL CHECK (relationships_stored >= 0),
    outcome              text        NOT NULL CHECK (outcome IN ('ok', 'failed'))
);

CREATE INDEX match_runs_finished_desc ON match_runs (finished_at DESC);

-- +goose Down
DROP TABLE match_runs;

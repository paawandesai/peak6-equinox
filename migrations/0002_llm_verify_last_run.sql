-- Add llm_verify_last_run table to persist the last LLM verification run summary
-- (UI-08 follow-up: issue #40). This is a singleton table with at most one row.

-- +goose Up

-- One row holding the last LLM verification run summary.
CREATE TABLE llm_verify_last_run (
    id            boolean     PRIMARY KEY DEFAULT true CHECK (id = true),
    verified      integer     NOT NULL CHECK (verified >= 0),
    downgraded    integer     NOT NULL CHECK (downgraded >= 0),
    cache_hits    integer     NOT NULL CHECK (cache_hits >= 0),
    ran_at        timestamptz NOT NULL
);

-- +goose Down
DROP TABLE llm_verify_last_run;

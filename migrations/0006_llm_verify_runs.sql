-- Add llm_verify_runs table to persist LLM verification run history with bounded retention,
-- and link verified relationships to their verdicts (UI-22, issue #54).

-- +goose Up

-- LLM verification runs with bounded retention (last 100 runs).
CREATE TABLE llm_verify_runs (
    id             bigserial   PRIMARY KEY,
    started_at     timestamptz NOT NULL,
    finished_at    timestamptz,
    model          text        NOT NULL,
    prompt_version text        NOT NULL,
    verified       integer     NOT NULL DEFAULT 0 CHECK (verified >= 0),
    downgraded     integer     NOT NULL DEFAULT 0 CHECK (downgraded >= 0),
    cache_hits     integer     NOT NULL DEFAULT 0 CHECK (cache_hits >= 0),
    calls_made     integer     NOT NULL DEFAULT 0 CHECK (calls_made >= 0),
    input_tokens   bigint      NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens  bigint      NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    outcome        text        NOT NULL CHECK (outcome IN ('ok', 'cap_reached', 'error'))
);
CREATE INDEX llm_verify_runs_started ON llm_verify_runs (started_at DESC, id DESC);

-- Link verified relationships to their LLM verdicts.
-- llm_cache_key references llm_verdicts.cache_key when the relationship was verified by LLM.
-- llm_run_id and llm_verified_at track when verification happened.
ALTER TABLE relationships
ADD COLUMN llm_cache_key text,
ADD COLUMN llm_run_id bigint REFERENCES llm_verify_runs (id),
ADD COLUMN llm_verified_at timestamptz;

CREATE INDEX relationships_llm_cache ON relationships (llm_cache_key) WHERE llm_cache_key IS NOT NULL;
-- Note: llm_cache_key is not a FK to llm_verdicts because the cache may be pruned,
-- but we still want to keep the relationship's link to show which verdict was used.

-- +goose Down
DROP INDEX relationships_llm_cache;
ALTER TABLE relationships
DROP COLUMN llm_verified_at,
DROP COLUMN llm_run_id,
DROP COLUMN llm_cache_key;

DROP INDEX llm_verify_runs_started;
DROP TABLE llm_verify_runs;

-- Add index on quotes (instrument_id, fetched_at DESC) to speed up per-instrument
-- history queries (M5-02, issue #84).

-- +goose Up

CREATE INDEX IF NOT EXISTS quotes_instrument_fetched ON quotes (instrument_id, fetched_at DESC);

-- +goose Down

DROP INDEX IF EXISTS quotes_instrument_fetched;

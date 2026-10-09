-- +goose Up
-- Add rule layers columns to instruments table (UI-21, D-103)
ALTER TABLE instruments
    ADD COLUMN rules_summary TEXT NOT NULL DEFAULT '',
    ADD COLUMN clarifications TEXT NOT NULL DEFAULT '',
    ADD COLUMN clarifications_updated_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE instruments
    DROP COLUMN clarifications_updated_at,
    DROP COLUMN clarifications,
    DROP COLUMN rules_summary;

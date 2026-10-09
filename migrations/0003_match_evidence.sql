-- +goose Up
-- Add matcher evidence column to relationships table (UI-20, D-110)
ALTER TABLE relationships
    ADD COLUMN evidence jsonb;

COMMENT ON COLUMN relationships.evidence IS 'Matcher confidence calculation: prior, per-term contributions, total, and pre-veto score for near misses (UI-20, D-110). NULL for relationships stored before this feature.';

-- +goose Down
ALTER TABLE relationships
    DROP COLUMN evidence;

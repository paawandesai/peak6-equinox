-- Add complement_given_facts relationship type to the relationships.type CHECK constraint.
-- This type represents pairs that are strict complements only given external facts
-- (D-107, Tier 2 schema change: row 19 of golden labels, "cut before 2027" vs
-- "no cuts in 2026", human-confirmed by Paawan Desai 2026-10-09).

-- +goose Up

-- Drop the existing CHECK constraint on relationships.type
ALTER TABLE relationships DROP CONSTRAINT IF EXISTS relationships_type_check;

-- Add the new CHECK constraint including complement_given_facts
ALTER TABLE relationships ADD CONSTRAINT relationships_type_check
    CHECK (type IN ('equivalent', 'equivalent_given_facts', 'complement',
                    'complement_given_facts', 'implies', 'related', 'unrelated'));

-- +goose Down

-- Drop the new CHECK constraint
ALTER TABLE relationships DROP CONSTRAINT IF EXISTS relationships_type_check;

-- Restore the original CHECK constraint without complement_given_facts
ALTER TABLE relationships ADD CONSTRAINT relationships_type_check
    CHECK (type IN ('equivalent', 'equivalent_given_facts', 'complement',
                    'implies', 'related', 'unrelated'));

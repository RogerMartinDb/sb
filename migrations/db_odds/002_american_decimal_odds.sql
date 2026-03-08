-- db_odds: migrate from fractional (num/den) to American + Decimal odds

BEGIN;

DROP VIEW IF EXISTS current_odds;

ALTER TABLE odds DROP COLUMN IF EXISTS offered_num;
ALTER TABLE odds DROP COLUMN IF EXISTS offered_den;
ALTER TABLE odds ADD COLUMN offered_decimal NUMERIC(10,4) NOT NULL DEFAULT 1.0;
ALTER TABLE odds ADD COLUMN offered_american INT NOT NULL DEFAULT 0;

-- Recreate the current_odds view with new columns.
CREATE OR REPLACE VIEW current_odds AS
SELECT DISTINCT ON (market_id, selection_id)
    market_id, selection_id, offered_decimal, offered_american, source_offset, updated_at
FROM odds
ORDER BY market_id, selection_id, updated_at DESC;

COMMIT;

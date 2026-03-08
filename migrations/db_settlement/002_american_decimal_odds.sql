-- db_settlement: migrate from fractional (num/den) to American + Decimal odds

BEGIN;

ALTER TABLE pending_bets DROP COLUMN IF EXISTS odds_num;
ALTER TABLE pending_bets DROP COLUMN IF EXISTS odds_den;
ALTER TABLE pending_bets ADD COLUMN odds_decimal NUMERIC(10,4) NOT NULL DEFAULT 1.0;
ALTER TABLE pending_bets ADD COLUMN odds_american INT NOT NULL DEFAULT 0;

COMMIT;

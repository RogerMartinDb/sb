-- db_bet_history: add human-readable market and selection names
BEGIN;
ALTER TABLE bets ADD COLUMN IF NOT EXISTS market_name    TEXT NOT NULL DEFAULT '';
ALTER TABLE bets ADD COLUMN IF NOT EXISTS selection_name TEXT NOT NULL DEFAULT '';
COMMIT;

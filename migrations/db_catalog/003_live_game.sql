-- db_catalog: add live game state columns to events table

BEGIN;

ALTER TABLE events
    ADD COLUMN IF NOT EXISTS home_score  INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS away_score  INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS game_period TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS game_clock  TEXT NOT NULL DEFAULT '';

COMMIT;

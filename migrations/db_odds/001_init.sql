-- db_odds: current and historical offered odds

BEGIN;

CREATE TABLE IF NOT EXISTS odds (
    id              BIGSERIAL   PRIMARY KEY,
    market_id       TEXT        NOT NULL,
    selection_id    TEXT        NOT NULL,
    offered_num     BIGINT      NOT NULL,
    offered_den     BIGINT      NOT NULL CHECK (offered_den > 0),
    source_offset   BIGINT      NOT NULL,  -- Kafka offset of the triggering event
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Latest odds per market/selection (for reads).
CREATE INDEX IF NOT EXISTS idx_odds_market_selection_updated
    ON odds (market_id, selection_id, updated_at DESC);

-- View: current odds (most recent row per market+selection).
CREATE OR REPLACE VIEW current_odds AS
SELECT DISTINCT ON (market_id, selection_id)
    market_id, selection_id, offered_num, offered_den, source_offset, updated_at
FROM odds
ORDER BY market_id, selection_id, updated_at DESC;

COMMIT;

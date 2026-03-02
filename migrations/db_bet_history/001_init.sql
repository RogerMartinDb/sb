-- db_bet_history: append-only read model, partitioned by placed_at month

BEGIN;

-- Parent table — partitioned by placed_at month.
CREATE TABLE IF NOT EXISTS bets (
    bet_id        TEXT        NOT NULL,
    user_id       TEXT        NOT NULL,
    market_id     TEXT        NOT NULL,
    selection_id  TEXT        NOT NULL,
    odds_num      BIGINT      NOT NULL,
    odds_den      BIGINT      NOT NULL,
    stake_minor   BIGINT      NOT NULL,
    currency      TEXT        NOT NULL,
    status        TEXT        NOT NULL DEFAULT 'PLACED'
                              CHECK (status IN ('PLACED','SETTLED_WIN','SETTLED_LOSS','VOID')),
    placed_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    settled_at    TIMESTAMPTZ,
    payout_minor  BIGINT,
    PRIMARY KEY (bet_id, placed_at)
) PARTITION BY RANGE (placed_at);

-- Create partitions for the current and next two months (extend monthly via cron).
CREATE TABLE IF NOT EXISTS bets_2026_03 PARTITION OF bets
    FOR VALUES FROM ('2026-03-01') TO ('2026-04-01');

CREATE TABLE IF NOT EXISTS bets_2026_04 PARTITION OF bets
    FOR VALUES FROM ('2026-04-01') TO ('2026-05-01');

CREATE TABLE IF NOT EXISTS bets_2026_05 PARTITION OF bets
    FOR VALUES FROM ('2026-05-01') TO ('2026-06-01');

-- "My Bets" query index (user + recency).
CREATE INDEX IF NOT EXISTS idx_bets_user_placed
    ON bets (user_id, placed_at DESC);

COMMIT;

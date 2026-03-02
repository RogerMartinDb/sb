-- db_settlement: pending and completed settlements

BEGIN;

-- Tracks bets that are awaiting settlement (read from bet.placed events).
CREATE TABLE IF NOT EXISTS pending_bets (
    bet_id        TEXT        PRIMARY KEY,
    user_id       TEXT        NOT NULL,
    market_id     TEXT        NOT NULL,
    selection_id  TEXT        NOT NULL,
    odds_num      BIGINT      NOT NULL,
    odds_den      BIGINT      NOT NULL,
    stake_minor   BIGINT      NOT NULL,
    currency      TEXT        NOT NULL,
    status        TEXT        NOT NULL DEFAULT 'PLACED'
                              CHECK (status IN ('PLACED','SETTLED','VOID')),
    placed_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_pending_bets_market
    ON pending_bets (market_id)
    WHERE status = 'PLACED';

-- Completed settlement records (idempotency on bet_id).
CREATE TABLE IF NOT EXISTS settlements (
    settlement_id  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    bet_id         TEXT        NOT NULL UNIQUE,
    outcome        TEXT        NOT NULL CHECK (outcome IN ('WIN','LOSS','VOID')),
    payout_minor   BIGINT      NOT NULL DEFAULT 0,
    settled_at     TIMESTAMPTZ NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMIT;

-- db_bet_acceptance: outbox table only (high write throughput)
-- PgBouncer: transaction pooling (no advisory locks needed here)

BEGIN;

CREATE TABLE IF NOT EXISTS outbox (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    bet_id         UUID        NOT NULL,
    topic          TEXT        NOT NULL,          -- e.g. 'bet.placed'
    partition_key  TEXT        NOT NULL,          -- market_id (drives Kafka partitioning)
    payload        JSONB       NOT NULL,
    status         TEXT        NOT NULL           -- PENDING | READY_TO_PUBLISH | PUBLISHED | CANCELLED
                               CHECK (status IN ('PENDING','READY_TO_PUBLISH','PUBLISHED','CANCELLED')),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at   TIMESTAMPTZ
);

-- Relay polls this index constantly.
CREATE INDEX IF NOT EXISTS idx_outbox_status_created
    ON outbox (status, created_at ASC)
    WHERE status = 'READY_TO_PUBLISH';

-- Lookup by bet_id (cancel flow).
CREATE INDEX IF NOT EXISTS idx_outbox_bet_id ON outbox (bet_id);

COMMIT;

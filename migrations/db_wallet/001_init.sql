-- db_wallet: double-entry ledger, ACID critical
-- PgBouncer: SESSION pooling (advisory locks required for balance serialisation)

BEGIN;

-- User balance rows (one per user).
CREATE TABLE IF NOT EXISTS balances (
    user_id              TEXT        PRIMARY KEY,
    available_minor      BIGINT      NOT NULL DEFAULT 0 CHECK (available_minor >= 0),
    bets_in_flight_minor BIGINT      NOT NULL DEFAULT 0 CHECK (bets_in_flight_minor >= 0),
    currency             TEXT        NOT NULL DEFAULT 'GBP',
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Per-user stake limits and KYC status.
CREATE TABLE IF NOT EXISTS user_limits (
    user_id                 TEXT        PRIMARY KEY,
    max_single_stake_minor  BIGINT      NOT NULL DEFAULT 100000,  -- £1000.00
    daily_limit_minor       BIGINT      NOT NULL DEFAULT 500000,  -- £5000.00
    currency                TEXT        NOT NULL DEFAULT 'GBP',
    kyc_status              TEXT        NOT NULL DEFAULT 'PENDING'
                                        CHECK (kyc_status IN ('PENDING','VERIFIED','REJECTED'))
);

-- Double-entry ledger. Immutable rows — never UPDATE except status transitions.
CREATE TABLE IF NOT EXISTS ledger_entries (
    transaction_id  TEXT        PRIMARY KEY,
    user_id         TEXT        NOT NULL REFERENCES balances(user_id),
    entry_type      TEXT        NOT NULL,   -- DEBIT, CREDIT_WIN, CREDIT_VOID, CREDIT_PUSH
    stake_minor     BIGINT      NOT NULL,
    currency        TEXT        NOT NULL,
    status          TEXT        NOT NULL DEFAULT 'PENDING_CONFIRMATION'
                                CHECK (status IN ('PENDING_CONFIRMATION','CONFIRMED','CANCELLED')),
    bet_id          TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    confirmed_at    TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_ledger_user_created
    ON ledger_entries (user_id, created_at DESC);

-- Reconciliation job uses this.
CREATE INDEX IF NOT EXISTS idx_ledger_pending_old
    ON ledger_entries (created_at ASC)
    WHERE status = 'PENDING_CONFIRMATION';

COMMIT;

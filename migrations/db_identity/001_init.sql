-- db_identity: users, credentials, KYC

BEGIN;

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS users (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    email          TEXT        NOT NULL UNIQUE,
    password_hash  TEXT        NOT NULL,  -- bcrypt
    kyc_status     TEXT        NOT NULL DEFAULT 'PENDING'
                               CHECK (kyc_status IN ('PENDING','VERIFIED','REJECTED')),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users (email);

-- OAuth2 / social login links.
CREATE TABLE IF NOT EXISTS oauth_identities (
    id          UUID  PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID  NOT NULL REFERENCES users(id),
    provider    TEXT  NOT NULL,  -- 'google', 'apple'
    external_id TEXT  NOT NULL,
    UNIQUE (provider, external_id)
);

-- Refresh token store (for token rotation).
CREATE TABLE IF NOT EXISTS refresh_tokens (
    token_hash   TEXT        PRIMARY KEY,
    user_id      UUID        NOT NULL REFERENCES users(id),
    expires_at   TIMESTAMPTZ NOT NULL,
    revoked      BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens (user_id);

COMMIT;

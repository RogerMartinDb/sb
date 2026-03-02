-- db_catalog: sports, competitions, events, markets, selections

BEGIN;

CREATE TABLE IF NOT EXISTS sports (
    sport_id    TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS competitions (
    competition_id  TEXT PRIMARY KEY,
    sport_id        TEXT NOT NULL REFERENCES sports(sport_id),
    name            TEXT NOT NULL,
    country         TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS events (
    event_id        TEXT        PRIMARY KEY,
    competition_id  TEXT        NOT NULL REFERENCES competitions(competition_id),
    name            TEXT        NOT NULL,
    starts_at       TIMESTAMPTZ NOT NULL,
    status          TEXT        NOT NULL DEFAULT 'SCHEDULED'
                                CHECK (status IN ('SCHEDULED','LIVE','FINISHED','CANCELLED')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS markets (
    market_id   TEXT        PRIMARY KEY,
    event_id    TEXT        NOT NULL REFERENCES events(event_id),
    name        TEXT        NOT NULL,
    status      TEXT        NOT NULL DEFAULT 'OPEN'
                            CHECK (status IN ('OPEN','SUSPENDED','CLOSED','SETTLED')),
    opens_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    closes_at   TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_markets_event_id ON markets (event_id);
CREATE INDEX IF NOT EXISTS idx_markets_status   ON markets (status) WHERE status = 'OPEN';

CREATE TABLE IF NOT EXISTS selections (
    selection_id  TEXT    PRIMARY KEY,
    market_id     TEXT    NOT NULL REFERENCES markets(market_id),
    name          TEXT    NOT NULL,
    active        BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE INDEX IF NOT EXISTS idx_selections_market ON selections (market_id);

COMMIT;

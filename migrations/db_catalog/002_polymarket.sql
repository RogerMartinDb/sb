-- db_catalog: add market_type, target_value, feed_probability for Polymarket integration

BEGIN;

ALTER TABLE markets
    ADD COLUMN IF NOT EXISTS market_type  TEXT CHECK (market_type IN ('ML', 'SPREAD', 'TOTAL')),
    ADD COLUMN IF NOT EXISTS target_value NUMERIC(10, 5) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS is_main      BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE selections
    ADD COLUMN IF NOT EXISTS target_value     NUMERIC(10, 5) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS feed_probability NUMERIC(6, 5)
                                              CHECK (feed_probability >= 0 AND feed_probability <= 1);

COMMIT;

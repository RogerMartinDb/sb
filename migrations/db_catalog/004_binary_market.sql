-- db_catalog: add BINARY market type for yes/no prediction markets (politics, etc.)

BEGIN;

ALTER TABLE markets
    DROP CONSTRAINT IF EXISTS markets_market_type_check;

ALTER TABLE markets
    ADD CONSTRAINT markets_market_type_check
        CHECK (market_type IN ('ML', 'SPREAD', 'TOTAL', 'BINARY'));

COMMIT;

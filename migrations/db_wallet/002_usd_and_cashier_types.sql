-- Migrate currency from GBP to USD
UPDATE balances SET currency = 'USD';
ALTER TABLE balances ALTER COLUMN currency SET DEFAULT 'USD';
UPDATE user_limits SET currency = 'USD';
ALTER TABLE user_limits ALTER COLUMN currency SET DEFAULT 'USD';

-- Add payment_method column for cashier audit trail
ALTER TABLE ledger_entries ADD COLUMN IF NOT EXISTS payment_method TEXT;

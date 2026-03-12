-- Update default stake limits: maxSingleStake $1,000 (100,000 minor), dailyLimit $10,000 (1,000,000 minor)
ALTER TABLE user_limits ALTER COLUMN max_single_stake_minor SET DEFAULT 100000;
ALTER TABLE user_limits ALTER COLUMN daily_limit_minor SET DEFAULT 1000000;

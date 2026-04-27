-- Rollback: 000044_add_investment_fund_full

DROP TABLE IF EXISTS core_banking.client_fund_transactions;
DROP TABLE IF EXISTS core_banking.fund_securities;

ALTER TABLE core_banking.fund_positions
    DROP COLUMN IF EXISTS last_changed;

ALTER TABLE core_banking.investment_funds
    DROP COLUMN IF EXISTS minimum_contribution,
    DROP COLUMN IF EXISTS liquid_assets,
    DROP COLUMN IF EXISTS account_id;

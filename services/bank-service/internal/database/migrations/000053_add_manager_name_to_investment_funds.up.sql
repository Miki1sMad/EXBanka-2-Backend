ALTER TABLE core_banking.investment_funds
    ADD COLUMN IF NOT EXISTS manager_name VARCHAR(255) NOT NULL DEFAULT '';

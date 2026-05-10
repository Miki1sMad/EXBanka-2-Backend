-- Adds fund_id column to orders so a supervisor can link a BUY order to an
-- investment fund. NULL means the order is for the bank's own account (normal flow).
ALTER TABLE core_banking.orders
    ADD COLUMN IF NOT EXISTS fund_id BIGINT NULL;

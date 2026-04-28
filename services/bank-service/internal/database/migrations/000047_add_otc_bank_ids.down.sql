-- =============================================================================
-- Migration: 000047_add_otc_bank_ids (DOWN)
-- =============================================================================

DROP INDEX IF EXISTS core_banking.idx_otc_contracts_buyer_bank;
DROP INDEX IF EXISTS core_banking.idx_otc_contracts_seller_bank;
DROP INDEX IF EXISTS core_banking.idx_otc_offers_buyer_bank;
DROP INDEX IF EXISTS core_banking.idx_otc_offers_seller_bank;

ALTER TABLE core_banking.otc_contracts
    DROP COLUMN IF EXISTS buyer_bank_id,
    DROP COLUMN IF EXISTS seller_bank_id;

ALTER TABLE core_banking.otc_offers
    DROP COLUMN IF EXISTS buyer_bank_id,
    DROP COLUMN IF EXISTS seller_bank_id;

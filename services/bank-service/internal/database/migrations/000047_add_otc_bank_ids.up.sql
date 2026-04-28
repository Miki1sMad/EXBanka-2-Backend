-- =============================================================================
-- Migration: 000047_add_otc_bank_ids (UP)
-- Faza 2 — forward-compat: priprema za inter-bank OTC trgovinu.
--
-- Trenutno se sve OTC operacije izvršavaju unutar jedne (naše) banke, pa će
-- ova polja u praksi biti NULL ili identifikator naše banke. Kada se kasnije
-- doda komunikacija sa drugim bankama, polja će razlikovati strane učesnice
-- bez potrebe za ponovnom migracijom postojećih redova.
-- =============================================================================

ALTER TABLE core_banking.otc_offers
    ADD COLUMN seller_bank_id BIGINT NULL,
    ADD COLUMN buyer_bank_id  BIGINT NULL;

ALTER TABLE core_banking.otc_contracts
    ADD COLUMN seller_bank_id BIGINT NULL,
    ADD COLUMN buyer_bank_id  BIGINT NULL;

-- Indeksi su korisni za buduće inter-bank upite (filter po banci učesnice).
CREATE INDEX IF NOT EXISTS idx_otc_offers_seller_bank
    ON core_banking.otc_offers (seller_bank_id);
CREATE INDEX IF NOT EXISTS idx_otc_offers_buyer_bank
    ON core_banking.otc_offers (buyer_bank_id);
CREATE INDEX IF NOT EXISTS idx_otc_contracts_seller_bank
    ON core_banking.otc_contracts (seller_bank_id);
CREATE INDEX IF NOT EXISTS idx_otc_contracts_buyer_bank
    ON core_banking.otc_contracts (buyer_bank_id);

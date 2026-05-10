-- =============================================================================
-- Migration: 000052_add_interbank_tables (DOWN)
-- =============================================================================

ALTER TABLE core_banking.transakcija
    DROP CONSTRAINT transakcija_tip_check,
    ADD CONSTRAINT transakcija_tip_check
        CHECK (tip_transakcije IN ('UPLATA', 'ISPLATA', 'INTERNI_TRANSFER', 'MENJACNICA'));

DROP TABLE IF EXISTS core_banking.interbank_option_contract;
DROP TABLE IF EXISTS core_banking.interbank_negotiation;
DROP TABLE IF EXISTS core_banking.interbank_reservation;
DROP TABLE IF EXISTS core_banking.interbank_transaction;
DROP TABLE IF EXISTS core_banking.interbank_message_log;

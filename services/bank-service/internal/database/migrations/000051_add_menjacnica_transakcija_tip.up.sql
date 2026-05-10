ALTER TABLE core_banking.transakcija
    DROP CONSTRAINT transakcija_tip_check,
    ADD CONSTRAINT transakcija_tip_check
        CHECK (tip_transakcije IN ('UPLATA', 'ISPLATA', 'INTERNI_TRANSFER', 'MENJACNICA'));

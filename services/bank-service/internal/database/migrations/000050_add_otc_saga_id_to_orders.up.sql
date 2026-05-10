-- Migration: 000050_add_otc_saga_id_to_orders (UP)
--
-- Dodaje otc_saga_execution_id kolonu u tabelu orders.
--
-- Problem koji rešava:
--   compensateTransferOwnership (SAGA korak 4 rollback) je prethodno identifikovao
--   sintetičke naloge po vremenskom prozoru od ±2 minute, što je heuristika koja
--   može greškom obrisati legitimne naloge ako postoji high-concurrency scenario
--   (dva korisnika trguju istim listingom u isto vreme).
--
-- Rešenje:
--   Svaki sintetički BUY/SELL nalog kreiran u stepTransferOwnership dobija
--   otc_saga_execution_id koji direktno referencira otc_saga_executions(id).
--   compensateTransferOwnership sada briše naloge po ovom egzaktnom ID-u,
--   bez ikakve vremenske heuristike.
--
-- NULL vrednost znači da nalog nije sintetički OTC nalog (normalan trading nalog).

ALTER TABLE core_banking.orders
    ADD COLUMN IF NOT EXISTS otc_saga_execution_id BIGINT NULL
        REFERENCES core_banking.otc_saga_executions(id) ON DELETE SET NULL;

-- Parcijalni indeks — pokriva samo sintetičke naloge (NULL redovi su isključeni),
-- što čuva veličinu indeksa i ubrzava rollback lookup.
CREATE INDEX IF NOT EXISTS idx_orders_otc_saga_execution_id
    ON core_banking.orders (otc_saga_execution_id)
    WHERE otc_saga_execution_id IS NOT NULL;

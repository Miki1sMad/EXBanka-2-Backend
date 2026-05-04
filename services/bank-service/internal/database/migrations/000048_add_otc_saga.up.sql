-- =============================================================================
-- Migration: 000048_add_otc_saga (UP)
-- Faza 4 (Celina 4): SAGA mehanizam za izvršavanje OTC ugovora.
--
-- Tabele:
--   otc_saga_executions — jedan red po pokušaju izvršavanja ugovora;
--                         čuva dokle je SAGA stigla i koliko je retry-a bilo.
--   otc_saga_step_log   — svaki pokušaj svakog koraka (za audit i manuelni retry).
--
-- Dizajn:
--   - otc_saga_executions.current_step drži POSLEDNJI USPEŠNO ZAVRŠENI korak
--     (ili 'PENDING' na početku). Ovo je jedina tačka istine za recovery.
--   - Svaki kompenzacioni poziv inkrementira retry_count u step_log-u.
--   - Posle MAX_RETRIES (3) kompenzacija, status postaje 'COMPENSATION_FAILED'
--     što signalizira potrebu za manuelnom intervencijom.
-- =============================================================================

CREATE TABLE core_banking.otc_saga_executions (
    id                     BIGINT         GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    contract_id            BIGINT         NOT NULL UNIQUE
                               REFERENCES core_banking.otc_contracts(id),
    -- Poslednji uspešno završen korak. Vrednosti: PENDING, RESERVE_FUNDS,
    -- RESERVE_SECURITIES, TRANSFER_FUNDS, TRANSFER_OWNERSHIP, COMPLETED.
    current_step           VARCHAR(30)    NOT NULL DEFAULT 'PENDING',
    -- Ukupan status SAGE: IN_PROGRESS, COMPLETED, FAILED,
    -- COMPENSATING, COMPENSATION_FAILED.
    status                 VARCHAR(30)    NOT NULL DEFAULT 'IN_PROGRESS'
                               CHECK (status IN (
                                   'IN_PROGRESS', 'COMPLETED', 'FAILED',
                                   'COMPENSATING', 'COMPENSATION_FAILED'
                               )),
    -- Iznos koji je rezervisan na kupčevom računu u valuti listinga (za rollback).
    buyer_reserved_amount  NUMERIC(18, 6) NOT NULL DEFAULT 0,
    -- Poslednja greška (za dijagnostiku).
    error_message          TEXT,
    -- Ukupan broj pokušaja kompenzacije (cross svih koraka).
    retry_count            INTEGER        NOT NULL DEFAULT 0,
    initiated_by           BIGINT         NOT NULL,  -- user_id koji je pokrenuo execute
    created_at             TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    updated_at             TIMESTAMPTZ    NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_otc_saga_contract ON core_banking.otc_saga_executions (contract_id);
CREATE INDEX idx_otc_saga_status   ON core_banking.otc_saga_executions (status)
    WHERE status NOT IN ('COMPLETED', 'COMPENSATION_FAILED');

-- Log svakog individualnog koraka: uspešnih i neuspešnih pokušaja.
CREATE TABLE core_banking.otc_saga_step_log (
    id           BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    execution_id BIGINT      NOT NULL
                     REFERENCES core_banking.otc_saga_executions(id),
    -- Korak koji je pokušan.
    step         VARCHAR(30) NOT NULL,
    -- Rezultat: COMPLETED | FAILED | COMPENSATED | COMPENSATION_FAILED.
    step_status  VARCHAR(30) NOT NULL,
    error_msg    TEXT,
    -- Koji je ovo pokušaj (1 = prvi, 2 = retry, ...).
    attempt      INTEGER     NOT NULL DEFAULT 1,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_otc_saga_log_exec ON core_banking.otc_saga_step_log (execution_id);

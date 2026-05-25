-- Istorija kursne liste (dnevni snapshoti).
-- Tabelu popunjava worker.ExchangeRateSnapshotWorker:
--   • pri startu: 30 dana backfill-a kao random walk oko trenutnog srednjeg kursa
--     (samo ako u tabeli za zadnjih 30 dana nema podataka — idempotent)
--   • svaki dan u ponoć: nov dnevni snapshot.
CREATE TABLE IF NOT EXISTS core_banking.exchange_rate_history (
    id            BIGSERIAL PRIMARY KEY,
    snapshot_date DATE          NOT NULL,
    oznaka        VARCHAR(8)    NOT NULL,
    naziv         VARCHAR(128)  NOT NULL DEFAULT '',
    kupovni       NUMERIC(20,6) NOT NULL DEFAULT 0,
    srednji       NUMERIC(20,6) NOT NULL DEFAULT 0,
    prodajni      NUMERIC(20,6) NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_exchange_rate_snapshot UNIQUE (snapshot_date, oznaka)
);

CREATE INDEX IF NOT EXISTS idx_exchange_rate_history_date
    ON core_banking.exchange_rate_history (snapshot_date DESC);

CREATE INDEX IF NOT EXISTS idx_exchange_rate_history_oznaka_date
    ON core_banking.exchange_rate_history (oznaka, snapshot_date DESC);

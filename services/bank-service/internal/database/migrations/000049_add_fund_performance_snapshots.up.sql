CREATE TABLE IF NOT EXISTS core_banking.fund_performance_snapshots (
    id              BIGSERIAL PRIMARY KEY,
    fund_id         BIGINT       NOT NULL REFERENCES core_banking.investment_funds(id) ON DELETE CASCADE,
    snapshot_date   DATE         NOT NULL,
    fund_value_rsd  NUMERIC(20,4) NOT NULL DEFAULT 0,
    total_invested  NUMERIC(20,4) NOT NULL DEFAULT 0,
    liquid_assets   NUMERIC(20,4) NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_fund_snapshot UNIQUE (fund_id, snapshot_date)
);

CREATE INDEX IF NOT EXISTS idx_fund_snapshots_fund_date
    ON core_banking.fund_performance_snapshots (fund_id, snapshot_date DESC);

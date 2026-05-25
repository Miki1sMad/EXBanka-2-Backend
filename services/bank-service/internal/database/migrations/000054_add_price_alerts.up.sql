CREATE TABLE IF NOT EXISTS core_banking.price_alerts (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL,
    listing_id  BIGINT NOT NULL REFERENCES core_banking.listing(id) ON DELETE CASCADE,
    ticker      VARCHAR(20) NOT NULL,
    threshold   NUMERIC(18, 6) NOT NULL,
    direction   VARCHAR(5) NOT NULL CHECK (direction IN ('ABOVE', 'BELOW')),
    email       VARCHAR(255) NOT NULL,
    active      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_price_alerts_listing_active ON core_banking.price_alerts (listing_id, active);
CREATE INDEX IF NOT EXISTS idx_price_alerts_user ON core_banking.price_alerts (user_id);

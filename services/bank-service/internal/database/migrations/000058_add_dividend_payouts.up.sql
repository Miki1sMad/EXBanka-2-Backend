CREATE TABLE IF NOT EXISTS core_banking.dividend_payouts (
    id             BIGSERIAL PRIMARY KEY,
    user_id        BIGINT NOT NULL,
    listing_id     BIGINT NOT NULL,
    ticker         VARCHAR(20) NOT NULL,
    quantity       BIGINT NOT NULL,
    price_on_date  NUMERIC(20,6) NOT NULL,
    gross_amount   NUMERIC(20,6) NOT NULL,
    tax_amount_rsd NUMERIC(20,6) NOT NULL DEFAULT 0,
    net_amount     NUMERIC(20,6) NOT NULL,
    currency       VARCHAR(10) NOT NULL,
    account_id     BIGINT,
    is_actuary     BOOLEAN NOT NULL DEFAULT FALSE,
    payment_date   DATE NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_dividend_payouts_user    ON core_banking.dividend_payouts(user_id);
CREATE INDEX IF NOT EXISTS idx_dividend_payouts_listing ON core_banking.dividend_payouts(listing_id);
CREATE INDEX IF NOT EXISTS idx_dividend_payouts_date    ON core_banking.dividend_payouts(payment_date);

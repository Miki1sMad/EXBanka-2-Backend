CREATE TABLE IF NOT EXISTS core_banking.recurring_orders (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL,
    listing_id  BIGINT NOT NULL REFERENCES core_banking.listing(id) ON DELETE CASCADE,
    direction   VARCHAR(10)  NOT NULL,  -- BUY | SELL
    mode        VARCHAR(20)  NOT NULL,  -- BYQUANTITY | BYAMOUNT
    value       NUMERIC(20,6) NOT NULL,
    account_id  BIGINT NOT NULL,
    is_client   BOOLEAN NOT NULL DEFAULT TRUE,
    cadence     VARCHAR(10)  NOT NULL,  -- DAILY | WEEKLY | MONTHLY
    next_run    TIMESTAMPTZ NOT NULL,
    active      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_recurring_orders_due    ON core_banking.recurring_orders(next_run) WHERE active = TRUE;
CREATE INDEX IF NOT EXISTS idx_recurring_orders_user   ON core_banking.recurring_orders(user_id);

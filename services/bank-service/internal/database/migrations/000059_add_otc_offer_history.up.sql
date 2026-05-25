-- Stores the full history of OTC offer negotiations.
-- Each CREATED / COUNTER / ACCEPTED / DECLINED action is recorded with old and new values.

CREATE TABLE core_banking.otc_offer_history (
    id                  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    offer_id            BIGINT       NOT NULL,
    action              VARCHAR(20)  NOT NULL CHECK (action IN ('CREATED','COUNTER','ACCEPTED','DECLINED')),
    changed_by          BIGINT       NOT NULL,
    -- New values (populated for CREATED and COUNTER)
    amount              INT,
    price_per_stock     NUMERIC(15,6),
    premium             NUMERIC(15,6),
    settlement_date     DATE,
    -- Old values (populated for COUNTER only)
    old_amount          INT,
    old_price_per_stock NUMERIC(15,6),
    old_premium         NUMERIC(15,6),
    old_settlement_date DATE,
    -- Terminal status (populated for ACCEPTED and DECLINED)
    new_status          VARCHAR(20),
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_otc_offer_history_offer_id   ON core_banking.otc_offer_history(offer_id);
CREATE INDEX idx_otc_offer_history_changed_by ON core_banking.otc_offer_history(changed_by);

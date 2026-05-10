-- =============================================================================
-- Migration: 000046_add_otc_offers_and_contracts (UP)
-- Faza 2: OTC pregovaranje za akcije.
--
-- Tabele:
--   otc_offers     — entitet ponude u pregovoru (PENDING/ACCEPTED/REJECTED/DEACTIVATED)
--   otc_contracts  — opcioni ugovor koji nastaje kad je ponuda prihvaćena
--
-- Napomena: ownership-capacity check se izvršava u servisnom sloju protiv
-- core_banking.public_shares (vlasnik mora da je akcije postavio na javni režim).
-- =============================================================================

CREATE TABLE core_banking.otc_offers (
    id                BIGINT         GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    listing_id        BIGINT         NOT NULL REFERENCES core_banking.listing(id),
    seller_id         BIGINT         NOT NULL,                          -- user iz user-service
    buyer_id          BIGINT         NOT NULL,                          -- user iz user-service
    buyer_account_id  BIGINT         NOT NULL REFERENCES core_banking.racun(id),
    seller_account_id BIGINT         REFERENCES core_banking.racun(id), -- popunjava prodavac pri prvom counter/accept
    amount            INTEGER        NOT NULL CHECK (amount > 0),
    price_per_stock   NUMERIC(18, 6) NOT NULL CHECK (price_per_stock > 0),
    premium           NUMERIC(18, 6) NOT NULL CHECK (premium >= 0),
    settlement_date   DATE           NOT NULL,
    status            VARCHAR(20)    NOT NULL DEFAULT 'PENDING'
                      CHECK (status IN ('PENDING', 'ACCEPTED', 'REJECTED', 'DEACTIVATED')),
    last_modified     TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    modified_by       BIGINT         NOT NULL,
    created_at        TIMESTAMPTZ    NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_otc_offer_parties CHECK (buyer_id <> seller_id)
);

CREATE INDEX idx_otc_offers_seller   ON core_banking.otc_offers (seller_id);
CREATE INDEX idx_otc_offers_buyer    ON core_banking.otc_offers (buyer_id);
CREATE INDEX idx_otc_offers_listing  ON core_banking.otc_offers (listing_id);
CREATE INDEX idx_otc_offers_status   ON core_banking.otc_offers (status);
-- Hot path: capacity SUM po (seller_id, listing_id) za PENDING ponude.
CREATE INDEX idx_otc_offers_seller_listing_pending
    ON core_banking.otc_offers (seller_id, listing_id) WHERE status = 'PENDING';

-- otc_contracts: kreira se atomski iz accept handlera.
CREATE TABLE core_banking.otc_contracts (
    id                BIGINT         GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    offer_id          BIGINT         NOT NULL REFERENCES core_banking.otc_offers(id),
    listing_id        BIGINT         NOT NULL REFERENCES core_banking.listing(id),
    seller_id         BIGINT         NOT NULL,
    buyer_id          BIGINT         NOT NULL,
    buyer_account_id  BIGINT         NOT NULL REFERENCES core_banking.racun(id),
    seller_account_id BIGINT         NOT NULL REFERENCES core_banking.racun(id),
    amount            INTEGER        NOT NULL CHECK (amount > 0),
    strike_price      NUMERIC(18, 6) NOT NULL,
    premium           NUMERIC(18, 6) NOT NULL,
    settlement_date   DATE           NOT NULL,
    status            VARCHAR(20)    NOT NULL DEFAULT 'VALID'
                      CHECK (status IN ('VALID', 'EXPIRED', 'EXERCISED')),
    created_at        TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    exercised_at      TIMESTAMPTZ
);

CREATE INDEX idx_otc_contracts_seller   ON core_banking.otc_contracts (seller_id);
CREATE INDEX idx_otc_contracts_buyer    ON core_banking.otc_contracts (buyer_id);
CREATE INDEX idx_otc_contracts_listing  ON core_banking.otc_contracts (listing_id);
CREATE INDEX idx_otc_contracts_status   ON core_banking.otc_contracts (status);
-- Hot path: capacity SUM po (seller_id, listing_id) za VALID ugovore.
CREATE INDEX idx_otc_contracts_seller_listing_valid
    ON core_banking.otc_contracts (seller_id, listing_id) WHERE status = 'VALID';

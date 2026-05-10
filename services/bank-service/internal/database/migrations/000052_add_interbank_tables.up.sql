-- =============================================================================
-- Migration: 000052_add_interbank_tables (UP)
-- Modul: Komunikacija između banaka po si-tx-proto.
--
-- Tabele:
--   interbank_message_log    — sve incoming/outgoing poruke (idempotency, retry)
--   interbank_transaction    — stanje međubankarske transakcije (NEW/PREPARED/COMMITTED/ROLLED_BACK)
--   interbank_negotiation    — autoritativna kopija OTC pregovaranja (na strani prodavca)
--   interbank_option_contract — opcioni ugovor formiran iz OTC pregovaranja
--
-- Napomena:
--   Idempotency ključ je par (routing_number, locally_generated_key).
--   Banka mora čuvati ove ključeve indefinitno.
-- =============================================================================

-- ── Interbank message log (incoming + outgoing) ──────────────────────────────
CREATE TABLE core_banking.interbank_message_log (
    id                          BIGINT       GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    direction                   VARCHAR(10)  NOT NULL,                            -- 'INCOMING' | 'OUTGOING'
    message_type                VARCHAR(20)  NOT NULL,                            -- 'NEW_TX' | 'COMMIT_TX' | 'ROLLBACK_TX'
    idempotence_routing_number  BIGINT       NOT NULL,
    idempotence_local_key       VARCHAR(64)  NOT NULL,
    target_routing_number       BIGINT,                                           -- za OUTGOING: kome se šalje
    payload                     TEXT         NOT NULL,                            -- JSON tela poruke
    response_payload            TEXT,                                             -- JSON odgovora (votes / 204)
    response_status_code        INTEGER,                                          -- HTTP status koji je vraćen / primljen
    status                      VARCHAR(20)  NOT NULL DEFAULT 'PENDING',          -- 'PENDING' | 'PROCESSED' | 'SENT' | 'FAILED' | 'ACCEPTED'
    retry_count                 INTEGER      NOT NULL DEFAULT 0,
    next_retry_at               TIMESTAMPTZ,
    last_error                  TEXT,
    created_at                  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    CONSTRAINT interbank_msg_dir_chk
        CHECK (direction IN ('INCOMING', 'OUTGOING')),
    CONSTRAINT interbank_msg_type_chk
        CHECK (message_type IN ('NEW_TX', 'COMMIT_TX', 'ROLLBACK_TX')),
    CONSTRAINT interbank_msg_status_chk
        CHECK (status IN ('PENDING', 'PROCESSED', 'SENT', 'FAILED', 'ACCEPTED', 'NO_VOTE'))
);

-- Idempotency: za INCOMING poruke par (routing_number, local_key, direction)
-- mora biti jedinstven kako bi se duplikati prepoznali.
CREATE UNIQUE INDEX uq_interbank_msg_idempotence
    ON core_banking.interbank_message_log (
        direction, idempotence_routing_number, idempotence_local_key
    );

CREATE INDEX idx_interbank_msg_status      ON core_banking.interbank_message_log (status);
CREATE INDEX idx_interbank_msg_next_retry  ON core_banking.interbank_message_log (next_retry_at)
    WHERE status IN ('PENDING', 'ACCEPTED', 'FAILED');

-- ── Interbank transaction (saga state) ───────────────────────────────────────
CREATE TABLE core_banking.interbank_transaction (
    id                              BIGINT       GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    transaction_routing_number      BIGINT       NOT NULL,                          -- ForeignBankId.routingNumber
    transaction_foreign_id          VARCHAR(64)  NOT NULL,                          -- ForeignBankId.id
    role                            VARCHAR(20)  NOT NULL,                          -- 'COORDINATOR' | 'PARTICIPANT'
    status                          VARCHAR(20)  NOT NULL DEFAULT 'NEW',            -- NEW | PREPARED | COMMITTED | ROLLED_BACK | FAILED
    current_step                    VARCHAR(40)  NOT NULL DEFAULT 'CREATED',
    payload                         TEXT         NOT NULL,                          -- JSON Transaction objekta
    failure_reason                  TEXT,
    initiator_user_id               BIGINT,
    initiator_account_id            BIGINT,
    created_at                      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at                      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    CONSTRAINT interbank_tx_role_chk
        CHECK (role IN ('COORDINATOR', 'PARTICIPANT')),
    CONSTRAINT interbank_tx_status_chk
        CHECK (status IN ('NEW', 'PREPARED', 'COMMITTED', 'ROLLED_BACK', 'FAILED'))
);

CREATE UNIQUE INDEX uq_interbank_tx_id
    ON core_banking.interbank_transaction (transaction_routing_number, transaction_foreign_id);

-- ── Interbank reservation (lock-aware reservation snapshot) ──────────────────
-- Čuva detalje rezervacija prikačenih za prepared interbank transakciju kako bi
-- COMMIT_TX i ROLLBACK_TX mogli da znaju šta da urade. Jedan red po posting-u.
CREATE TABLE core_banking.interbank_reservation (
    id                         BIGINT        GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    interbank_transaction_id   BIGINT        NOT NULL REFERENCES core_banking.interbank_transaction(id) ON DELETE CASCADE,
    posting_index              INTEGER       NOT NULL,
    account_kind               VARCHAR(10)   NOT NULL,                              -- 'ACCOUNT' | 'PERSON' | 'OPTION'
    account_num                VARCHAR(32),                                          -- za ACCOUNT
    foreign_routing_number     BIGINT,                                               -- za PERSON/OPTION
    foreign_id                 VARCHAR(64),                                          -- za PERSON/OPTION
    asset_type                 VARCHAR(10)   NOT NULL,                              -- 'MONAS' | 'STOCK' | 'OPTION'
    asset_currency             VARCHAR(10),                                          -- za MONAS
    asset_ticker               VARCHAR(20),                                          -- za STOCK ili OPTION.stock
    asset_negotiation_routing  BIGINT,                                               -- za OPTION
    asset_negotiation_local_id VARCHAR(64),                                          -- za OPTION
    amount                     NUMERIC(20, 6) NOT NULL,
    reserved                   BOOLEAN       NOT NULL DEFAULT FALSE,
    created_at                 TIMESTAMPTZ   NOT NULL DEFAULT NOW(),

    CONSTRAINT interbank_resv_account_kind_chk
        CHECK (account_kind IN ('ACCOUNT', 'PERSON', 'OPTION')),
    CONSTRAINT interbank_resv_asset_type_chk
        CHECK (asset_type IN ('MONAS', 'STOCK', 'OPTION'))
);

CREATE INDEX idx_interbank_resv_tx
    ON core_banking.interbank_reservation (interbank_transaction_id);

-- ── Interbank OTC negotiation (autoritativna kopija na strani prodavca) ──────
CREATE TABLE core_banking.interbank_negotiation (
    id                              BIGINT       GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- ForeignBankId (negotiation)
    negotiation_routing_number      BIGINT       NOT NULL,
    negotiation_foreign_id          VARCHAR(64)  NOT NULL,
    -- Stock & finansijski parametri
    stock_ticker                    VARCHAR(20)  NOT NULL,
    settlement_date                 TIMESTAMPTZ  NOT NULL,
    price_currency                  VARCHAR(10)  NOT NULL,
    price_amount                    NUMERIC(20, 6) NOT NULL,
    premium_currency                VARCHAR(10)  NOT NULL,
    premium_amount                  NUMERIC(20, 6) NOT NULL,
    amount                          INTEGER      NOT NULL CHECK (amount > 0),
    -- Strane
    buyer_routing_number            BIGINT       NOT NULL,
    buyer_id                        VARCHAR(64)  NOT NULL,
    seller_routing_number           BIGINT       NOT NULL,
    seller_id                       VARCHAR(64)  NOT NULL,
    -- Naizmenično pregovaranje
    last_modified_routing_number    BIGINT       NOT NULL,
    last_modified_id                VARCHAR(64)  NOT NULL,
    is_ongoing                      BOOLEAN      NOT NULL DEFAULT TRUE,
    status                          VARCHAR(20)  NOT NULL DEFAULT 'OPEN',          -- OPEN | CLOSED | ACCEPTED | CANCELLED
    created_at                      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at                      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    CONSTRAINT interbank_neg_status_chk
        CHECK (status IN ('OPEN', 'CLOSED', 'ACCEPTED', 'CANCELLED'))
);

CREATE UNIQUE INDEX uq_interbank_negotiation_id
    ON core_banking.interbank_negotiation (negotiation_routing_number, negotiation_foreign_id);
CREATE INDEX idx_interbank_neg_buyer
    ON core_banking.interbank_negotiation (buyer_routing_number, buyer_id);
CREATE INDEX idx_interbank_neg_seller
    ON core_banking.interbank_negotiation (seller_routing_number, seller_id);
CREATE INDEX idx_interbank_neg_status
    ON core_banking.interbank_negotiation (status);

-- ── Interbank option contract ────────────────────────────────────────────────
CREATE TABLE core_banking.interbank_option_contract (
    id                              BIGINT       GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- Identifikator: koristimo isti negotiation foreign id kao primarnu vezu
    negotiation_routing_number      BIGINT       NOT NULL,
    negotiation_foreign_id          VARCHAR(64)  NOT NULL,
    stock_ticker                    VARCHAR(20)  NOT NULL,
    price_currency                  VARCHAR(10)  NOT NULL,
    price_amount                    NUMERIC(20, 6) NOT NULL,
    premium_currency                VARCHAR(10)  NOT NULL,
    premium_amount                  NUMERIC(20, 6) NOT NULL,
    settlement_date                 TIMESTAMPTZ  NOT NULL,
    amount                          INTEGER      NOT NULL CHECK (amount > 0),
    buyer_routing_number            BIGINT       NOT NULL,
    buyer_id                        VARCHAR(64)  NOT NULL,
    seller_routing_number           BIGINT       NOT NULL,
    seller_id                       VARCHAR(64)  NOT NULL,
    status                          VARCHAR(20)  NOT NULL DEFAULT 'ACTIVE',         -- ACTIVE | EXERCISED | EXPIRED
    used_at                         TIMESTAMPTZ,
    created_at                      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at                      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    CONSTRAINT interbank_optc_status_chk
        CHECK (status IN ('ACTIVE', 'EXERCISED', 'EXPIRED'))
);

CREATE UNIQUE INDEX uq_interbank_option_id
    ON core_banking.interbank_option_contract (negotiation_routing_number, negotiation_foreign_id);
CREATE INDEX idx_interbank_optc_buyer
    ON core_banking.interbank_option_contract (buyer_routing_number, buyer_id);
CREATE INDEX idx_interbank_optc_seller
    ON core_banking.interbank_option_contract (seller_routing_number, seller_id);
CREATE INDEX idx_interbank_optc_status
    ON core_banking.interbank_option_contract (status);

-- ── Public stock — eksplicitna oznaka da klijent stavlja akcije u javni režim
-- OTC public stock već ima public_shares (000029). Ne dupliramo.

-- Dozvoljen novi tip u core_banking.transakcija da bismo razlikovali interbank
-- knjiženja od lokalnih.
ALTER TABLE core_banking.transakcija
    DROP CONSTRAINT transakcija_tip_check,
    ADD CONSTRAINT transakcija_tip_check
        CHECK (tip_transakcije IN (
            'UPLATA', 'ISPLATA', 'INTERNI_TRANSFER', 'MENJACNICA', 'INTERBANK'
        ));

-- =============================================================================
-- Migration: 000044_add_investment_fund_full
-- Service:   bank-service
-- Schema:    core_banking
--
-- Proširuje investicione fondove svim poljima iz specifikacije Celine 4.
-- Postojeća tabela investment_funds dobija nova polja.
-- Nove tabele: fund_securities, client_fund_transactions.
-- =============================================================================

-- ─── 1. Dodaj polja koja nedostaju u investment_funds ─────────────────────────
-- Tabela je kreirana u ranijoj migraciji (fund_handler.go je koristio direktni SQL).
-- Ako tabela ne postoji, kreiramo je sa svim potrebnim kolonama.

CREATE TABLE IF NOT EXISTS core_banking.investment_funds (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name        VARCHAR(255) NOT NULL UNIQUE,
    description TEXT         NOT NULL DEFAULT '',
    manager_id  BIGINT       NOT NULL,
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

ALTER TABLE core_banking.investment_funds
    ADD COLUMN IF NOT EXISTS minimum_contribution DECIMAL(15, 2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS liquid_assets        DECIMAL(15, 2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS account_id           BIGINT;

-- ─── 2. Dodaj last_changed u fund_positions ───────────────────────────────────
-- Tabela je kreirana u ranijoj migraciji sa fund_handler.go logiom.

CREATE TABLE IF NOT EXISTS core_banking.fund_positions (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    fund_id      BIGINT         NOT NULL,
    user_id      BIGINT         NOT NULL,
    account_id   BIGINT         NOT NULL,
    invested_rsd DECIMAL(15, 2) NOT NULL DEFAULT 0,
    created_at   TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE (fund_id, user_id)
);

ALTER TABLE core_banking.fund_positions
    ADD COLUMN IF NOT EXISTS last_changed TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW();

-- ─── 3. fund_securities — hartije koje fond poseduje ─────────────────────────
-- Svaka hartija (listing) vezana za fond ima: količinu, datum nabavke i
-- nabavnu cenu u RSD (za buduće računanje realizovanog P&L fonda).
-- Jedinstvenost: (fund_id, listing_id) — jedan listing, jedna agregirana pozicija.

CREATE TABLE IF NOT EXISTS core_banking.fund_securities (
    id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    fund_id          BIGINT         NOT NULL,
    listing_id       BIGINT         NOT NULL,
    quantity         DECIMAL(20, 6) NOT NULL DEFAULT 0,
    acquisition_date TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    initial_cost_rsd DECIMAL(15, 2) NOT NULL DEFAULT 0,
    UNIQUE (fund_id, listing_id)
);

CREATE INDEX IF NOT EXISTS idx_fund_securities_fund_id    ON core_banking.fund_securities (fund_id);
CREATE INDEX IF NOT EXISTS idx_fund_securities_listing_id ON core_banking.fund_securities (listing_id);

-- ─── 4. client_fund_transactions — individualne uplate/isplate ───────────────
-- Svaka uplata ili isplata klijenta u/iz fonda se beleži ovde.
-- ClientFundPosition (fund_positions) je agregat; ovo su atomske transakcije.

CREATE TABLE IF NOT EXISTS core_banking.client_fund_transactions (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    fund_id    BIGINT         NOT NULL,
    user_id    BIGINT         NOT NULL,
    amount_rsd DECIMAL(15, 2) NOT NULL,
    status     VARCHAR(20)    NOT NULL DEFAULT 'pending'
                   CHECK (status IN ('pending', 'completed', 'failed')),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    is_inflow  BOOLEAN        NOT NULL DEFAULT TRUE
);

CREATE INDEX IF NOT EXISTS idx_client_fund_tx_fund_user ON core_banking.client_fund_transactions (fund_id, user_id);
CREATE INDEX IF NOT EXISTS idx_client_fund_tx_user      ON core_banking.client_fund_transactions (user_id);

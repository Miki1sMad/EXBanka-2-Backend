-- =============================================================================
-- Migration: 000001_init_schema
-- Implements: Backend Issue 1 (schema) + Backend Issue 2 (seed permissions)
--
-- NOTE: This supersedes the placeholder at services/user-service/migrations/.
--       When switching to this migration set, drop the old one first.
-- =============================================================================

-- ─── Extensions ──────────────────────────────────────────────────────────────

-- pgcrypto: gen_random_bytes() used later for secure token generation.
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ─── 1. MAIN TABLE: users ─────────────────────────────────────────────────────
-- Single table for all user types (ADMIN, EMPLOYEE, CLIENT).
-- Shared identity and login credentials live here.
-- password_hash and salt_password are NEVER returned by the service layer.

CREATE TABLE users (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- Authentication
    email         VARCHAR(255) NOT NULL,
    password_hash VARCHAR(255) NOT NULL DEFAULT '',  -- empty until account is activated
    salt_password VARCHAR(255) NOT NULL DEFAULT '',  -- unique per user, set at activation
    -- Role
    user_type     VARCHAR(50)  NOT NULL,
    -- Personal data
    first_name    VARCHAR(100) NOT NULL,
    last_name     VARCHAR(100) NOT NULL,
    birth_date    BIGINT       NOT NULL,             -- Unix epoch ms, matches domain spec
    gender        VARCHAR(20),                       -- nullable: 'M' | 'F' | 'OTHER'
    phone_number  VARCHAR(50),
    address       VARCHAR(255),
    -- Status
    is_active     BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    -- Constraints
    CONSTRAINT users_email_unique    UNIQUE (email),
    CONSTRAINT users_user_type_check CHECK (user_type IN ('ADMIN', 'EMPLOYEE', 'CLIENT'))
);

-- Fast lookup by email (login) and by name/surname (admin search — Issue 6).
CREATE INDEX idx_users_email ON users (email);
CREATE INDEX idx_users_names ON users (first_name, last_name);

-- ─── 2. SPECIFIC TABLE: employee_details ─────────────────────────────────────
-- Extends users for EMPLOYEE (and ADMIN) rows only.
-- Permissions are managed via user_permissions join table.

CREATE TABLE employee_details (
    user_id    BIGINT       PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    username   VARCHAR(100) NOT NULL,
    position   VARCHAR(100),           -- e.g. 'Menadžer'
    department VARCHAR(100),           -- e.g. 'Finansije'

    CONSTRAINT employee_details_username_unique UNIQUE (username)
);

-- ─── 3. SPECIFIC TABLE: client_details ───────────────────────────────────────
-- Extends users for CLIENT rows only.
-- linked_accounts holds account numbers; populated by the Account service (Celina 2).

CREATE TABLE client_details (
    user_id         BIGINT  PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    linked_accounts TEXT[]  NOT NULL DEFAULT '{}'
);

-- ─── 4. PERMISSION CODEBOOK ───────────────────────────────────────────────────
-- Static list of named capabilities used by the RBAC system.
-- permission_code is the value embedded in JWT tokens.

CREATE TABLE permissions (
    id              SERIAL       PRIMARY KEY,
    permission_code VARCHAR(100) NOT NULL,

    CONSTRAINT permissions_code_unique UNIQUE (permission_code)
);

-- ─── 5. JUNCTION TABLE: user_permissions ─────────────────────────────────────
-- Maps which EMPLOYEE accounts hold which permissions.
-- Clients and Admins do not use this table (Admins are identified by user_type).

CREATE TABLE user_permissions (
    user_id       BIGINT  NOT NULL REFERENCES users       (id) ON DELETE CASCADE,
    permission_id INTEGER NOT NULL REFERENCES permissions (id) ON DELETE CASCADE,

    PRIMARY KEY (user_id, permission_id)
);

-- ─── 6. VERIFICATION TOKENS ───────────────────────────────────────────────────
-- Stores short-lived tokens for account activation (Issue 10/11) and
-- password reset (Issue 10/12). Each token is single-use (used_at IS NOT NULL
-- means it has been consumed).

CREATE TABLE verification_tokens (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT       NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token      VARCHAR(255) NOT NULL,
    token_type VARCHAR(50)  NOT NULL,         -- 'ACTIVATION' | 'PASSWORD_RESET'
    expires_at TIMESTAMPTZ  NOT NULL,
    used_at    TIMESTAMPTZ,                   -- NULL = not yet used

    CONSTRAINT verification_tokens_token_unique UNIQUE (token),
    CONSTRAINT verification_tokens_type_check   CHECK  (token_type IN ('ACTIVATION', 'PASSWORD_RESET'))
);

CREATE INDEX idx_verification_tokens_token ON verification_tokens (token);

-- =============================================================================
-- SEED DATA — Backend Issue 2: permissions codebook
-- These codes are embedded in JWT access tokens and checked by RBAC middleware.
-- =============================================================================

INSERT INTO permissions (permission_code) VALUES
    ('ADMIN_PERMISSION'),   -- grants full administrative access
    ('CONTRACT_SIGNING'),   -- sign banking contracts and agreements
    ('NEW_INSURANCE'),      -- create new insurance policies
    ('STOCK_TRADING'),      -- trade securities on behalf of clients
    ('VIEW_STOCKS'),        -- read-only view of stocks and securities
    ('VIEW_ACCOUNTS'),      -- view client account details
    ('MANAGE_ACCOUNTS')     -- open / close / modify client accounts
ON CONFLICT (permission_code) DO NOTHING;

-- =============================================================================
-- SEED DATA — initial admin user
-- password: Admin123 (bcrypt cost 12)
-- =============================================================================

INSERT INTO users (email, password_hash, salt_password, user_type, first_name, last_name, birth_date, is_active)
VALUES ('admin@raf.rs', '$2a$12$AcicRLhfUC1gQ2CWY.7t0.enY/PeLQU3.whwoBNr3CwSCncnbO5Qq', '', 'ADMIN', 'Admin', 'Admin', 0, TRUE)
ON CONFLICT (email) DO NOTHING;

INSERT INTO employee_details (user_id, username, position, department)
SELECT id, 'admin', 'Administrator', 'IT'
FROM users WHERE email = 'admin@raf.rs'
ON CONFLICT (user_id) DO NOTHING;

INSERT INTO user_permissions (user_id, permission_id)
SELECT u.id, p.id
FROM users u, permissions p
WHERE u.email = 'admin@raf.rs' AND p.permission_code = 'ADMIN_PERMISSION'
ON CONFLICT DO NOTHING;

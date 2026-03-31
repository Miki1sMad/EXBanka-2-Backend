-- =============================================================================
-- Migration: 000014_add_actuary_table
-- Service:   bank-service
-- Schema:    core_banking
--
-- Table: actuary_info
--
-- Extends the Employee entity (cross-service reference) with stock-trading
-- actuary metadata. Supervisors have no spending limit (limit = 0,
-- need_approval = false). Agents have a daily limit that resets at 23:59.
--
-- NOTE: employee_id is a plain BIGINT without FK — cross-service reference to
--       the users table in user-service. References are resolved at the
--       application layer only.
-- =============================================================================

CREATE TABLE core_banking.actuary_info (
    id            BIGINT         GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    -- Cross-service reference to the employee in user-service (no FK constraint).
    -- One employee can hold exactly one actuary role.
    employee_id   BIGINT         NOT NULL UNIQUE,

    -- Role: SUPERVISOR has no limit and never needs approval;
    --       AGENT has a daily spending limit and may require approval.
    actuary_type  VARCHAR(20)    NOT NULL
        CONSTRAINT actuary_type_check
            CHECK (actuary_type IN ('SUPERVISOR', 'AGENT')),

    -- Daily spending limit in RSD. Always 0 for supervisors.
    "limit"       NUMERIC(15, 2) NOT NULL DEFAULT 0
        CONSTRAINT actuary_limit_non_negative CHECK ("limit" >= 0),

    -- Amount of the daily limit already consumed. Resets automatically at 23:59
    -- or manually by a supervisor. Always 0 for supervisors.
    used_limit    NUMERIC(15, 2) NOT NULL DEFAULT 0
        CONSTRAINT actuary_used_limit_non_negative CHECK (used_limit >= 0),

    -- When true, every order placed by this agent must be approved by a supervisor.
    -- Always false for supervisors.
    need_approval BOOLEAN        NOT NULL DEFAULT FALSE,

    created_at    TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ    NOT NULL DEFAULT NOW()
);

-- Fast lookup by employee_id (used on every authenticated actuary request).
CREATE INDEX idx_actuary_info_employee_id
    ON core_banking.actuary_info (employee_id);

-- Fast filtering by role in the supervisor portal (lists only AGENTs).
CREATE INDEX idx_actuary_info_actuary_type
    ON core_banking.actuary_info (actuary_type);

-- =============================================================================
-- Migration: 000005_add_supervisor_agent_permissions
-- Adds SUPERVISOR and AGENT permission codes used by the actuary module.
-- bank-service/actuary_consumer.go checks for these codes when auto-provisioning
-- actuary_info records via the user_created RabbitMQ queue.
-- =============================================================================

INSERT INTO permissions (permission_code) VALUES
    ('SUPERVISOR'),   -- Supervizor: unlimited actuary with no approval requirement
    ('AGENT')         -- Agent: actuary with a configurable daily limit, needs approval
ON CONFLICT (permission_code) DO NOTHING;

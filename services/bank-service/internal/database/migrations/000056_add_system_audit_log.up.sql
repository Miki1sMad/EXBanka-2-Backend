CREATE TABLE IF NOT EXISTS core_banking.system_audit_log (
    id         BIGSERIAL PRIMARY KEY,
    action     VARCHAR(60)  NOT NULL,
    actor_id   BIGINT,
    target_id  BIGINT,
    details    JSONB        NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_system_audit_log_action  ON core_banking.system_audit_log(action);
CREATE INDEX IF NOT EXISTS idx_system_audit_log_actor   ON core_banking.system_audit_log(actor_id);
CREATE INDEX IF NOT EXISTS idx_system_audit_log_created ON core_banking.system_audit_log(created_at DESC);

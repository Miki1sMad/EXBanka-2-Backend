-- Rollback: remove SUPERVISOR and AGENT permission codes.
-- user_permissions rows referencing these will be cascade-deleted automatically.
DELETE FROM permissions WHERE permission_code IN ('SUPERVISOR', 'AGENT');

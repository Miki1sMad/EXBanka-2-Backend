-- =============================================================================
-- Migration: 000014_add_actuary_table (DOWN)
-- Drops the actuary_info table and its indexes.
-- =============================================================================

DROP INDEX IF EXISTS core_banking.idx_actuary_info_actuary_type;
DROP INDEX IF EXISTS core_banking.idx_actuary_info_employee_id;
DROP TABLE IF EXISTS core_banking.actuary_info;

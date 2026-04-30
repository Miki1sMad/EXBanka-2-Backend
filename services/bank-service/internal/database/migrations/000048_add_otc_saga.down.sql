-- Migration: 000048_add_otc_saga (DOWN)
DROP TABLE IF EXISTS core_banking.otc_saga_step_log;
DROP TABLE IF EXISTS core_banking.otc_saga_executions;

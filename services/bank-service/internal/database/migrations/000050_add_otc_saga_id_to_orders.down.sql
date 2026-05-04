-- Migration: 000050_add_otc_saga_id_to_orders (DOWN)

DROP INDEX IF EXISTS core_banking.idx_orders_otc_saga_execution_id;

ALTER TABLE core_banking.orders
    DROP COLUMN IF EXISTS otc_saga_execution_id;

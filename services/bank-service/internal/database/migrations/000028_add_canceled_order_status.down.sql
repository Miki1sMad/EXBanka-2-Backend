-- =============================================================================
-- Migration: 000028_add_canceled_order_status (DOWN)
-- Reverts the orders.status CHECK constraint back to the original set.
-- =============================================================================

ALTER TABLE core_banking.orders
    DROP CONSTRAINT chk_orders_status,
    ADD  CONSTRAINT chk_orders_status
         CHECK (status IN ('PENDING', 'APPROVED', 'DECLINED', 'DONE'));

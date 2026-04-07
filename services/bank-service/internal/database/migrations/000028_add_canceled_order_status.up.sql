-- =============================================================================
-- Migration: 000028_add_canceled_order_status (UP)
-- Extends the orders.status CHECK constraint to include 'CANCELED'.
--
-- CANCELED is semantically distinct from DECLINED:
--   DECLINED  — supervisor rejected a PENDING order before any execution.
--   CANCELED  — owner or supervisor actively stopped a PENDING or APPROVED
--               order; may follow partial fills recorded in order_transactions.
-- =============================================================================

ALTER TABLE core_banking.orders
    DROP CONSTRAINT chk_orders_status,
    ADD  CONSTRAINT chk_orders_status
         CHECK (status IN ('PENDING', 'APPROVED', 'DECLINED', 'DONE', 'CANCELED'));

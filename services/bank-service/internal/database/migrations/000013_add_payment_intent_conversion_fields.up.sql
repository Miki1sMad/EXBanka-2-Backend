-- Add conversion metadata fields to payment_intent for cross-currency display
ALTER TABLE core_banking.payment_intent
  ADD COLUMN IF NOT EXISTS kurs              DECIMAL(18,6),
  ADD COLUMN IF NOT EXISTS valuta_primaoca   VARCHAR(10);

ALTER TABLE core_banking.payment_intent
  DROP COLUMN IF EXISTS kurs,
  DROP COLUMN IF EXISTS valuta_primaoca;

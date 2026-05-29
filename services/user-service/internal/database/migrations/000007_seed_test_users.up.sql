-- =============================================================================
-- Migration: 000007_seed_test_users
-- Service:   user-service
--
-- Kreira dva testna korisnika za razvoj i testiranje novih funkcionalnosti:
--
--   kknezevic4622rn@raf.rs — EMPLOYEE sa svim permisijama
--   kseniakenny@gmail.com  — CLIENT
--
-- Lozinka za oba: Test1234
-- bcrypt cost 12: $2a$12$75i5IhAP/GBg/3JY14G3F.XQUpYGL6tCBr1EqLoV33XH5eu4n6sMO
-- =============================================================================

-- ─── 1. Employee: kknezevic4622rn@raf.rs ─────────────────────────────────────

INSERT INTO users (email, password_hash, salt_password, user_type, first_name, last_name, birth_date, gender, phone_number, address, is_active)
VALUES (
    'kknezevic4622rn@raf.rs',
    '$2a$12$75i5IhAP/GBg/3JY14G3F.XQUpYGL6tCBr1EqLoV33XH5eu4n6sMO',
    '',
    'EMPLOYEE',
    'Ksenija',
    'Knezevic',
    946684800000,
    'F',
    '+381601234567',
    'Bulevar Oslobodjenja 12, Beograd',
    TRUE
)
ON CONFLICT (email) DO NOTHING;

INSERT INTO employee_details (user_id, username, position, department)
SELECT id, 'kknezevic', 'Aktuar', 'Berze'
FROM users WHERE email = 'kknezevic4622rn@raf.rs'
ON CONFLICT (user_id) DO NOTHING;

-- Sve employee permisije (isključujući ADMIN_PERMISSION, TRADE_STOCKS, MANAGE_USERS)
INSERT INTO user_permissions (user_id, permission_id)
SELECT u.id, p.id
FROM users u
CROSS JOIN permissions p
WHERE u.email = 'kknezevic4622rn@raf.rs'
  AND p.permission_code IN (
      'CONTRACT_SIGNING',
      'NEW_INSURANCE',
      'STOCK_TRADING',
      'VIEW_STOCKS',
      'VIEW_ACCOUNTS',
      'MANAGE_ACCOUNTS',
      'SUPERVISOR'
  )
ON CONFLICT DO NOTHING;

-- ─── 2. Client: kseniakenny@gmail.com ────────────────────────────────────────

INSERT INTO users (email, password_hash, salt_password, user_type, first_name, last_name, birth_date, gender, phone_number, address, is_active)
VALUES (
    'kseniakenny@gmail.com',
    '$2a$12$75i5IhAP/GBg/3JY14G3F.XQUpYGL6tCBr1EqLoV33XH5eu4n6sMO',
    '',
    'CLIENT',
    'Ksenia',
    'Kenny',
    946684800000,
    'F',
    '+381641234567',
    'Knez Mihailova 5, Beograd',
    TRUE
)
ON CONFLICT (email) DO NOTHING;

INSERT INTO client_details (user_id)
SELECT id FROM users WHERE email = 'kseniakenny@gmail.com'
ON CONFLICT (user_id) DO NOTHING;

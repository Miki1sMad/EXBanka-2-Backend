-- =============================================================================
-- Migration: 000010_add_supervisor_to_admins
-- Retroaktivno dodaje SUPERVISOR permisiju svim postojećim ADMIN korisnicima.
--
-- CreateEmployee i UpdateEmployee (admin promocija) već automatski dodaju
-- SUPERVISOR novim adminima. Ova migracija popravlja seedovane i ranije
-- kreirane admin naloge koji ga nemaju.
-- =============================================================================

INSERT INTO user_permissions (user_id, permission_id)
SELECT u.id, p.id
FROM users u
CROSS JOIN permissions p
WHERE u.user_type = 'ADMIN'
  AND p.permission_code = 'SUPERVISOR'
ON CONFLICT DO NOTHING;

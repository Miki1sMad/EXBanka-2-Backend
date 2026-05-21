DELETE FROM user_permissions
WHERE user_id IN (SELECT id FROM users WHERE user_type = 'ADMIN')
  AND permission_id = (SELECT id FROM permissions WHERE permission_code = 'SUPERVISOR');

-- =============================================================================
-- SQLC queries for actuary_info — bank-service
-- Run `sqlc generate` from services/bank-service/ to regenerate Go code.
--
-- Naming convention:
--   :one   → returns a single row  (sql.ErrNoRows if not found)
--   :exec  → returns no rows       (only checks for execution error)
--   :many  → returns []Row
-- =============================================================================

-- name: CreateActuary :one
-- Inserts a new actuary_info record and returns the full row.
-- employee_id must be unique; the caller is responsible for checking
-- that the referenced employee exists in user-service.
INSERT INTO core_banking.actuary_info (
    employee_id,
    actuary_type,
    "limit",
    used_limit,
    need_approval
) VALUES (
    $1, $2, $3, $4, $5
)
RETURNING
    id,
    employee_id,
    actuary_type,
    "limit",
    used_limit,
    need_approval,
    created_at,
    updated_at;

-- name: GetActuaryById :one
-- Returns the actuary_info row for the given PK.
-- Returns sql.ErrNoRows when no matching record exists.
SELECT
    id,
    employee_id,
    actuary_type,
    "limit",
    used_limit,
    need_approval,
    created_at,
    updated_at
FROM core_banking.actuary_info
WHERE id = $1;

-- name: GetActuaryByEmployeeId :one
-- Returns the actuary_info row for a given employee_id.
-- Used on every authenticated actuary request (JWT → employee_id lookup).
-- Returns sql.ErrNoRows when the employee is not registered as an actuary.
SELECT
    id,
    employee_id,
    actuary_type,
    "limit",
    used_limit,
    need_approval,
    created_at,
    updated_at
FROM core_banking.actuary_info
WHERE employee_id = $1;

-- name: ListActuaries :many
-- Returns all actuary_info rows, with an optional filter on actuary_type.
-- Pass a NULL sql.NullString to return all types (both SUPERVISOR and AGENT).
-- sqlc.narg() generates a sql.NullString parameter so the IS NULL predicate
-- works correctly from Go without resorting to raw *sql.DB queries.
--
-- Note: user-side filters (email, first_name, last_name, position) are
-- cross-service attributes resolved at the application layer after this query.
SELECT
    id,
    employee_id,
    actuary_type,
    "limit",
    used_limit,
    need_approval,
    created_at,
    updated_at
FROM core_banking.actuary_info
WHERE (sqlc.narg('actuary_type')::VARCHAR IS NULL OR actuary_type = sqlc.narg('actuary_type'))
ORDER BY id;

-- name: UpdateActuary :one
-- Replaces all mutable fields of an existing actuary_info record atomically.
-- Bumps updated_at to the current timestamp.
-- Returns sql.ErrNoRows when the given id does not exist.
UPDATE core_banking.actuary_info
SET
    actuary_type  = $2,
    "limit"       = $3,
    used_limit    = $4,
    need_approval = $5,
    updated_at    = NOW()
WHERE id = $1
RETURNING
    id,
    employee_id,
    actuary_type,
    "limit",
    used_limit,
    need_approval,
    created_at,
    updated_at;

-- name: DeleteActuary :exec
-- Removes the actuary_info record for the given PK.
-- No-op (no error) when the id does not exist.
DELETE FROM core_banking.actuary_info
WHERE id = $1;

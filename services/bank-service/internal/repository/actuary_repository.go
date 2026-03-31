package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"banka-backend/services/bank-service/internal/database/sqlc"
	"banka-backend/services/bank-service/internal/domain"

	"github.com/shopspring/decimal"
)

// =============================================================================
// actuaryRepository wraps sqlc.Queries backed by a plain *sql.DB.
// No GORM dependency — the caller (main.go) extracts sqlDB once from the
// shared GORM pool and passes it here directly.
// =============================================================================

type actuaryRepository struct {
	q *sqlc.Queries
}

// NewActuaryRepository constructs the repository from a standard *sql.DB.
func NewActuaryRepository(db *sql.DB) domain.ActuaryRepository {
	return &actuaryRepository{q: sqlc.New(db)}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// sqlcToDomain converts a sqlc CoreBankingActuaryInfo row to domain.Actuary.
// NUMERIC(15,2) columns arrive as strings; decimal.NewFromString preserves
// all significant digits without floating-point rounding.
func sqlcToDomain(row sqlc.CoreBankingActuaryInfo) domain.Actuary {
	lim, _ := decimal.NewFromString(row.Limit)
	used, _ := decimal.NewFromString(row.UsedLimit)
	return domain.Actuary{
		ID:           row.ID,
		EmployeeID:   row.EmployeeID,
		ActuaryType:  domain.ActuaryType(row.ActuaryType),
		Limit:        lim,
		UsedLimit:    used,
		NeedApproval: row.NeedApproval,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
}

// ─── Create ───────────────────────────────────────────────────────────────────

func (r *actuaryRepository) Create(ctx context.Context, input domain.CreateActuaryInput) (*domain.Actuary, error) {
	row, err := r.q.CreateActuary(ctx, sqlc.CreateActuaryParams{
		EmployeeID:   input.EmployeeID,
		ActuaryType:  string(input.ActuaryType),
		Limit:        input.Limit.StringFixed(2),
		UsedLimit:    input.UsedLimit.StringFixed(2),
		NeedApproval: input.NeedApproval,
	})
	if err != nil {
		return nil, fmt.Errorf("create actuary: %w", err)
	}
	a := sqlcToDomain(row)
	return &a, nil
}

// ─── GetByID ──────────────────────────────────────────────────────────────────

func (r *actuaryRepository) GetByID(ctx context.Context, id int64) (*domain.Actuary, error) {
	row, err := r.q.GetActuaryById(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrActuaryNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get actuary by id %d: %w", id, err)
	}
	a := sqlcToDomain(row)
	return &a, nil
}

// ─── GetByEmployeeID ──────────────────────────────────────────────────────────

func (r *actuaryRepository) GetByEmployeeID(ctx context.Context, employeeID int64) (*domain.Actuary, error) {
	row, err := r.q.GetActuaryByEmployeeId(ctx, employeeID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrActuaryNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get actuary by employee_id %d: %w", employeeID, err)
	}
	a := sqlcToDomain(row)
	return &a, nil
}

// ─── List ─────────────────────────────────────────────────────────────────────

// List returns actuaries filtered by actuaryType.
// Passing "" returns all rows (sql.NullString with Valid=false → SQL NULL →
// the IS NULL predicate in ListActuaries matches every row).
func (r *actuaryRepository) List(ctx context.Context, actuaryType string) ([]domain.Actuary, error) {
	var param sql.NullString
	if actuaryType != "" {
		param = sql.NullString{String: actuaryType, Valid: true}
	}
	rows, err := r.q.ListActuaries(ctx, param)
	if err != nil {
		return nil, fmt.Errorf("list actuaries (type=%q): %w", actuaryType, err)
	}
	result := make([]domain.Actuary, 0, len(rows))
	for _, row := range rows {
		result = append(result, sqlcToDomain(row))
	}
	return result, nil
}

// ─── Update ───────────────────────────────────────────────────────────────────

func (r *actuaryRepository) Update(ctx context.Context, input domain.UpdateActuaryInput) (*domain.Actuary, error) {
	row, err := r.q.UpdateActuary(ctx, sqlc.UpdateActuaryParams{
		ID:           input.ID,
		ActuaryType:  string(input.ActuaryType),
		Limit:        input.Limit.StringFixed(2),
		UsedLimit:    input.UsedLimit.StringFixed(2),
		NeedApproval: input.NeedApproval,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrActuaryNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update actuary id %d: %w", input.ID, err)
	}
	a := sqlcToDomain(row)
	return &a, nil
}

// ─── Delete ───────────────────────────────────────────────────────────────────

func (r *actuaryRepository) Delete(ctx context.Context, id int64) error {
	if err := r.q.DeleteActuary(ctx, id); err != nil {
		return fmt.Errorf("delete actuary id %d: %w", id, err)
	}
	return nil
}


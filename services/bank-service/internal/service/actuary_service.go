package service

import (
	"context"
	"fmt"

	"banka-backend/services/bank-service/internal/domain"

	"github.com/shopspring/decimal"
)

// =============================================================================
// actuaryService implementira domain.ActuaryService.
// Sloj je namerno tanak: validacija biznis-pravila i orkestracija repozitorijuma.
// =============================================================================

type actuaryService struct {
	repo domain.ActuaryRepository
}

func NewActuaryService(repo domain.ActuaryRepository) domain.ActuaryService {
	return &actuaryService{repo: repo}
}

// ─── Opšte operacije ──────────────────────────────────────────────────────────

func (s *actuaryService) GetActuaryByID(ctx context.Context, id int64) (*domain.Actuary, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *actuaryService) GetActuaryByEmployeeID(ctx context.Context, employeeID int64) (*domain.Actuary, error) {
	return s.repo.GetByEmployeeID(ctx, employeeID)
}

// ─── Operacije supervizorskog portala ─────────────────────────────────────────

// ListAgents vraća sve agente (bez supervizora).
func (s *actuaryService) ListAgents(ctx context.Context) ([]domain.Actuary, error) {
	return s.repo.List(ctx, string(domain.ActuaryTypeAgent))
}

// SetAgentLimit ažurira dnevni limit troškova za datog agenta.
// Supervizori ne mogu imati limit (ne bi trebalo pozvati ovu metodu za supervizore,
// ali servis ne proverava ulogu — to je odgovornost handler sloja).
func (s *actuaryService) SetAgentLimit(ctx context.Context, employeeID int64, limit decimal.Decimal) (*domain.Actuary, error) {
	if limit.IsNegative() {
		return nil, fmt.Errorf("limit ne može biti negativan")
	}
	a, err := s.repo.GetByEmployeeID(ctx, employeeID)
	if err != nil {
		return nil, err
	}
	return s.repo.Update(ctx, domain.UpdateActuaryInput{
		ID:           a.ID,
		ActuaryType:  a.ActuaryType,
		Limit:        limit,
		UsedLimit:    a.UsedLimit,
		NeedApproval: a.NeedApproval,
	})
}

// ResetAgentUsedLimit resetuje potrošnju agenta na 0 (ručni reset).
// Automatski reset se izvodi u 23:59 kroz scheduler.
func (s *actuaryService) ResetAgentUsedLimit(ctx context.Context, employeeID int64) (*domain.Actuary, error) {
	a, err := s.repo.GetByEmployeeID(ctx, employeeID)
	if err != nil {
		return nil, err
	}
	return s.repo.Update(ctx, domain.UpdateActuaryInput{
		ID:           a.ID,
		ActuaryType:  a.ActuaryType,
		Limit:        a.Limit,
		UsedLimit:    decimal.Zero,
		NeedApproval: a.NeedApproval,
	})
}

// SetAgentNeedApproval menja flag koji zahteva odobrenje supervizora za svaki order agenta.
func (s *actuaryService) SetAgentNeedApproval(ctx context.Context, employeeID int64, needApproval bool) (*domain.Actuary, error) {
	a, err := s.repo.GetByEmployeeID(ctx, employeeID)
	if err != nil {
		return nil, err
	}
	return s.repo.Update(ctx, domain.UpdateActuaryInput{
		ID:           a.ID,
		ActuaryType:  a.ActuaryType,
		Limit:        a.Limit,
		UsedLimit:    a.UsedLimit,
		NeedApproval: needApproval,
	})
}

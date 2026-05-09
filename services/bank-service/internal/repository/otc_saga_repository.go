package repository

// otc_saga_repository.go — implementacija domain.OTCSagaRepository.
// Čuva stanje SAGA egzekucije u otc_saga_executions i beleži svaki korak
// u otc_saga_step_log radi auditabilnosti i mogućnosti manuelnog retry-a.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"banka-backend/services/bank-service/internal/domain"

	"gorm.io/gorm"
)

// ─── GORM modeli ──────────────────────────────────────────────────────────────

type otcSagaExecutionModel struct {
	ID                  int64     `gorm:"column:id;primaryKey"`
	ContractID          int64     `gorm:"column:contract_id"`
	CurrentStep         string    `gorm:"column:current_step"`
	Status              string    `gorm:"column:status"`
	BuyerReservedAmount float64   `gorm:"column:buyer_reserved_amount"`
	ErrorMessage        string    `gorm:"column:error_message"`
	RetryCount          int       `gorm:"column:retry_count"`
	InitiatedBy         int64     `gorm:"column:initiated_by"`
	CreatedAt           time.Time `gorm:"column:created_at"`
	UpdatedAt           time.Time `gorm:"column:updated_at"`
}

func (otcSagaExecutionModel) TableName() string { return "core_banking.otc_saga_executions" }

func (m otcSagaExecutionModel) toDomain() domain.OTCSagaExecution {
	return domain.OTCSagaExecution{
		ID:                  m.ID,
		ContractID:          m.ContractID,
		CurrentStep:         domain.OTCSagaStep(m.CurrentStep),
		Status:              domain.OTCSagaStatus(m.Status),
		BuyerReservedAmount: m.BuyerReservedAmount,
		ErrorMessage:        m.ErrorMessage,
		RetryCount:          m.RetryCount,
		InitiatedBy:         m.InitiatedBy,
		CreatedAt:           m.CreatedAt,
		UpdatedAt:           m.UpdatedAt,
	}
}

type otcSagaStepLogModel struct {
	ID          int64     `gorm:"column:id;primaryKey"`
	ExecutionID int64     `gorm:"column:execution_id"`
	Step        string    `gorm:"column:step"`
	StepStatus  string    `gorm:"column:step_status"`
	ErrorMsg    string    `gorm:"column:error_msg"`
	Attempt     int       `gorm:"column:attempt"`
	CreatedAt   time.Time `gorm:"column:created_at"`
}

func (otcSagaStepLogModel) TableName() string { return "core_banking.otc_saga_step_log" }

// ─── Repository ───────────────────────────────────────────────────────────────

type otcSagaRepository struct {
	db *gorm.DB
}

// NewOTCSagaRepository kreira novi OTCSagaRepository.
func NewOTCSagaRepository(db *gorm.DB) domain.OTCSagaRepository {
	return &otcSagaRepository{db: db}
}

func (r *otcSagaRepository) WithTx(tx interface{}) domain.OTCSagaRepository {
	g, ok := tx.(*gorm.DB)
	if !ok || g == nil {
		return r
	}
	return &otcSagaRepository{db: g}
}

// CreateExecution upisuje novi red u IN_PROGRESS sa korakom PENDING.
// Ako za isti contract_id već postoji red, vraća grešku (UNIQUE constraint).
func (r *otcSagaRepository) CreateExecution(ctx context.Context, contractID, initiatedBy int64, buyerReservedAmount float64) (*domain.OTCSagaExecution, error) {
	now := time.Now().UTC()
	m := otcSagaExecutionModel{
		ContractID:          contractID,
		CurrentStep:         string(domain.OTCSagaStepPending),
		Status:              string(domain.OTCSagaStatusInProgress),
		BuyerReservedAmount: buyerReservedAmount,
		ErrorMessage:        "",
		RetryCount:          0,
		InitiatedBy:         initiatedBy,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		return nil, fmt.Errorf("create saga execution: %w", err)
	}
	d := m.toDomain()
	return &d, nil
}

// GetExecution čita egzekuciju po primarnom ključu.
func (r *otcSagaRepository) GetExecution(ctx context.Context, id int64) (*domain.OTCSagaExecution, error) {
	var m otcSagaExecutionModel
	if err := r.db.WithContext(ctx).First(&m, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("saga execution %d nije pronađena", id)
		}
		return nil, fmt.Errorf("get saga execution: %w", err)
	}
	d := m.toDomain()
	return &d, nil
}

// GetExecutionByContractID čita egzekuciju za dati contract_id.
// Vraća nil, nil ako ne postoji (nije greška — znači da SAGA još nije pokrenuta).
func (r *otcSagaRepository) GetExecutionByContractID(ctx context.Context, contractID int64) (*domain.OTCSagaExecution, error) {
	var m otcSagaExecutionModel
	if err := r.db.WithContext(ctx).
		Where("contract_id = ?", contractID).
		First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get saga execution by contract: %w", err)
	}
	d := m.toDomain()
	return &d, nil
}

// UpdateStep ažurira current_step, status, error_message i updated_at.
func (r *otcSagaRepository) UpdateStep(ctx context.Context, id int64, step domain.OTCSagaStep, status domain.OTCSagaStatus, errMsg string) error {
	res := r.db.WithContext(ctx).
		Model(&otcSagaExecutionModel{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"current_step":  string(step),
			"status":        string(status),
			"error_message": errMsg,
			"updated_at":    time.Now().UTC(),
		})
	if res.Error != nil {
		return fmt.Errorf("update saga step: %w", res.Error)
	}
	return nil
}

// IncrementRetry atomički povećava retry_count za 1 i vraća novi broj.
func (r *otcSagaRepository) IncrementRetry(ctx context.Context, id int64) (int, error) {
	if err := r.db.WithContext(ctx).
		Model(&otcSagaExecutionModel{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"retry_count": gorm.Expr("retry_count + 1"),
			"updated_at":  time.Now().UTC(),
		}).Error; err != nil {
		return 0, fmt.Errorf("increment saga retry: %w", err)
	}
	exec, err := r.GetExecution(ctx, id)
	if err != nil {
		return 0, err
	}
	return exec.RetryCount, nil
}

// DeleteExecution briše SAGA egzekuciju po ID-u (za retry posle FAILED/COMPENSATION_FAILED).
func (r *otcSagaRepository) DeleteExecution(ctx context.Context, id int64) error {
	if err := r.db.WithContext(ctx).
		Delete(&otcSagaExecutionModel{}, id).Error; err != nil {
		return fmt.Errorf("delete saga execution: %w", err)
	}
	return nil
}

// LogStep upisuje jedan red u otc_saga_step_log.
func (r *otcSagaRepository) LogStep(ctx context.Context, executionID int64, step domain.OTCSagaStep, stepStatus domain.OTCSagaStepStatus, errMsg string, attempt int) error {
	m := otcSagaStepLogModel{
		ExecutionID: executionID,
		Step:        string(step),
		StepStatus:  string(stepStatus),
		ErrorMsg:    errMsg,
		Attempt:     attempt,
		CreatedAt:   time.Now().UTC(),
	}
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		return fmt.Errorf("log saga step: %w", err)
	}
	return nil
}

package repository

import (
	"time"

	"banka-backend/services/bank-service/internal/domain"

	"gorm.io/gorm"
)

type recurringOrderModel struct {
	ID        int64     `gorm:"column:id;primaryKey"`
	UserID    int64     `gorm:"column:user_id"`
	ListingID int64     `gorm:"column:listing_id"`
	Direction string    `gorm:"column:direction"`
	Mode      string    `gorm:"column:mode"`
	Value     float64   `gorm:"column:value"`
	AccountID int64     `gorm:"column:account_id"`
	IsClient  bool      `gorm:"column:is_client"`
	Cadence   string    `gorm:"column:cadence"`
	NextRun   time.Time `gorm:"column:next_run"`
	Active    bool      `gorm:"column:active"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (recurringOrderModel) TableName() string { return "core_banking.recurring_orders" }

type recurringOrderRepository struct {
	db *gorm.DB
}

func NewRecurringOrderRepository(db *gorm.DB) domain.RecurringOrderRepository {
	return &recurringOrderRepository{db: db}
}

func toRecurringOrderDomain(m recurringOrderModel) domain.RecurringOrder {
	return domain.RecurringOrder{
		ID:        m.ID,
		UserID:    m.UserID,
		ListingID: m.ListingID,
		Direction: m.Direction,
		Mode:      m.Mode,
		Value:     m.Value,
		AccountID: m.AccountID,
		IsClient:  m.IsClient,
		Cadence:   m.Cadence,
		NextRun:   m.NextRun,
		Active:    m.Active,
		CreatedAt: m.CreatedAt,
	}
}

func (r *recurringOrderRepository) Create(order domain.RecurringOrder) (*domain.RecurringOrder, error) {
	m := recurringOrderModel{
		UserID:    order.UserID,
		ListingID: order.ListingID,
		Direction: order.Direction,
		Mode:      order.Mode,
		Value:     order.Value,
		AccountID: order.AccountID,
		IsClient:  order.IsClient,
		Cadence:   order.Cadence,
		NextRun:   order.NextRun,
		Active:    order.Active,
	}
	if err := r.db.Create(&m).Error; err != nil {
		return nil, err
	}
	result := toRecurringOrderDomain(m)
	return &result, nil
}

func (r *recurringOrderRepository) List(userID int64) ([]domain.RecurringOrder, error) {
	var rows []recurringOrderModel
	if err := r.db.Where("user_id = ?", userID).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]domain.RecurringOrder, 0, len(rows))
	for _, m := range rows {
		result = append(result, toRecurringOrderDomain(m))
	}
	return result, nil
}

func (r *recurringOrderRepository) GetByID(id int64) (*domain.RecurringOrder, error) {
	var m recurringOrderModel
	if err := r.db.Where("id = ?", id).First(&m).Error; err != nil {
		return nil, err
	}
	result := toRecurringOrderDomain(m)
	return &result, nil
}

func (r *recurringOrderRepository) Update(order domain.RecurringOrder) error {
	return r.db.Model(&recurringOrderModel{}).Where("id = ?", order.ID).Updates(map[string]interface{}{
		"active":   order.Active,
		"value":    order.Value,
		"next_run": order.NextRun,
	}).Error
}

func (r *recurringOrderRepository) Delete(id, userID int64) error {
	return r.db.Where("id = ? AND user_id = ?", id, userID).Delete(&recurringOrderModel{}).Error
}

func (r *recurringOrderRepository) ListDue(now time.Time) ([]domain.RecurringOrder, error) {
	var rows []recurringOrderModel
	if err := r.db.Where("active = TRUE AND next_run <= ?", now).Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]domain.RecurringOrder, 0, len(rows))
	for _, m := range rows {
		result = append(result, toRecurringOrderDomain(m))
	}
	return result, nil
}

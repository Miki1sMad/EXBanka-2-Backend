package repository

import (
	"errors"
	"time"

	"banka-backend/services/bank-service/internal/domain"

	"gorm.io/gorm"
)

type priceAlertModel struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement"`
	UserID    int64     `gorm:"column:user_id;not null"`
	ListingID int64     `gorm:"column:listing_id;not null"`
	Ticker    string    `gorm:"column:ticker;not null"`
	Threshold float64   `gorm:"column:threshold;not null"`
	Direction string    `gorm:"column:direction;not null"`
	Email     string    `gorm:"column:email;not null"`
	Active    bool      `gorm:"column:active;not null;default:true"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (priceAlertModel) TableName() string { return "core_banking.price_alerts" }

func toAlertDomain(m priceAlertModel) domain.PriceAlert {
	return domain.PriceAlert{
		ID:        m.ID,
		UserID:    m.UserID,
		ListingID: m.ListingID,
		Ticker:    m.Ticker,
		Threshold: m.Threshold,
		Direction: domain.PriceAlertDirection(m.Direction),
		Email:     m.Email,
		Active:    m.Active,
		CreatedAt: m.CreatedAt,
	}
}

type PriceAlertRepository struct {
	db *gorm.DB
}

func NewPriceAlertRepository(db *gorm.DB) *PriceAlertRepository {
	return &PriceAlertRepository{db: db}
}

func (r *PriceAlertRepository) Create(alert domain.PriceAlert) (domain.PriceAlert, error) {
	m := priceAlertModel{
		UserID:    alert.UserID,
		ListingID: alert.ListingID,
		Ticker:    alert.Ticker,
		Threshold: alert.Threshold,
		Direction: string(alert.Direction),
		Email:     alert.Email,
		Active:    true,
	}
	if err := r.db.Create(&m).Error; err != nil {
		return domain.PriceAlert{}, err
	}
	return toAlertDomain(m), nil
}

func (r *PriceAlertRepository) ListByUser(userID int64) ([]domain.PriceAlert, error) {
	var rows []priceAlertModel
	if err := r.db.Where("user_id = ?", userID).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.PriceAlert, len(rows))
	for i, m := range rows {
		out[i] = toAlertDomain(m)
	}
	return out, nil
}

func (r *PriceAlertRepository) Delete(id int64, userID int64) error {
	res := r.db.Where("id = ? AND user_id = ?", id, userID).Delete(&priceAlertModel{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrPriceAlertNotFound
	}
	return nil
}

func (r *PriceAlertRepository) ListActiveForListing(listingID int64) ([]domain.PriceAlert, error) {
	var rows []priceAlertModel
	if err := r.db.Where("listing_id = ? AND active = true", listingID).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.PriceAlert, len(rows))
	for i, m := range rows {
		out[i] = toAlertDomain(m)
	}
	return out, nil
}

func (r *PriceAlertRepository) Deactivate(id int64) error {
	res := r.db.Model(&priceAlertModel{}).Where("id = ?", id).Update("active", false)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("alert nije pronađen za deaktivaciju")
	}
	return nil
}

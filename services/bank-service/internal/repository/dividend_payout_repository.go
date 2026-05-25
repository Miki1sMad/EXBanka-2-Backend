package repository

import (
	"time"

	"banka-backend/services/bank-service/internal/domain"

	"gorm.io/gorm"
)

type dividendPayoutModel struct {
	ID           int64     `gorm:"column:id;primaryKey"`
	UserID       int64     `gorm:"column:user_id"`
	ListingID    int64     `gorm:"column:listing_id"`
	Ticker       string    `gorm:"column:ticker"`
	Quantity     int64     `gorm:"column:quantity"`
	PriceOnDate  float64   `gorm:"column:price_on_date"`
	GrossAmount  float64   `gorm:"column:gross_amount"`
	TaxAmountRSD float64   `gorm:"column:tax_amount_rsd"`
	NetAmount    float64   `gorm:"column:net_amount"`
	Currency     string    `gorm:"column:currency"`
	AccountID    *int64    `gorm:"column:account_id"`
	IsActuary    bool      `gorm:"column:is_actuary"`
	PaymentDate  time.Time `gorm:"column:payment_date"`
	CreatedAt    time.Time `gorm:"column:created_at"`
}

func (dividendPayoutModel) TableName() string { return "core_banking.dividend_payouts" }

type dividendPayoutRepository struct {
	db *gorm.DB
}

func NewDividendPayoutRepository(db *gorm.DB) domain.DividendPayoutRepository {
	return &dividendPayoutRepository{db: db}
}

func (r *dividendPayoutRepository) Create(payout domain.DividendPayout) error {
	m := dividendPayoutModel{
		UserID:       payout.UserID,
		ListingID:    payout.ListingID,
		Ticker:       payout.Ticker,
		Quantity:     payout.Quantity,
		PriceOnDate:  payout.PriceOnDate,
		GrossAmount:  payout.GrossAmount,
		TaxAmountRSD: payout.TaxAmountRSD,
		NetAmount:    payout.NetAmount,
		Currency:     payout.Currency,
		AccountID:    payout.AccountID,
		IsActuary:    payout.IsActuary,
		PaymentDate:  payout.PaymentDate,
	}
	return r.db.Create(&m).Error
}

func (r *dividendPayoutRepository) ListForUser(userID int64) ([]domain.DividendPayout, error) {
	var rows []dividendPayoutModel
	if err := r.db.Where("user_id = ?", userID).Order("payment_date DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]domain.DividendPayout, 0, len(rows))
	for _, m := range rows {
		result = append(result, domain.DividendPayout{
			ID:           m.ID,
			UserID:       m.UserID,
			ListingID:    m.ListingID,
			Ticker:       m.Ticker,
			Quantity:     m.Quantity,
			PriceOnDate:  m.PriceOnDate,
			GrossAmount:  m.GrossAmount,
			TaxAmountRSD: m.TaxAmountRSD,
			NetAmount:    m.NetAmount,
			Currency:     m.Currency,
			AccountID:    m.AccountID,
			IsActuary:    m.IsActuary,
			PaymentDate:  m.PaymentDate,
			CreatedAt:    m.CreatedAt,
		})
	}
	return result, nil
}

func (r *dividendPayoutRepository) ExistsForPeriod(listingID int64, paymentDate time.Time) (bool, error) {
	var count int64
	err := r.db.Model(&dividendPayoutModel{}).
		Where("listing_id = ? AND payment_date = ?", listingID, paymentDate.Format("2006-01-02")).
		Count(&count).Error
	return count > 0, err
}

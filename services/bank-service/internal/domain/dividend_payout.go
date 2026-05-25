package domain

import "time"

type DividendPayout struct {
	ID           int64
	UserID       int64
	ListingID    int64
	Ticker       string
	Quantity     int64
	PriceOnDate  float64
	GrossAmount  float64
	TaxAmountRSD float64
	NetAmount    float64
	Currency     string
	AccountID    *int64
	IsActuary    bool
	PaymentDate  time.Time
	CreatedAt    time.Time
}

type DividendPayoutRepository interface {
	Create(payout DividendPayout) error
	ListForUser(userID int64) ([]DividendPayout, error)
	ExistsForPeriod(listingID int64, paymentDate time.Time) (bool, error)
}

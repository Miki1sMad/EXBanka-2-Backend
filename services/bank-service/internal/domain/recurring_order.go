package domain

import "time"

type RecurringOrder struct {
	ID        int64
	UserID    int64
	ListingID int64
	Direction string  // BUY | SELL
	Mode      string  // BYQUANTITY | BYAMOUNT
	Value     float64
	AccountID int64
	IsClient  bool
	Cadence   string  // DAILY | WEEKLY | MONTHLY
	NextRun   time.Time
	Active    bool
	CreatedAt time.Time
}

type RecurringOrderRepository interface {
	Create(order RecurringOrder) (*RecurringOrder, error)
	List(userID int64) ([]RecurringOrder, error)
	GetByID(id int64) (*RecurringOrder, error)
	Update(order RecurringOrder) error
	Delete(id, userID int64) error
	ListDue(now time.Time) ([]RecurringOrder, error)
}

package domain

import (
	"errors"
	"time"
)

// PriceAlertDirection defines whether the alert fires when price goes above or below threshold.
type PriceAlertDirection string

const (
	PriceAlertAbove PriceAlertDirection = "ABOVE"
	PriceAlertBelow PriceAlertDirection = "BELOW"
)

// PriceAlert represents a user-defined price threshold notification.
type PriceAlert struct {
	ID        int64               `json:"id"`
	UserID    int64               `json:"userId"`
	ListingID int64               `json:"listingId"`
	Ticker    string              `json:"ticker"`
	Threshold float64             `json:"threshold"`
	Direction PriceAlertDirection `json:"direction"`
	Email     string              `json:"email"`
	Active    bool                `json:"active"`
	CreatedAt time.Time           `json:"createdAt"`
}

// PriceAlertRepository defines persistence operations for price alerts.
type PriceAlertRepository interface {
	Create(alert PriceAlert) (PriceAlert, error)
	ListByUser(userID int64) ([]PriceAlert, error)
	Delete(id int64, userID int64) error
	// ListActiveForListing returns all active alerts for a given listing.
	ListActiveForListing(listingID int64) ([]PriceAlert, error)
	// Deactivate marks an alert as inactive (after it fires).
	Deactivate(id int64) error
}

var ErrPriceAlertNotFound = errors.New("price alert nije pronađen")

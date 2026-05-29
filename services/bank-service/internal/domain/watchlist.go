package domain

import (
	"errors"
	"time"
)

var ErrWatchlistNotFound = errors.New("watchlist nije pronađen")
var ErrWatchlistItemExists = errors.New("hartija već postoji u watchlisti")

type Watchlist struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"userId"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

type WatchlistItem struct {
	WatchlistID   int64     `json:"watchlistId"`
	ListingID     int64     `json:"listingId"`
	Ticker        string    `json:"ticker"`
	Name          string    `json:"name"`
	ListingType   string    `json:"listingType"`
	Price         float64   `json:"price"`
	ChangePercent float64   `json:"changePercent"`
	AddedAt       time.Time `json:"addedAt"`
}

type WatchlistDetail struct {
	Watchlist
	Items []WatchlistItem `json:"items"`
}

type WatchlistRepository interface {
	Create(userID int64, name string) (Watchlist, error)
	List(userID int64) ([]Watchlist, error)
	Rename(id, userID int64, name string) (Watchlist, error)
	Delete(id, userID int64) error
	AddItem(watchlistID, listingID, userID int64) error
	RemoveItem(watchlistID, listingID, userID int64) error
	GetWithItems(id, userID int64) (WatchlistDetail, error)
}

package domain

import (
	"errors"
	"time"
)

var ErrWatchlistNotFound = errors.New("watchlist nije pronađen")
var ErrWatchlistItemExists = errors.New("hartija već postoji u watchlisti")

type Watchlist struct {
	ID        int64
	UserID    int64
	Name      string
	CreatedAt time.Time
}

type WatchlistItem struct {
	WatchlistID int64
	ListingID   int64
	Ticker      string
	Name        string
	ListingType string
	Price       float64
	AddedAt     time.Time
}

type WatchlistDetail struct {
	Watchlist
	Items []WatchlistItem
}

type WatchlistRepository interface {
	Create(userID int64, name string) (Watchlist, error)
	List(userID int64) ([]Watchlist, error)
	Delete(id, userID int64) error
	AddItem(watchlistID, listingID, userID int64) error
	RemoveItem(watchlistID, listingID, userID int64) error
	GetWithItems(id, userID int64) (WatchlistDetail, error)
}

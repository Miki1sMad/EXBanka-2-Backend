package repository

import (
	"errors"
	"strings"
	"time"

	"banka-backend/services/bank-service/internal/domain"
	"gorm.io/gorm"
)

type watchlistModel struct {
	ID        int64     `gorm:"column:id;primaryKey"`
	UserID    int64     `gorm:"column:user_id"`
	Name      string    `gorm:"column:name"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (watchlistModel) TableName() string { return "core_banking.watchlists" }

type watchlistItemRow struct {
	WatchlistID int64     `gorm:"column:watchlist_id"`
	ListingID   int64     `gorm:"column:listing_id"`
	Ticker      string    `gorm:"column:ticker"`
	Name        string    `gorm:"column:name"`
	ListingType string    `gorm:"column:listing_type"`
	Price       float64   `gorm:"column:price"`
	AddedAt     time.Time `gorm:"column:added_at"`
}

type GormWatchlistRepository struct {
	db *gorm.DB
}

func NewWatchlistRepository(db *gorm.DB) *GormWatchlistRepository {
	return &GormWatchlistRepository{db: db}
}

func (r *GormWatchlistRepository) Create(userID int64, name string) (domain.Watchlist, error) {
	m := watchlistModel{UserID: userID, Name: name}
	if err := r.db.Create(&m).Error; err != nil {
		return domain.Watchlist{}, err
	}
	return domain.Watchlist{ID: m.ID, UserID: m.UserID, Name: m.Name, CreatedAt: m.CreatedAt}, nil
}

func (r *GormWatchlistRepository) List(userID int64) ([]domain.Watchlist, error) {
	var models []watchlistModel
	if err := r.db.Where("user_id = ?", userID).Order("created_at DESC").Find(&models).Error; err != nil {
		return nil, err
	}
	result := make([]domain.Watchlist, len(models))
	for i, m := range models {
		result[i] = domain.Watchlist{ID: m.ID, UserID: m.UserID, Name: m.Name, CreatedAt: m.CreatedAt}
	}
	return result, nil
}

func (r *GormWatchlistRepository) Delete(id, userID int64) error {
	res := r.db.Where("id = ? AND user_id = ?", id, userID).Delete(&watchlistModel{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrWatchlistNotFound
	}
	return nil
}

func (r *GormWatchlistRepository) AddItem(watchlistID, listingID, userID int64) error {
	var count int64
	if err := r.db.Model(&watchlistModel{}).Where("id = ? AND user_id = ?", watchlistID, userID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return domain.ErrWatchlistNotFound
	}
	type itemModel struct {
		WatchlistID int64     `gorm:"column:watchlist_id"`
		ListingID   int64     `gorm:"column:listing_id"`
		AddedAt     time.Time `gorm:"column:added_at"`
	}
	item := itemModel{WatchlistID: watchlistID, ListingID: listingID, AddedAt: time.Now()}
	res := r.db.Table("core_banking.watchlist_items").Create(&item)
	if res.Error != nil {
		if errors.Is(res.Error, gorm.ErrDuplicatedKey) || isWatchlistUniqueViolation(res.Error) {
			return domain.ErrWatchlistItemExists
		}
		return res.Error
	}
	return nil
}

func (r *GormWatchlistRepository) RemoveItem(watchlistID, listingID, userID int64) error {
	var count int64
	if err := r.db.Model(&watchlistModel{}).Where("id = ? AND user_id = ?", watchlistID, userID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return domain.ErrWatchlistNotFound
	}
	return r.db.Table("core_banking.watchlist_items").
		Where("watchlist_id = ? AND listing_id = ?", watchlistID, listingID).
		Delete(nil).Error
}

func (r *GormWatchlistRepository) GetWithItems(id, userID int64) (domain.WatchlistDetail, error) {
	var m watchlistModel
	if err := r.db.Where("id = ? AND user_id = ?", id, userID).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.WatchlistDetail{}, domain.ErrWatchlistNotFound
		}
		return domain.WatchlistDetail{}, err
	}

	var rows []watchlistItemRow
	r.db.Raw(`
		SELECT wi.watchlist_id, wi.listing_id, wi.added_at,
		       l.ticker, l.name, l.listing_type, l.price
		FROM core_banking.watchlist_items wi
		JOIN core_banking.listing l ON l.id = wi.listing_id
		WHERE wi.watchlist_id = ?
		ORDER BY wi.added_at DESC
	`, id).Scan(&rows)

	items := make([]domain.WatchlistItem, len(rows))
	for i, row := range rows {
		items[i] = domain.WatchlistItem{
			WatchlistID: row.WatchlistID,
			ListingID:   row.ListingID,
			Ticker:      row.Ticker,
			Name:        row.Name,
			ListingType: row.ListingType,
			Price:       row.Price,
			AddedAt:     row.AddedAt,
		}
	}
	return domain.WatchlistDetail{
		Watchlist: domain.Watchlist{ID: m.ID, UserID: m.UserID, Name: m.Name, CreatedAt: m.CreatedAt},
		Items:     items,
	}, nil
}

func isWatchlistUniqueViolation(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "23505") || strings.Contains(err.Error(), "duplicate key"))
}

package repository

import (
	"context"
	"fmt"

	"banka-backend/services/bank-service/internal/trading"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// racunMarginRow je minimalna projekcija core_banking.racun tabele
// koja je potrebna za proveru slobodnih sredstava.
type racunMarginRow struct {
	StanjeRacuna      string `gorm:"column:stanje_racuna"`
	RezervovanaSredstva string `gorm:"column:rezervisana_sredstva"`
}

func (racunMarginRow) TableName() string { return "core_banking.racun" }

type marginChecker struct {
	db *gorm.DB
}

// NewMarginChecker vraća implementaciju trading.MarginChecker koja čita
// slobodna sredstva (stanje_racuna − rezervisana_sredstva) iz GORM-a.
func NewMarginChecker(db *gorm.DB) trading.MarginChecker {
	return &marginChecker{db: db}
}

// HasSufficientMargin vraća (true, nil) kada slobodna sredstva naloga
// pokrivaju traženi iznos. Slobodna sredstva = stanje_racuna − rezervisana_sredstva.
func (m *marginChecker) HasSufficientMargin(ctx context.Context, accountID int64, required decimal.Decimal) (bool, error) {
	var row racunMarginRow
	err := m.db.WithContext(ctx).
		Select("stanje_racuna, rezervisana_sredstva").
		Where("id = ?", accountID).
		First(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return false, fmt.Errorf("margin check: račun %d nije pronađen", accountID)
		}
		return false, fmt.Errorf("margin check: %w", err)
	}

	stanje, err := decimal.NewFromString(row.StanjeRacuna)
	if err != nil {
		return false, fmt.Errorf("margin check: parse stanje_racuna: %w", err)
	}
	rezervisano, err := decimal.NewFromString(row.RezervovanaSredstva)
	if err != nil {
		return false, fmt.Errorf("margin check: parse rezervisana_sredstva: %w", err)
	}

	slobodna := stanje.Sub(rezervisano)
	return slobodna.GreaterThanOrEqual(required), nil
}

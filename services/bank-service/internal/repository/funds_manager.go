package repository

import (
	"context"
	"fmt"
	"time"

	"banka-backend/services/bank-service/internal/trading"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type fundsManager struct {
	db *gorm.DB
}

// NewFundsManager vraća implementaciju trading.FundsManager koja direktno
// ažurira core_banking.racun u jednoj SQL naredbi (atomično).
func NewFundsManager(db *gorm.DB) trading.FundsManager {
	return &fundsManager{db: db}
}

// ReserveFunds povećava rezervisana_sredstva za dati iznos.
func (f *fundsManager) ReserveFunds(ctx context.Context, accountID int64, amount decimal.Decimal) error {
	result := f.db.WithContext(ctx).Exec(
		`UPDATE core_banking.racun
		 SET rezervisana_sredstva = rezervisana_sredstva + ?
		 WHERE id = ?`,
		amount.InexactFloat64(), accountID,
	)
	if result.Error != nil {
		return fmt.Errorf("rezervacija sredstava za račun %d: %w", accountID, result.Error)
	}
	return nil
}

// ReleaseFunds smanjuje rezervisana_sredstva za dati iznos (ne ide ispod 0).
func (f *fundsManager) ReleaseFunds(ctx context.Context, accountID int64, amount decimal.Decimal) error {
	result := f.db.WithContext(ctx).Exec(
		`UPDATE core_banking.racun
		 SET rezervisana_sredstva = GREATEST(0, rezervisana_sredstva - ?)
		 WHERE id = ?`,
		amount.InexactFloat64(), accountID,
	)
	if result.Error != nil {
		return fmt.Errorf("oslobađanje sredstava za račun %d: %w", accountID, result.Error)
	}
	return nil
}

// SettleBuyFill atomično smanjuje i stanje_racuna i rezervisana_sredstva
// za isti iznos (fill jednog BUY naloga). Kreira i zapis u transakcija tabeli.
func (f *fundsManager) SettleBuyFill(ctx context.Context, accountID int64, amount decimal.Decimal) error {
	result := f.db.WithContext(ctx).Exec(
		`UPDATE core_banking.racun
		 SET stanje_racuna        = stanje_racuna - ?,
		     rezervisana_sredstva = GREATEST(0, rezervisana_sredstva - ?)
		 WHERE id = ?`,
		amount.InexactFloat64(), amount.InexactFloat64(), accountID,
	)
	if result.Error != nil {
		return fmt.Errorf("namirenje BUY filla za račun %d: %w", accountID, result.Error)
	}
	f.db.WithContext(ctx).Create(&transakcijaModel{
		RacunID:          accountID,
		TipTransakcije:   "ISPLATA",
		Iznos:            amount.InexactFloat64(),
		Opis:             "Kupovina hartije od vrednosti",
		VremeIzvrsavanja: time.Now().UTC(),
		Status:           "IZVRSEN",
	})
	return nil
}

// CreditSellFill povećava stanje_racuna za dati iznos (fill jednog SELL naloga).
// Kreira i zapis u transakcija tabeli.
func (f *fundsManager) CreditSellFill(ctx context.Context, accountID int64, amount decimal.Decimal) error {
	result := f.db.WithContext(ctx).Exec(
		`UPDATE core_banking.racun
		 SET stanje_racuna = stanje_racuna + ?
		 WHERE id = ?`,
		amount.InexactFloat64(), accountID,
	)
	if result.Error != nil {
		return fmt.Errorf("kredit SELL filla za račun %d: %w", accountID, result.Error)
	}
	f.db.WithContext(ctx).Create(&transakcijaModel{
		RacunID:          accountID,
		TipTransakcije:   "UPLATA",
		Iznos:            amount.InexactFloat64(),
		Opis:             "Prodaja hartije od vrednosti",
		VremeIzvrsavanja: time.Now().UTC(),
		Status:           "IZVRSEN",
	})
	return nil
}

// ChargeCommission smanjuje stanje_racuna za iznos provizije (ne dirá rezervisana_sredstva)
// i kreira zapis u transakcija tabeli.
func (f *fundsManager) ChargeCommission(ctx context.Context, accountID int64, amount decimal.Decimal) error {
	result := f.db.WithContext(ctx).Exec(
		`UPDATE core_banking.racun
		 SET stanje_racuna = stanje_racuna - ?
		 WHERE id = ?`,
		amount.InexactFloat64(), accountID,
	)
	if result.Error != nil {
		return fmt.Errorf("naplata provizije za račun %d: %w", accountID, result.Error)
	}
	f.db.WithContext(ctx).Create(&transakcijaModel{
		RacunID:          accountID,
		TipTransakcije:   "ISPLATA",
		Iznos:            amount.InexactFloat64(),
		Opis:             "Provizija za hartiju od vrednosti",
		VremeIzvrsavanja: time.Now().UTC(),
		Status:           "IZVRSEN",
	})
	return nil
}

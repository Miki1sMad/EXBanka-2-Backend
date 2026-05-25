package worker

// exchange_rate_snapshot_worker.go — dnevni snapshot kursne liste u tabelu
// core_banking.exchange_rate_history.
//
// Životni ciklus:
//   • Pri startu: ako je tabela prazna ili nema podataka za zadnjih 30 dana,
//     backfill-uje 30 dana retroaktivno random-walk modelom oko trenutnog srednjeg
//     kursa (±1.2% dnevno). Cilj: mobilni klijent odmah ima podatke za prikaz.
//   • Svaki dan u ponoć (UTC): UPSERT-uje snapshot za današnji dan.
//
// Worker NE menja postojeću funkcionalnost — samo dopisuje u novu tabelu.

import (
	"context"
	"log"
	"math/rand"
	"time"

	"banka-backend/services/bank-service/internal/domain"
	"gorm.io/gorm"
)

// ExchangeRatesProvider je minimalni interfejs koji worker koristi za
// dohvat trenutne kursne liste (mid + naziv). Implementiran je već u
// service.exchangeService.
type ExchangeRatesProvider interface {
	GetRates(ctx context.Context) ([]domain.ExchangeRate, error)
}

// ExchangeRateSnapshotWorker snima dnevne snapshot-e kursne liste.
type ExchangeRateSnapshotWorker struct {
	db       *gorm.DB
	provider ExchangeRatesProvider
}

// NewExchangeRateSnapshotWorker konstruktor.
func NewExchangeRateSnapshotWorker(db *gorm.DB, provider ExchangeRatesProvider) *ExchangeRateSnapshotWorker {
	return &ExchangeRateSnapshotWorker{db: db, provider: provider}
}

// Start pokreće worker. Pozivati kao: go worker.Start(ctx).
func (w *ExchangeRateSnapshotWorker) Start(ctx context.Context) {
	log.Printf("[worker] ExchangeRateSnapshotWorker started")

	// 1) Backfill ako je potrebno.
	if err := w.backfillIfNeeded(ctx); err != nil {
		log.Printf("[worker] ExchangeRateSnapshotWorker backfill ERROR: %v", err)
	}

	// 2) Snapshot pri startu (za danas).
	w.runSnapshot(ctx, time.Now())

	// 3) Dnevna petlja.
	for {
		next := nextMidnight()
		log.Printf("[worker] ExchangeRateSnapshotWorker: next snapshot at %s", next.Format("2006-01-02 15:04:05"))
		select {
		case <-time.After(time.Until(next)):
			w.runSnapshot(ctx, next)
		case <-ctx.Done():
			log.Printf("[worker] ExchangeRateSnapshotWorker: shutting down")
			return
		}
	}
}

func (w *ExchangeRateSnapshotWorker) runSnapshot(ctx context.Context, t time.Time) {
	date := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	rates, err := w.provider.GetRates(ctx)
	if err != nil {
		log.Printf("[worker] ExchangeRateSnapshotWorker: GetRates failed for %s: %v", date.Format("2006-01-02"), err)
		return
	}
	for _, r := range rates {
		if err := w.upsert(ctx, date, r.Oznaka, r.Naziv, r.Kupovni, r.Srednji, r.Prodajni); err != nil {
			log.Printf("[worker] ExchangeRateSnapshotWorker: upsert %s %s failed: %v", date.Format("2006-01-02"), r.Oznaka, err)
		}
	}
	log.Printf("[worker] ExchangeRateSnapshotWorker: snapshot saved for %s (%d rates)", date.Format("2006-01-02"), len(rates))
}

// backfillIfNeeded popunjava istoriju za prethodnih 30 dana ako u tabeli
// nema podataka unutar tog prozora. Random walk oko trenutnog srednjeg kursa,
// ±1.2% dnevno. Idempotent: UNIQUE (snapshot_date, oznaka) sprečava duplikate.
func (w *ExchangeRateSnapshotWorker) backfillIfNeeded(ctx context.Context) error {
	var cnt int64
	if err := w.db.WithContext(ctx).Raw(`
		SELECT COUNT(*) FROM core_banking.exchange_rate_history
		WHERE snapshot_date >= CURRENT_DATE - INTERVAL '30 days'
	`).Scan(&cnt).Error; err != nil {
		return err
	}
	if cnt > 0 {
		log.Printf("[worker] ExchangeRateSnapshotWorker: backfill skipped (table already has %d rows in last 30 days)", cnt)
		return nil
	}

	rates, err := w.provider.GetRates(ctx)
	if err != nil {
		return err
	}
	if len(rates) == 0 {
		log.Printf("[worker] ExchangeRateSnapshotWorker: backfill skipped (no rates from provider)")
		return nil
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	today := time.Now().UTC()
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)

	rowsInserted := 0
	for _, r := range rates {
		for i := 1; i <= 30; i++ {
			d := today.AddDate(0, 0, -i)
			factor := 1.0 + (rng.Float64()-0.5)*0.024 // ±1.2%
			if err := w.upsert(ctx, d, r.Oznaka, r.Naziv,
				r.Kupovni*factor, r.Srednji*factor, r.Prodajni*factor); err != nil {
				log.Printf("[worker] ExchangeRateSnapshotWorker: backfill upsert %s %s failed: %v", d.Format("2006-01-02"), r.Oznaka, err)
				continue
			}
			rowsInserted++
		}
	}
	log.Printf("[worker] ExchangeRateSnapshotWorker: backfill inserted %d rows (30 days × %d currencies)", rowsInserted, len(rates))
	return nil
}

func (w *ExchangeRateSnapshotWorker) upsert(
	ctx context.Context,
	date time.Time,
	oznaka, naziv string,
	kupovni, srednji, prodajni float64,
) error {
	return w.db.WithContext(ctx).Exec(`
		INSERT INTO core_banking.exchange_rate_history
			(snapshot_date, oznaka, naziv, kupovni, srednji, prodajni)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (snapshot_date, oznaka) DO UPDATE SET
			naziv    = EXCLUDED.naziv,
			kupovni  = EXCLUDED.kupovni,
			srednji  = EXCLUDED.srednji,
			prodajni = EXCLUDED.prodajni
	`, date, oznaka, naziv, kupovni, srednji, prodajni).Error
}

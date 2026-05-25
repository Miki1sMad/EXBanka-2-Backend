package worker

import (
	"context"
	"log"
	"time"

	"banka-backend/services/bank-service/internal/domain"
)

const expiryWarnDays = 3 // dana unapred za upozorenje pre isteka ugovora

// OTCContractExpiryWorker jednom dnevno skenira sve VALID OTC ugovore čiji je
// settlement_date prošao i postavlja im status na EXPIRED.
// Pored toga, šalje notifikaciju korisnicima čiji ugovori ističu za expiryWarnDays dana.
type OTCContractExpiryWorker struct {
	repo     domain.OTCRepository
	notifier OTCNotifier
}

func NewOTCContractExpiryWorker(repo domain.OTCRepository, notifier OTCNotifier) *OTCContractExpiryWorker {
	return &OTCContractExpiryWorker{repo: repo, notifier: notifier}
}

// Start blokira dok se ctx ne otkaže. Prva provera ide 1 minutu nakon pokretanja
// (da se migracije slegnu), zatim svakih 24h.
func (w *OTCContractExpiryWorker) Start(ctx context.Context) {
	log.Printf("[worker] OTCContractExpiryWorker started (interval≈24h)")
	t := time.NewTimer(1 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Printf("[worker] OTCContractExpiryWorker stopped")
			return
		case <-t.C:
			w.run(ctx)
			t.Reset(24 * time.Hour)
		}
	}
}

func (w *OTCContractExpiryWorker) run(ctx context.Context) {
	n, err := w.repo.ExpireOverdueContracts(ctx)
	if err != nil {
		log.Printf("[worker] OTCContractExpiry: greška pri isteku: %v", err)
	} else if n > 0 {
		log.Printf("[worker] OTCContractExpiry: %d ugovor(a) označeno kao EXPIRED", n)
	}

	expiring, err := w.repo.ListContractsExpiringSoon(ctx, expiryWarnDays)
	if err != nil {
		log.Printf("[worker] OTCContractExpiry: greška pri dohvatu ugovora koji uskoro ističu: %v", err)
		return
	}
	for _, c := range expiring {
		w.notifier.NotifyContractExpiringSoon(c, expiryWarnDays)
	}
	if len(expiring) > 0 {
		log.Printf("[worker] OTCContractExpiry: %d ugovor(a) — upozorenje %d dana pre isteka", len(expiring), expiryWarnDays)
	}
}

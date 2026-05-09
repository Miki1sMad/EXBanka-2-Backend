package worker

import (
	"context"
	"log"
	"time"

	"banka-backend/services/bank-service/internal/domain"
)

// OTCContractExpiryWorker jednom dnevno skenira sve VALID OTC ugovore čiji je
// settlement_date prošao i postavlja im status na EXPIRED.
// Bez ovog workera status u bazi ostaje VALID zauvek, što zbunjuje korisnika.
type OTCContractExpiryWorker struct {
	repo domain.OTCRepository
}

func NewOTCContractExpiryWorker(repo domain.OTCRepository) *OTCContractExpiryWorker {
	return &OTCContractExpiryWorker{repo: repo}
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
		log.Printf("[worker] OTCContractExpiry: greška: %v", err)
		return
	}
	if n > 0 {
		log.Printf("[worker] OTCContractExpiry: %d ugovor(a) označeno kao EXPIRED", n)
	}
}

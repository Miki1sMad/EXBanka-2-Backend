package worker

// fund_performance_worker.go — daily worker koji snima FundValueRSD, totalInvested
// i liquidAssets za svaki aktivan fond u tabelu fund_performance_snapshots.
//
// Pokreće se jednom pri startu (snapshot za danas ako još ne postoji) i potom
// svaki dan u ponoć.

import (
	"context"
	"log"
	"time"
)

// SnapshotRunner je callback koji za dati datum snima stanje svih fondova.
// Implementaciju (adapter ka DB + service) postavlja main.go.
type SnapshotRunner func(ctx context.Context, date time.Time) error

// FundPerformanceWorker snima dnevni snapshot svih fondova.
type FundPerformanceWorker struct {
	run SnapshotRunner
}

// NewFundPerformanceWorker kreira worker sa zadatim callback-om.
func NewFundPerformanceWorker(run SnapshotRunner) *FundPerformanceWorker {
	return &FundPerformanceWorker{run: run}
}

// Start pokreće dnevni snapshot loop. Pozivati kao: go worker.Start(ctx).
func (w *FundPerformanceWorker) Start(ctx context.Context) {
	log.Printf("[worker] FundPerformanceWorker started")

	// Snapshot pri startu (za danas).
	w.runSnapshot(ctx, time.Now())

	for {
		next := nextMidnight()
		log.Printf("[worker] FundPerformanceWorker: next snapshot at %s", next.Format("2006-01-02 15:04:05"))

		select {
		case <-time.After(time.Until(next)):
			w.runSnapshot(ctx, next)
		case <-ctx.Done():
			log.Printf("[worker] FundPerformanceWorker: shutting down")
			return
		}
	}
}

func (w *FundPerformanceWorker) runSnapshot(ctx context.Context, t time.Time) {
	date := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	if err := w.run(ctx, date); err != nil {
		log.Printf("[worker] FundPerformanceWorker ERROR for %s: %v", date.Format("2006-01-02"), err)
	} else {
		log.Printf("[worker] FundPerformanceWorker: snapshot saved for %s", date.Format("2006-01-02"))
	}
}

// nextMidnight vraća naredni UTC ponoć.
func nextMidnight() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
}

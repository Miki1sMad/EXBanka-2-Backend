package worker

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ─── advanceNextRun ───────────────────────────────────────────────────────────

func TestAdvanceNextRun_Daily(t *testing.T) {
	base := time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC)
	got := advanceNextRun(base, "DAILY")
	assert.Equal(t, time.Date(2026, 1, 16, 9, 0, 0, 0, time.UTC), got)
}

func TestAdvanceNextRun_Weekly(t *testing.T) {
	base := time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC)
	got := advanceNextRun(base, "WEEKLY")
	assert.Equal(t, time.Date(2026, 1, 22, 9, 0, 0, 0, time.UTC), got)
}

func TestAdvanceNextRun_Monthly(t *testing.T) {
	base := time.Date(2026, 1, 31, 9, 0, 0, 0, time.UTC)
	got := advanceNextRun(base, "MONTHLY")
	assert.Equal(t, time.Date(2026, 2, 28, 9, 0, 0, 0, time.UTC), got)
}

func TestAdvanceNextRun_Monthly_YearRollover(t *testing.T) {
	base := time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)
	got := advanceNextRun(base, "MONTHLY")
	assert.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), got)
}

func TestAdvanceNextRun_UnknownCadence_DefaultsToDaily(t *testing.T) {
	base := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	got := advanceNextRun(base, "HOURLY")
	assert.Equal(t, time.Date(2026, 3, 11, 12, 0, 0, 0, time.UTC), got)
}

func TestAdvanceNextRun_Empty_DefaultsToDaily(t *testing.T) {
	base := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	got := advanceNextRun(base, "")
	assert.Equal(t, time.Date(2026, 3, 11, 12, 0, 0, 0, time.UTC), got)
}

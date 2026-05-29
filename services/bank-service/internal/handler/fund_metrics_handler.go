package handler

// fund_metrics_handler.go — napredne metrike fondova izračunate iz
// core_banking.fund_performance_snapshots.
//
// Endpoint:
//   GET /bank/funds/{id}/metrics?window=365
//
// Metrike koje se vraćaju:
//   • annual_return_pct    — godišnji prinos (poslednja/prva − 1, anualiziran ako window != 365)
//   • volatility_pct       — godišnja standardna devijacija dnevnih log-returns
//   • max_drawdown_pct     — najveći pad od peak-a do trough-a u prozoru
//   • sharpe_ratio         — annual_return / volatility (risk-free = 0 zbog jednostavnosti)
//   • sample_size          — broj snapshot-a iskorišćenih za izračun
//   • period_start / period_end — datumski opseg iskorišćen
//
// Ako u prozoru ima manje od 2 snapshot-a, vraća `available: false` i prazne metrike
// (mobilni klijent može da prikaže poruku "metrike još nisu dostupne").

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	auth "banka-backend/shared/auth"
	"gorm.io/gorm"
)

// FundMetricsHandler serves GET /bank/funds/{id}/metrics.
type FundMetricsHandler struct {
	db        *gorm.DB
	jwtSecret string
}

// NewFundMetricsHandler kreira novi handler.
func NewFundMetricsHandler(db *gorm.DB, jwtSecret string) *FundMetricsHandler {
	return &FundMetricsHandler{db: db, jwtSecret: jwtSecret}
}

type fundMetricsResponse struct {
	Available       bool    `json:"available"`
	FundID          int64   `json:"fundId"`
	SampleSize      int     `json:"sampleSize"`
	PeriodStart     string  `json:"periodStart,omitempty"`
	PeriodEnd       string  `json:"periodEnd,omitempty"`
	AnnualReturnPct float64 `json:"annualReturnPct"`
	VolatilityPct   float64 `json:"volatilityPct"`
	MaxDrawdownPct  float64 `json:"maxDrawdownPct"`
	SharpeRatio     float64 `json:"sharpeRatio"`
	Note            string  `json:"note,omitempty"`
}

// ServeHTTP routes GET /bank/funds/{id}/metrics.
func (h *FundMetricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		exchangeWriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		exchangeWriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if _, err := auth.VerifyToken(strings.TrimPrefix(authHeader, "Bearer "), h.jwtSecret); err != nil {
		exchangeWriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	// path: /bank/funds/{id}/metrics
	path := strings.TrimSuffix(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 5 {
		exchangeWriteJSON(w, http.StatusBadRequest, map[string]string{"error": "neispravna putanja"})
		return
	}
	fundID, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil || fundID <= 0 {
		exchangeWriteJSON(w, http.StatusBadRequest, map[string]string{"error": "neispravan fund id"})
		return
	}

	window := 365
	if winStr := r.URL.Query().Get("window"); winStr != "" {
		if n, err := strconv.Atoi(winStr); err == nil && n > 1 {
			if n > 3650 {
				n = 3650
			}
			window = n
		}
	}

	type row struct {
		SnapshotDate time.Time `gorm:"column:snapshot_date"`
		FundValueRSD float64   `gorm:"column:fund_value_rsd"`
	}
	var rows []row
	if err := h.db.WithContext(r.Context()).Raw(`
		SELECT snapshot_date, fund_value_rsd
		FROM core_banking.fund_performance_snapshots
		WHERE fund_id = ?
		  AND snapshot_date >= CURRENT_DATE - (? || ' days')::INTERVAL
		ORDER BY snapshot_date ASC
	`, fundID, window).Scan(&rows).Error; err != nil {
		exchangeWriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "greška pri dohvatu metrika fonda"})
		return
	}

	if len(rows) < 2 {
		exchangeWriteJSON(w, http.StatusOK, fundMetricsResponse{
			Available:  false,
			FundID:     fundID,
			SampleSize: len(rows),
			Note:       "Nedovoljno istorijskih snapshot-a za izračun metrika. Pokušajte ponovo nakon nekoliko dana.",
		})
		return
	}

	values := make([]float64, len(rows))
	for i, x := range rows {
		values[i] = x.FundValueRSD
	}

	first := values[0]
	last := values[len(values)-1]

	// Period u danima (kalendarski).
	periodDays := int(rows[len(rows)-1].SnapshotDate.Sub(rows[0].SnapshotDate).Hours()/24) + 1
	if periodDays < 1 {
		periodDays = 1
	}

	// Total return → anualiziran preko 365.
	totalReturn := 0.0
	if first > 0 {
		totalReturn = (last / first) - 1.0
	}
	annualReturnPct := 0.0
	if totalReturn > -1.0 {
		annualReturnPct = (math.Pow(1.0+totalReturn, 365.0/float64(periodDays)) - 1.0) * 100.0
	}

	// Dnevni log-returns za volatilnost (anualizovana: σ_daily × √252).
	logReturns := make([]float64, 0, len(values)-1)
	for i := 1; i < len(values); i++ {
		if values[i-1] <= 0 || values[i] <= 0 {
			continue
		}
		logReturns = append(logReturns, math.Log(values[i]/values[i-1]))
	}
	volatilityPct := 0.0
	if len(logReturns) >= 2 {
		mean := 0.0
		for _, r := range logReturns {
			mean += r
		}
		mean /= float64(len(logReturns))
		variance := 0.0
		for _, r := range logReturns {
			d := r - mean
			variance += d * d
		}
		variance /= float64(len(logReturns) - 1) // sample std
		dailyStd := math.Sqrt(variance)
		volatilityPct = dailyStd * math.Sqrt(252.0) * 100.0
	}

	// Max drawdown: max( (peak - value) / peak ) sa peak = running max.
	peak := values[0]
	maxDrawdown := 0.0
	for _, v := range values {
		if v > peak {
			peak = v
		}
		if peak > 0 {
			dd := (peak - v) / peak
			if dd > maxDrawdown {
				maxDrawdown = dd
			}
		}
	}
	maxDrawdownPct := maxDrawdown * 100.0

	// Sharpe: annual_return / annual_volatility (risk-free pretpostavljen 0).
	sharpe := 0.0
	if volatilityPct > 0 {
		sharpe = annualReturnPct / volatilityPct
	}

	exchangeWriteJSON(w, http.StatusOK, fundMetricsResponse{
		Available:       true,
		FundID:          fundID,
		SampleSize:      len(values),
		PeriodStart:     rows[0].SnapshotDate.Format("2006-01-02"),
		PeriodEnd:       rows[len(rows)-1].SnapshotDate.Format("2006-01-02"),
		AnnualReturnPct: roundN(annualReturnPct, 2),
		VolatilityPct:   roundN(volatilityPct, 2),
		MaxDrawdownPct:  roundN(maxDrawdownPct, 2),
		SharpeRatio:     roundN(sharpe, 3),
	})
}

func roundN(v float64, n int) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	pow := math.Pow(10.0, float64(n))
	return math.Round(v*pow) / pow
}

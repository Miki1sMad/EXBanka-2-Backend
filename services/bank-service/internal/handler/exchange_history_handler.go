package handler

// exchange_history_handler.go — istorija kursne liste.
//
// Endpoint:
//   GET /bank/exchange-rates/history?oznaka=EUR&from=YYYY-MM-DD&to=YYYY-MM-DD&days=30
//
// Sva tri filter parametra su opcionalni:
//   - oznaka : ograničava na jednu valutu; ako je prazan, vraća sve podržane
//   - from/to: datumski opseg (inkluzivno). Ako se ne pošalju, koristi se zadnjih `days` dana.
//   - days   : alternativa za from/to. Default = 30. Max 365.
//
// Auth: Bearer JWT (isto kao /bank/exchange-rates u exchange_handler.go).

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	auth "banka-backend/shared/auth"
	"gorm.io/gorm"
)

// ExchangeHistoryHandler serves GET /bank/exchange-rates/history.
type ExchangeHistoryHandler struct {
	db        *gorm.DB
	jwtSecret string
}

// NewExchangeHistoryHandler kreira novi handler.
func NewExchangeHistoryHandler(db *gorm.DB, jwtSecret string) *ExchangeHistoryHandler {
	return &ExchangeHistoryHandler{db: db, jwtSecret: jwtSecret}
}

type exchangeRateHistoryPoint struct {
	Date     string  `json:"date"` // YYYY-MM-DD
	Oznaka   string  `json:"oznaka"`
	Naziv    string  `json:"naziv,omitempty"`
	Kupovni  float64 `json:"kupovni"`
	Srednji  float64 `json:"srednji"`
	Prodajni float64 `json:"prodajni"`
}

type exchangeRateHistoryResponse struct {
	From    string                     `json:"from"`
	To      string                     `json:"to"`
	Oznaka  string                     `json:"oznaka,omitempty"` // present samo ako je query filter koristio oznaka
	History []exchangeRateHistoryPoint `json:"history"`
}

// ServeHTTP obrađuje GET /bank/exchange-rates/history zahteve.
func (h *ExchangeHistoryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

	q := r.URL.Query()
	oznaka := strings.ToUpper(strings.TrimSpace(q.Get("oznaka")))

	// from/to parsing — default: zadnjih `days` dana.
	days := 30
	if d := q.Get("days"); d != "" {
		if n, err := strconv.Atoi(d); err == nil && n > 0 {
			if n > 365 {
				n = 365
			}
			days = n
		}
	}

	today := time.Now().UTC()
	defaultTo := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	defaultFrom := defaultTo.AddDate(0, 0, -(days - 1))

	from := defaultFrom
	to := defaultTo
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			from = t
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			to = t
		}
	}
	if to.Before(from) {
		from, to = to, from
	}

	// Query.
	sql := `
		SELECT snapshot_date, oznaka, naziv, kupovni, srednji, prodajni
		FROM core_banking.exchange_rate_history
		WHERE snapshot_date BETWEEN ? AND ?
	`
	args := []interface{}{from, to}
	if oznaka != "" {
		sql += " AND oznaka = ?"
		args = append(args, oznaka)
	}
	sql += " ORDER BY snapshot_date ASC, oznaka ASC"

	type row struct {
		SnapshotDate time.Time `gorm:"column:snapshot_date"`
		Oznaka       string    `gorm:"column:oznaka"`
		Naziv        string    `gorm:"column:naziv"`
		Kupovni      float64   `gorm:"column:kupovni"`
		Srednji      float64   `gorm:"column:srednji"`
		Prodajni     float64   `gorm:"column:prodajni"`
	}

	var rows []row
	if err := h.db.WithContext(r.Context()).Raw(sql, args...).Scan(&rows).Error; err != nil {
		exchangeWriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "greška pri dohvatu istorije kursne liste"})
		return
	}

	out := make([]exchangeRateHistoryPoint, 0, len(rows))
	for _, x := range rows {
		out = append(out, exchangeRateHistoryPoint{
			Date:     x.SnapshotDate.Format("2006-01-02"),
			Oznaka:   x.Oznaka,
			Naziv:    x.Naziv,
			Kupovni:  x.Kupovni,
			Srednji:  x.Srednji,
			Prodajni: x.Prodajni,
		})
	}

	exchangeWriteJSON(w, http.StatusOK, exchangeRateHistoryResponse{
		From:    from.Format("2006-01-02"),
		To:      to.Format("2006-01-02"),
		Oznaka:  oznaka,
		History: out,
	})
}

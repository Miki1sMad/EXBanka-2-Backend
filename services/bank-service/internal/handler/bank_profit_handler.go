package handler

// bank_profit_handler.go — HTTP handlers za Portal "Profit Banke" (Celina 4).
//
// Endpoints:
//   GET /bank/actuary-performance  — agregiran profit svakog aktuara agenta iz DONE SELL naloga
//   GET /bank/fund-positions       — sve klijentske pozicije u fondovima koje upravlja supervisor
//
// Auth: Bearer JWT, zaštita samo za SUPERVISOR/ADMIN.

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"banka-backend/services/bank-service/internal/domain"
	"banka-backend/services/bank-service/internal/transport"
	auth "banka-backend/shared/auth"

	"gorm.io/gorm"
)

// BankProfitHandler opslužuje endpoint-e za prikaz profita banke.
type BankProfitHandler struct {
	db          *gorm.DB
	fundService domain.InvestmentFundService
	userClient  *transport.UserServiceClient // može biti nil
	jwtSecret   string
}

// NewBankProfitHandler kreira handler sa zavisnostima.
func NewBankProfitHandler(
	db *gorm.DB,
	fundService domain.InvestmentFundService,
	userClient *transport.UserServiceClient,
	jwtSecret string,
) *BankProfitHandler {
	return &BankProfitHandler{
		db:          db,
		fundService: fundService,
		userClient:  userClient,
		jwtSecret:   jwtSecret,
	}
}

// authSupervisor parsira JWT i provjerava SUPERVISOR/ADMIN rolu.
// Vraća (callerID, claims, true) ili piše 401/403 i vraća (0, nil, false).
func (h *BankProfitHandler) authSupervisor(w http.ResponseWriter, r *http.Request) (int64, *auth.AccessClaims, bool) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return 0, nil, false
	}
	claims, err := auth.VerifyToken(strings.TrimPrefix(authHeader, "Bearer "), h.jwtSecret)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return 0, nil, false
	}
	callerID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return 0, nil, false
	}
	if !isBankSupervisorClaims(claims) {
		writeJSONError(w, http.StatusForbidden, "pristup samo za supervizore")
		return 0, nil, false
	}
	return callerID, claims, true
}

func isBankSupervisorClaims(claims *auth.AccessClaims) bool {
	if claims.UserType == "ADMIN" {
		return true
	}
	for _, p := range claims.Permissions {
		if p == "SUPERVISOR" {
			return true
		}
	}
	return false
}

// ─── GET /bank/actuary-performance ───────────────────────────────────────────

type actuaryPerformanceDTO struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Surname string  `json:"surname"`
	Role    string  `json:"role"`
	Profit  float64 `json:"profit"`
}

type actuaryProfitRow struct {
	EmployeeID  int64   `gorm:"column:employee_id"`
	ActuaryType string  `gorm:"column:actuary_type"`
	TotalProfit float64 `gorm:"column:total_profit"`
}

// actuaryProfitSQL agregira ostvareni profit svakog aktuara agenta koristeći
// isti WAVG pristup kao tax_service: profit = (avg_sell − avg_buy) × qty.
const actuaryProfitSQL = `
WITH sell_fills AS (
    SELECT
        o.user_id,
        o.listing_id,
        SUM(ot.executed_quantity)                                        AS sold_qty,
        SUM(CAST(ot.executed_price AS FLOAT) * ot.executed_quantity)
            / NULLIF(SUM(ot.executed_quantity), 0)                       AS avg_sell_price
    FROM core_banking.order_transactions ot
    JOIN core_banking.orders o ON o.id = ot.order_id
    WHERE o.direction = 'SELL'
      AND o.status    = 'DONE'
      AND o.is_done   = TRUE
    GROUP BY o.user_id, o.listing_id
),
buy_avg AS (
    SELECT
        o.user_id,
        o.listing_id,
        CASE
            WHEN SUM(ot.executed_quantity) > 0
            THEN SUM(CAST(ot.executed_price AS FLOAT) * ot.executed_quantity)
                 / NULLIF(SUM(ot.executed_quantity), 0)
            ELSE AVG(CAST(o.price_per_unit AS FLOAT))
        END AS avg_buy_price
    FROM core_banking.orders o
    LEFT JOIN core_banking.order_transactions ot ON ot.order_id = o.id
    WHERE o.direction = 'BUY'
      AND o.status    = 'DONE'
      AND o.is_done   = TRUE
    GROUP BY o.user_id, o.listing_id
)
SELECT
    ai.employee_id,
    ai.actuary_type,
    COALESCE(
        SUM(GREATEST(0, (s.avg_sell_price - COALESCE(b.avg_buy_price, 0)) * s.sold_qty)),
        0
    ) AS total_profit
FROM core_banking.actuary_info ai
LEFT JOIN sell_fills s ON s.user_id  = ai.employee_id
LEFT JOIN buy_avg    b ON b.user_id  = ai.employee_id
                       AND b.listing_id = s.listing_id
WHERE ai.actuary_type = 'AGENT'
GROUP BY ai.employee_id, ai.actuary_type
ORDER BY total_profit DESC
`

// ActuaryPerformanceHandler — GET /bank/actuary-performance
func (h *BankProfitHandler) ActuaryPerformanceHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	_, _, ok := h.authSupervisor(w, r)
	if !ok {
		return
	}

	ctx := r.Context()

	var rows []actuaryProfitRow
	if err := h.db.WithContext(ctx).Raw(actuaryProfitSQL).Scan(&rows).Error; err != nil {
		writeJSONError(w, http.StatusInternalServerError, "greška pri dohvatu performansi aktuara")
		return
	}

	out := make([]actuaryPerformanceDTO, 0, len(rows))
	for _, row := range rows {
		firstName, lastName := h.resolveEmployeeName(r, row.EmployeeID)
		out = append(out, actuaryPerformanceDTO{
			ID:      strconv.FormatInt(row.EmployeeID, 10),
			Name:    firstName,
			Surname: lastName,
			Role:    row.ActuaryType,
			Profit:  row.TotalProfit,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// ─── GET /bank/fund-positions ─────────────────────────────────────────────────

type fundPositionDTO struct {
	ID                   string   `json:"id"`
	ClientID             string   `json:"clientId"`
	ClientName           string   `json:"clientName"`
	FundID               string   `json:"fundId"`
	TotalInvestedAmount  float64  `json:"totalInvestedAmount"`
	FundSharePercentage  *float64 `json:"fundSharePercentage"`
	CurrentPositionValue *float64 `json:"currentPositionValue"`
	LastModifiedDate     string   `json:"lastModifiedDate"`
}

type fundPosRow struct {
	ID                int64     `gorm:"column:id"`
	FundID            int64     `gorm:"column:fund_id"`
	UserID            int64     `gorm:"column:user_id"`
	InvestedRSD       float64   `gorm:"column:invested_rsd"`
	LastChanged       time.Time `gorm:"column:last_changed"`
	TotalFundInvested float64   `gorm:"column:total_fund_invested"`
}

// FundPositionsHandler — GET /bank/fund-positions
func (h *BankProfitHandler) FundPositionsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	supervisorID, _, ok := h.authSupervisor(w, r)
	if !ok {
		return
	}

	ctx := r.Context()

	var rows []fundPosRow
	if err := h.db.WithContext(ctx).Raw(`
		SELECT
			fp.id,
			fp.fund_id,
			fp.user_id,
			fp.invested_rsd,
			COALESCE(fp.last_changed, fp.created_at) AS last_changed,
			COALESCE(tot.total_fund_invested, fp.invested_rsd) AS total_fund_invested
		FROM core_banking.fund_positions fp
		JOIN core_banking.investment_funds f ON f.id = fp.fund_id
		LEFT JOIN (
			SELECT fund_id, SUM(invested_rsd) AS total_fund_invested
			FROM core_banking.fund_positions
			GROUP BY fund_id
		) tot ON tot.fund_id = fp.fund_id
		WHERE f.manager_id = ?
		ORDER BY fp.fund_id, fp.invested_rsd DESC
	`, supervisorID).Scan(&rows).Error; err != nil {
		writeJSONError(w, http.StatusInternalServerError, "greška pri dohvatu pozicija")
		return
	}

	// Cache GetFundDetails per fund to avoid N×M calls.
	fundValueCache := make(map[int64]float64)
	getFundValue := func(fundID int64) float64 {
		if v, cached := fundValueCache[fundID]; cached {
			return v
		}
		details, err := h.fundService.GetFundDetails(ctx, fundID)
		if err != nil || details == nil {
			fundValueCache[fundID] = 0
			return 0
		}
		fundValueCache[fundID] = details.FundValueRSD
		return details.FundValueRSD
	}

	out := make([]fundPositionDTO, 0, len(rows))
	for _, row := range rows {
		clientName := h.resolveClientName(r, row.UserID)

		var sharePercent *float64
		var currentValue *float64
		if row.TotalFundInvested > 0 {
			pct := (row.InvestedRSD / row.TotalFundInvested) * 100
			sharePercent = &pct
			if fv := getFundValue(row.FundID); fv > 0 {
				cv := (row.InvestedRSD / row.TotalFundInvested) * fv
				currentValue = &cv
			}
		}

		out = append(out, fundPositionDTO{
			ID:                   strconv.FormatInt(row.ID, 10),
			ClientID:             strconv.FormatInt(row.UserID, 10),
			ClientName:           clientName,
			FundID:               strconv.FormatInt(row.FundID, 10),
			TotalInvestedAmount:  row.InvestedRSD,
			FundSharePercentage:  sharePercent,
			CurrentPositionValue: currentValue,
			LastModifiedDate:     row.LastChanged.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func (h *BankProfitHandler) resolveEmployeeName(r *http.Request, employeeID int64) (firstName, lastName string) {
	if h.userClient == nil || employeeID == 0 {
		return "", ""
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	info, err := h.userClient.GetEmployeeInfo(ctx, employeeID)
	if err != nil || info == nil {
		return "", ""
	}
	return info.FirstName, info.LastName
}

func (h *BankProfitHandler) resolveClientName(r *http.Request, clientID int64) string {
	if h.userClient == nil || clientID == 0 {
		return ""
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	info, err := h.userClient.GetClientInfo(ctx, clientID)
	if err != nil || info == nil {
		return ""
	}
	return strings.TrimSpace(info.FirstName + " " + info.LastName)
}

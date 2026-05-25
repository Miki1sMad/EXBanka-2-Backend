package handler

// my_orders_handler.go — plain HTTP handler for the "Moji nalozi" endpoint.
//
// Endpoint:
//   GET /bank/trading/my-orders?status=<STATUS>
//
// Returns only the orders that belong to the authenticated caller,
// enriched with ticker, listing_type, and estimated commission.

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"banka-backend/services/bank-service/internal/trading"
	auth "banka-backend/shared/auth"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type txRow struct {
	OrderID          int64  `gorm:"column:order_id"`
	ExecutedQuantity int32  `gorm:"column:executed_quantity"`
	ExecutedPrice    string `gorm:"column:executed_price"`
}

// MyOrdersHandler serves GET /bank/trading/my-orders.
type MyOrdersHandler struct {
	tradingService trading.TradingService
	db             *gorm.DB
	jwtSecret      string
}

// NewMyOrdersHandler constructs the handler.
func NewMyOrdersHandler(tradingService trading.TradingService, db *gorm.DB, jwtSecret string) *MyOrdersHandler {
	return &MyOrdersHandler{tradingService: tradingService, db: db, jwtSecret: jwtSecret}
}

// ServeHTTP handles GET /bank/trading/my-orders.
func (h *MyOrdersHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// ── Auth ──────────────────────────────────────────────────────────────────
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	claims, err := auth.VerifyToken(strings.TrimPrefix(authHeader, "Bearer "), h.jwtSecret)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	callerID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "neispravan korisnički ID u tokenu")
		return
	}

	// ── Optional status filter ────────────────────────────────────────────────
	statusFilter := r.URL.Query().Get("status")

	// ── Fetch orders joined with listing ─────────────────────────────────────
	type enrichedRow struct {
		ID                int64     `gorm:"column:id"`
		UserID            int64     `gorm:"column:user_id"`
		AccountID         int64     `gorm:"column:account_id"`
		ListingID         int64     `gorm:"column:listing_id"`
		OrderType         string    `gorm:"column:order_type"`
		Direction         string    `gorm:"column:direction"`
		Quantity          int32     `gorm:"column:quantity"`
		ContractSize      int32     `gorm:"column:contract_size"`
		PricePerUnit      *string   `gorm:"column:price_per_unit"`
		StopPrice         *string   `gorm:"column:stop_price"`
		Status            string    `gorm:"column:status"`
		ApprovedBy        *string   `gorm:"column:approved_by"`
		IsDone            bool      `gorm:"column:is_done"`
		RemainingPortions int32     `gorm:"column:remaining_portions"`
		AfterHours        bool      `gorm:"column:after_hours"`
		AllOrNone         bool      `gorm:"column:all_or_none"`
		Margin            bool      `gorm:"column:margin"`
		LastModified      time.Time `gorm:"column:last_modified"`
		CreatedAt         time.Time `gorm:"column:created_at"`
		Ticker            string    `gorm:"column:ticker"`
		ListingType       string    `gorm:"column:listing_type"`
	}

	query := `
		SELECT o.id, o.user_id, o.account_id, o.listing_id, o.order_type, o.direction,
		       o.quantity, o.contract_size, o.price_per_unit, o.stop_price, o.status,
		       o.approved_by, o.is_done, o.remaining_portions, o.after_hours, o.all_or_none,
		       o.margin, o.last_modified, o.created_at,
		       l.ticker, l.listing_type
		FROM core_banking.orders o
		JOIN core_banking.listing l ON l.id = o.listing_id
		WHERE o.user_id = ? AND o.fund_id IS NULL`

	args := []interface{}{callerID}
	if statusFilter != "" {
		query += " AND o.status = ?"
		args = append(args, statusFilter)
	}
	query += " ORDER BY o.created_at DESC LIMIT 500"

	var rows []enrichedRow
	if err := h.db.Raw(query, args...).Scan(&rows).Error; err != nil {
		writeJSONError(w, http.StatusInternalServerError, "greška pri dohvatu naloga")
		return
	}

	// ── Batch-fetch transactions for MARKET+DONE orders ───────────────────────
	var marketDoneIDs []int64
	for _, row := range rows {
		if row.OrderType == "MARKET" && row.IsDone {
			marketDoneIDs = append(marketDoneIDs, row.ID)
		}
	}

	txByOrder := make(map[int64][]txRow)
	if len(marketDoneIDs) > 0 {
		var txRows []txRow
		h.db.Raw(`SELECT order_id, executed_quantity, executed_price FROM core_banking.order_transactions WHERE order_id IN ?`, marketDoneIDs).Scan(&txRows)
		for _, tx := range txRows {
			txByOrder[tx.OrderID] = append(txByOrder[tx.OrderID], tx)
		}
	}

	// ── Serialize ─────────────────────────────────────────────────────────────
	type orderJSON struct {
		ID                int64     `json:"id"`
		UserID            int64     `json:"userId"`
		AccountID         int64     `json:"accountId"`
		ListingID         int64     `json:"listingId"`
		Ticker            string    `json:"ticker"`
		ListingType       string    `json:"listingType"`
		OrderType         string    `json:"orderType"`
		Direction         string    `json:"direction"`
		Quantity          int32     `json:"quantity"`
		ContractSize      int32     `json:"contractSize"`
		PricePerUnit      *string   `json:"pricePerUnit,omitempty"`
		StopPrice         *string   `json:"stopPrice,omitempty"`
		Status            string    `json:"status"`
		ApprovedBy        *string   `json:"approvedBy,omitempty"`
		IsDone            bool      `json:"isDone"`
		RemainingPortions int32     `json:"remainingPortions"`
		ExecutedQuantity  int32     `json:"executedQuantity"`
		AfterHours        bool      `json:"afterHours"`
		AllOrNone         bool      `json:"allOrNone"`
		Margin            bool      `json:"margin"`
		Commission        string    `json:"commission"`
		LastModified      time.Time `json:"lastModified"`
		CreatedAt         time.Time `json:"createdAt"`
	}

	result := make([]orderJSON, 0, len(rows))
	for _, row := range rows {
		commission := calcOrderCommission(row.OrderType, row.ContractSize, row.Quantity, row.PricePerUnit, row.StopPrice, txByOrder[row.ID])
		item := orderJSON{
			ID:                row.ID,
			UserID:            row.UserID,
			AccountID:         row.AccountID,
			ListingID:         row.ListingID,
			Ticker:            row.Ticker,
			ListingType:       row.ListingType,
			OrderType:         row.OrderType,
			Direction:         row.Direction,
			Quantity:          row.Quantity,
			ContractSize:      row.ContractSize,
			PricePerUnit:      row.PricePerUnit,
			StopPrice:         row.StopPrice,
			Status:            row.Status,
			ApprovedBy:        row.ApprovedBy,
			IsDone:            row.IsDone,
			RemainingPortions: row.RemainingPortions,
			ExecutedQuantity:  row.Quantity - row.RemainingPortions,
			AfterHours:        row.AfterHours,
			AllOrNone:         row.AllOrNone,
			Margin:            row.Margin,
			Commission:        commission.StringFixed(4),
			LastModified:      row.LastModified,
			CreatedAt:         row.CreatedAt,
		}
		result = append(result, item)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"orders": result})
}

// calcOrderCommission returns the estimated commission for an order.
func calcOrderCommission(orderType string, contractSize, quantity int32, pricePerUnit, stopPrice *string, txs []txRow) decimal.Decimal {
	zero := decimal.Zero
	cs := decimal.NewFromInt(int64(contractSize))
	qty := decimal.NewFromInt(int64(quantity))

	switch orderType {
	case "LIMIT", "STOP_LIMIT":
		if pricePerUnit == nil {
			return zero
		}
		ppu, err := decimal.NewFromString(*pricePerUnit)
		if err != nil {
			return zero
		}
		notional := cs.Mul(ppu).Mul(qty)
		return trading.CalcLimitCommission(notional)

	case "STOP":
		if stopPrice == nil {
			return zero
		}
		sp, err := decimal.NewFromString(*stopPrice)
		if err != nil {
			return zero
		}
		notional := cs.Mul(sp).Mul(qty)
		return trading.CalcMarketCommission(notional)

	case "MARKET":
		if len(txs) == 0 {
			return zero
		}
		var notional decimal.Decimal
		for _, tx := range txs {
			ep, err := decimal.NewFromString(tx.ExecutedPrice)
			if err != nil {
				continue
			}
			eqty := decimal.NewFromInt(int64(tx.ExecutedQuantity))
			notional = notional.Add(cs.Mul(ep).Mul(eqty))
		}
		return trading.CalcMarketCommission(notional)
	}

	return zero
}

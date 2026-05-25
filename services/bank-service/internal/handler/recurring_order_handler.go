package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"banka-backend/services/bank-service/internal/domain"
	auth "banka-backend/shared/auth"

	"gorm.io/gorm"
)

// RecurringOrderHandler serves REST endpoints for recurring order (DCA) management.
//
//	POST   /bank/recurring-orders        — create
//	GET    /bank/recurring-orders        — list caller's orders
//	GET    /bank/recurring-orders/{id}   — get one
//	PATCH  /bank/recurring-orders/{id}   — update (active/value/nextRun)
//	DELETE /bank/recurring-orders/{id}   — cancel
type RecurringOrderHandler struct {
	repo      domain.RecurringOrderRepository
	db        *gorm.DB
	jwtSecret string
}

func NewRecurringOrderHandler(repo domain.RecurringOrderRepository, db *gorm.DB, jwtSecret string) *RecurringOrderHandler {
	return &RecurringOrderHandler{repo: repo, db: db, jwtSecret: jwtSecret}
}

type recurringOrderJSON struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"userId"`
	ListingID int64     `json:"listingId"`
	Ticker    string    `json:"ticker"`
	Direction string    `json:"direction"`
	Mode      string    `json:"mode"`
	Value     float64   `json:"value"`
	AccountID int64     `json:"accountId"`
	Cadence   string    `json:"cadence"`
	NextRun   time.Time `json:"nextRun"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"createdAt"`
}

func (h *RecurringOrderHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid user id in token"}`, http.StatusUnauthorized)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/bank/recurring-orders")
	path = strings.TrimSuffix(path, "/")

	if path == "" {
		switch r.Method {
		case http.MethodGet:
			h.list(w, r, userID)
		case http.MethodPost:
			h.create(w, r, userID)
		default:
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		}
		return
	}

	id, parseErr := strconv.ParseInt(strings.TrimPrefix(path, "/"), 10, 64)
	if parseErr != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getOne(w, r, id, userID)
	case http.MethodPatch:
		h.update(w, r, id, userID)
	case http.MethodDelete:
		h.delete(w, r, id, userID)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (h *RecurringOrderHandler) list(w http.ResponseWriter, _ *http.Request, userID int64) {
	orders, err := h.repo.List(userID)
	if err != nil {
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}
	result := make([]recurringOrderJSON, 0, len(orders))
	for _, o := range orders {
		result = append(result, h.enrich(o))
	}
	recurringWriteJSON(w, http.StatusOK, map[string]any{"recurringOrders": result})
}

func (h *RecurringOrderHandler) create(w http.ResponseWriter, r *http.Request, userID int64) {
	var body struct {
		ListingID int64     `json:"listingId"`
		Direction string    `json:"direction"`
		Mode      string    `json:"mode"`
		Value     float64   `json:"value"`
		AccountID int64     `json:"accountId"`
		IsClient  bool      `json:"isClient"`
		Cadence   string    `json:"cadence"`
		NextRun   time.Time `json:"nextRun"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if body.ListingID == 0 || body.Direction == "" || body.Mode == "" || body.Value <= 0 || body.AccountID == 0 || body.Cadence == "" {
		http.Error(w, `{"error":"missing required fields"}`, http.StatusBadRequest)
		return
	}

	order := domain.RecurringOrder{
		UserID:    userID,
		ListingID: body.ListingID,
		Direction: body.Direction,
		Mode:      body.Mode,
		Value:     body.Value,
		AccountID: body.AccountID,
		IsClient:  body.IsClient,
		Cadence:   body.Cadence,
		NextRun:   body.NextRun,
		Active:    true,
	}
	created, err := h.repo.Create(order)
	if err != nil {
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}
	recurringWriteJSON(w, http.StatusCreated, h.enrich(*created))
}

func (h *RecurringOrderHandler) getOne(w http.ResponseWriter, _ *http.Request, id, userID int64) {
	order, err := h.repo.GetByID(id)
	if err != nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	if order.UserID != userID {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		return
	}
	recurringWriteJSON(w, http.StatusOK, h.enrich(*order))
}

func (h *RecurringOrderHandler) update(w http.ResponseWriter, r *http.Request, id, userID int64) {
	order, err := h.repo.GetByID(id)
	if err != nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	if order.UserID != userID {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		return
	}

	var body struct {
		Active  *bool    `json:"active"`
		Value   *float64 `json:"value"`
		NextRun *string  `json:"nextRun"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if body.Active != nil {
		order.Active = *body.Active
	}
	if body.Value != nil {
		order.Value = *body.Value
	}
	if body.NextRun != nil {
		t, parseErr := time.Parse(time.RFC3339, *body.NextRun)
		if parseErr != nil {
			http.Error(w, `{"error":"invalid nextRun format, use RFC3339"}`, http.StatusBadRequest)
			return
		}
		order.NextRun = t
	}

	if err := h.repo.Update(*order); err != nil {
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}
	recurringWriteJSON(w, http.StatusOK, h.enrich(*order))
}

func (h *RecurringOrderHandler) delete(w http.ResponseWriter, _ *http.Request, id, userID int64) {
	order, err := h.repo.GetByID(id)
	if err != nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	if order.UserID != userID {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		return
	}
	if err := h.repo.Delete(id, userID); err != nil {
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *RecurringOrderHandler) enrich(o domain.RecurringOrder) recurringOrderJSON {
	var ticker string
	h.db.Raw("SELECT ticker FROM core_banking.listing WHERE id = ?", o.ListingID).Scan(&ticker)
	return recurringOrderJSON{
		ID:        o.ID,
		UserID:    o.UserID,
		ListingID: o.ListingID,
		Ticker:    ticker,
		Direction: o.Direction,
		Mode:      o.Mode,
		Value:     o.Value,
		AccountID: o.AccountID,
		Cadence:   o.Cadence,
		NextRun:   o.NextRun,
		Active:    o.Active,
		CreatedAt: o.CreatedAt,
	}
}

func (h *RecurringOrderHandler) authenticate(w http.ResponseWriter, r *http.Request) (*auth.AccessClaims, bool) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return nil, false
	}
	claims, err := auth.VerifyToken(strings.TrimPrefix(authHeader, "Bearer "), h.jwtSecret)
	if err != nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return nil, false
	}
	return claims, true
}

func recurringWriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

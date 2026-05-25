package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"banka-backend/services/bank-service/internal/domain"
	"banka-backend/services/bank-service/internal/service"
	auth "banka-backend/shared/auth"
)

// PriceAlertHandler serves REST endpoints for price alert management.
//
//	POST   /bank/price-alerts           — create alert
//	GET    /bank/price-alerts           — list caller's alerts
//	DELETE /bank/price-alerts/{id}      — delete alert
type PriceAlertHandler struct {
	svc       *service.PriceAlertService
	jwtSecret string
}

func NewPriceAlertHandler(svc *service.PriceAlertService, jwtSecret string) *PriceAlertHandler {
	return &PriceAlertHandler{svc: svc, jwtSecret: jwtSecret}
}

func (h *PriceAlertHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		http.Error(w, "invalid user id in token", http.StatusUnauthorized)
		return
	}

	// Route: DELETE /bank/price-alerts/{id}
	path := strings.TrimPrefix(r.URL.Path, "/bank/price-alerts")
	if path != "" && path != "/" {
		id, parseErr := strconv.ParseInt(strings.Trim(path, "/"), 10, 64)
		if parseErr != nil {
			http.Error(w, "invalid alert id", http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := h.svc.DeleteAlert(id, userID); err != nil {
			if errors.Is(err, domain.ErrPriceAlertNotFound) {
				http.Error(w, "alert not found", http.StatusNotFound)
				return
			}
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.list(w, r, userID)
	case http.MethodPost:
		h.create(w, r, userID, claims.Email)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *PriceAlertHandler) list(w http.ResponseWriter, _ *http.Request, userID int64) {
	alerts, err := h.svc.ListAlerts(userID)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	alertWriteJSON(w, http.StatusOK, map[string]any{"alerts": alerts})
}

func (h *PriceAlertHandler) create(w http.ResponseWriter, r *http.Request, userID int64, email string) {
	var body struct {
		ListingID int64   `json:"listingId"`
		Threshold float64 `json:"threshold"`
		Direction string  `json:"direction"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	alert, err := h.svc.CreateAlert(r.Context(), service.CreatePriceAlertRequest{
		UserID:    userID,
		ListingID: body.ListingID,
		Threshold: body.Threshold,
		Direction: domain.PriceAlertDirection(body.Direction),
		Email:     email,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	alertWriteJSON(w, http.StatusCreated, alert)
}

func (h *PriceAlertHandler) authenticate(w http.ResponseWriter, r *http.Request) (*auth.AccessClaims, bool) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return nil, false
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	claims, err := auth.VerifyToken(token, h.jwtSecret)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return nil, false
	}
	return claims, true
}

func alertWriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

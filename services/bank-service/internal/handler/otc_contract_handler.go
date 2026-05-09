package handler

// otc_contract_handler.go — HTTP handler za OTC ugovore i njihovo SAGA izvršavanje.
//
// Endpoints:
//   GET  /api/otc/contracts         — lista svih ugovora korisnika (sa Profit, SellerInfo)
//   GET  /api/otc/contracts/{id}    — detalj jednog ugovora
//   POST /api/otc/contracts/{id}/execute — pokreće SAGA tok za "Iskoristi"
//
// Auth: Bearer JWT, isti pattern kao u otc_handler.go.
// SellerInfo se rezolvira best-effort pozivom na user-service (UserServiceClient).
// Ako user-service ne odgovori, vraćamo prazan string — FE prikazuje fallback.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"banka-backend/services/bank-service/internal/domain"
	"banka-backend/services/bank-service/internal/transport"
	auth "banka-backend/shared/auth"

	"google.golang.org/grpc/metadata"
)

// OTCContractHandler opslužuje /api/otc/contracts* rute.
type OTCContractHandler struct {
	svc        domain.OTCContractService
	userClient *transport.UserServiceClient // može biti nil
	jwtSecret  string
}

// NewOTCContractHandler kreira handler sa zavisnostima.
// userClient može biti nil — u tom slučaju SellerName/SellerBankName ostaju prazni.
func NewOTCContractHandler(
	svc domain.OTCContractService,
	userClient *transport.UserServiceClient,
	jwtSecret string,
) *OTCContractHandler {
	return &OTCContractHandler{svc: svc, userClient: userClient, jwtSecret: jwtSecret}
}

// ServeHTTP dispetcher za /api/otc/contracts*.
func (h *OTCContractHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	callerID, err := h.authenticate(r)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/otc/contracts")
	path = strings.TrimSuffix(path, "/")

	// GET /api/otc/contracts — lista
	if path == "" {
		if r.Method != http.MethodGet {
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.handleList(w, r, callerID)
		return
	}

	// /api/otc/contracts/{id}[/execute]
	parts := strings.SplitN(strings.TrimPrefix(path, "/"), "/", 2)
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "neispravan ID ugovora")
		return
	}

	if len(parts) == 1 {
		// GET /api/otc/contracts/{id}
		if r.Method != http.MethodGet {
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.handleGet(w, r, id, callerID)
		return
	}

	if parts[1] == "execute" && r.Method == http.MethodPost {
		h.handleExecute(w, r, id, callerID)
		return
	}

	writeJSONError(w, http.StatusNotFound, "not found")
}

// ─── GET /api/otc/contracts ───────────────────────────────────────────────────

func (h *OTCContractHandler) handleList(w http.ResponseWriter, r *http.Request, callerID int64) {
	items, err := h.svc.ListContracts(r.Context(), callerID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "greška pri dohvatu ugovora")
		return
	}
	out := make([]contractListItemDTO, 0, len(items))
	for _, it := range items {
		it.SellerName = h.resolveUserName(r, it.SellerID)
		it.SellerInfo = buildSellerInfo(it.SellerName, it.SellerBankName)
		dto := toContractListItemDTO(it)
		dto.BuyerName = h.resolveUserName(r, it.BuyerID)
		out = append(out, dto)
	}
	writeOTCJSON(w, http.StatusOK, out)
}

// ─── GET /api/otc/contracts/{id} ─────────────────────────────────────────────

func (h *OTCContractHandler) handleGet(w http.ResponseWriter, r *http.Request, id, callerID int64) {
	item, err := h.svc.GetContract(r.Context(), id, callerID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	item.SellerName = h.resolveUserName(r, item.SellerID)
	item.SellerInfo = buildSellerInfo(item.SellerName, item.SellerBankName)
	dto := toContractListItemDTO(*item)
	dto.BuyerName = h.resolveUserName(r, item.BuyerID)
	writeOTCJSON(w, http.StatusOK, dto)
}

// ─── POST /api/otc/contracts/{id}/execute ─────────────────────────────────────

type executeResponse struct {
	Message    string `json:"message"`
	SagaID     int64  `json:"sagaId"`
	ContractID int64  `json:"contractId"`
	Status     string `json:"status"`
}

func (h *OTCContractHandler) handleExecute(w http.ResponseWriter, r *http.Request, id, callerID int64) {
	exec, err := h.svc.ExerciseContract(r.Context(), domain.ExerciseOTCContractInput{
		ContractID: id,
		CallerID:   callerID,
	})
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeOTCJSON(w, http.StatusAccepted, executeResponse{
		Message:    "SAGA izvršavanje pokrenuto. Pratite status putem sagaId.",
		SagaID:     exec.ID,
		ContractID: exec.ContractID,
		Status:     string(exec.Status),
	})
}

// ─── DTO ──────────────────────────────────────────────────────────────────────

type contractListItemDTO struct {
	ID              int64   `json:"id"`
	OfferID         int64   `json:"offerId"`
	ListingID       int64   `json:"listingId"`
	Ticker          string  `json:"ticker"`
	StockName       string  `json:"stockName"`
	Exchange        string  `json:"exchange,omitempty"`
	SellerID        int64   `json:"sellerId"`
	BuyerID         int64   `json:"buyerId"`
	BuyerAccountID  int64   `json:"buyerAccountId"`
	SellerAccountID int64   `json:"sellerAccountId"`
	Amount          int32   `json:"amount"`
	StrikePrice     float64 `json:"strikePrice"`
	Premium         float64 `json:"premium"`
	SettlementDate  string  `json:"settlementDate"`
	Status          string  `json:"status"`
	CreatedAt       string  `json:"createdAt"`
	ExercisedAt     *string `json:"exercisedAt,omitempty"`
	// Izvedene vrednosti
	CurrentPrice   float64 `json:"currentPrice"`
	Profit         float64 `json:"profit"`
	SellerInfo     string  `json:"sellerInfo"`
	SellerName     string  `json:"sellerName"`
	SellerBankName string  `json:"sellerBankName"`
	BuyerName      string  `json:"buyerName,omitempty"`
}

func toContractListItemDTO(it domain.OTCContractListItem) contractListItemDTO {
	var exercisedAt *string
	if it.ExercisedAt != nil {
		s := it.ExercisedAt.UTC().Format(time.RFC3339)
		exercisedAt = &s
	}
	return contractListItemDTO{
		ID:              it.ID,
		OfferID:         it.OfferID,
		ListingID:       it.ListingID,
		Ticker:          it.Ticker,
		StockName:       it.StockName,
		Exchange:        it.Exchange,
		SellerID:        it.SellerID,
		BuyerID:         it.BuyerID,
		BuyerAccountID:  it.BuyerAccountID,
		SellerAccountID: it.SellerAccountID,
		Amount:          it.Amount,
		StrikePrice:     it.StrikePrice,
		Premium:         it.Premium,
		SettlementDate:  it.SettlementDate.Format("2006-01-02"),
		Status:          string(it.Status),
		CreatedAt:       it.CreatedAt.UTC().Format(time.RFC3339),
		ExercisedAt:     exercisedAt,
		CurrentPrice:    it.CurrentPrice,
		Profit:          it.Profit,
		SellerInfo:      it.SellerInfo,
		SellerName:      it.SellerName,
		SellerBankName:  it.SellerBankName,
	}
}

// ─── Pomoćne metode ───────────────────────────────────────────────────────────

func (h *OTCContractHandler) authenticate(r *http.Request) (int64, error) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return 0, errors.New("no token")
	}
	claims, err := auth.VerifyToken(strings.TrimPrefix(authHeader, "Bearer "), h.jwtSecret)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(claims.Subject, 10, 64)
}

// resolveUserName dohvata "Ime Prezime" za userID iz user-service.
// Koristi service token (EMPLOYEE+SUPERVISOR) za gRPC poziv — korisnikov token
// možda nema potrebne dozvole (CLIENT ne može gledati tuđe podatke).
func (h *OTCContractHandler) resolveUserName(r *http.Request, userID int64) string {
	if h.userClient == nil || userID == 0 {
		return ""
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+serviceToken(h.jwtSecret)))
	if info, err := h.userClient.GetClientInfo(ctx, userID); err == nil && info != nil {
		return strings.TrimSpace(info.FirstName + " " + info.LastName)
	}
	if info, err := h.userClient.GetEmployeeInfo(ctx, userID); err == nil && info != nil {
		return strings.TrimSpace(info.FirstName + " " + info.LastName)
	}
	return ""
}

// buildSellerInfo formatira "Ime Prezime, BankaNaziv" po specifikaciji.
func buildSellerInfo(sellerName, bankName string) string {
	if sellerName == "" && bankName == "" {
		return ""
	}
	if sellerName == "" {
		return bankName
	}
	if bankName == "" {
		return sellerName
	}
	return sellerName + ", " + bankName
}

func (h *OTCContractHandler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrOTCContractNotFound):
		writeJSONError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, domain.ErrOTCContractExpired),
		errors.Is(err, domain.ErrOTCContractAlreadyExecuted),
		errors.Is(err, domain.ErrOTCSagaAlreadyRunning),
		errors.Is(err, domain.ErrOTCInsufficientFunds),
		errors.Is(err, domain.ErrOTCInsufficientCapacity):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, domain.ErrOTCContractNotBuyer),
		errors.Is(err, domain.ErrOTCNotParticipant):
		writeJSONError(w, http.StatusForbidden, err.Error())
	default:
		writeJSONError(w, http.StatusInternalServerError, "interna greška")
	}
}

// ─── JSON helpers (ponovo definisani jer su lokalni za package handler) ──────

func writeContractJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

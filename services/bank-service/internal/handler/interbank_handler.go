// Package handler — interbank_handler.go
//
// HTTP handler-i za si-tx-proto:
//
//   POST   /interbank                                — primanje NEW_TX/COMMIT_TX/ROLLBACK_TX
//   GET    /public-stock                             — javne akcije za OTC
//   POST   /negotiations                             — kreiranje OTC pregovaranja
//   PUT    /negotiations/{routing}/{id}              — kontraponuda
//   GET    /negotiations/{routing}/{id}              — stanje
//   DELETE /negotiations/{routing}/{id}              — povlačenje (zatvaranje)
//   GET    /negotiations/{routing}/{id}/accept       — prihvatanje
//   GET    /user/{routing}/{id}                      — display name korisnika
//
// Sve dolazne zahteve validiramo preko X-Api-Key header-a (mora biti
// jednak našem InterbankAPIKey-u). Svi odgovori su JSON; status kodovi
// strogo po protokolu (200/202/204/404/409).
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"banka-backend/services/bank-service/internal/domain"
	"banka-backend/services/bank-service/internal/service"
	"banka-backend/services/bank-service/internal/transport"
)

// InterbankHandler ujedinjuje svih protokol-ranih endpoint-a.
type InterbankHandler struct {
	coordinator      *service.TransactionCoordinator
	otcService       *service.InterbankOTCService
	optionService    *service.InterbankOptionContractService
	repo             domain.InterbankRepository
	apiKey           string
	ourRoutingNumber int64
	bankDisplayName  string
	userClient       *transport.UserServiceClient
}

// NewInterbankHandler konstruktor.
func NewInterbankHandler(
	coordinator *service.TransactionCoordinator,
	otc *service.InterbankOTCService,
	option *service.InterbankOptionContractService,
	repo domain.InterbankRepository,
	apiKey string,
	ourRoutingNumber int64,
	bankDisplayName string,
	userClient *transport.UserServiceClient,
) *InterbankHandler {
	return &InterbankHandler{
		coordinator:      coordinator,
		otcService:       otc,
		optionService:    option,
		repo:             repo,
		apiKey:           apiKey,
		ourRoutingNumber: ourRoutingNumber,
		bankDisplayName:  bankDisplayName,
		userClient:       userClient,
	}
}

// ─── X-Api-Key middleware ────────────────────────────────────────────────────

func (h *InterbankHandler) checkAPIKey(r *http.Request) bool {
	if h.apiKey == "" {
		return true // ako nije podešen, ne prinudimo (developer mode)
	}
	return r.Header.Get("X-Api-Key") == h.apiKey
}

func writeIBJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(body)
}

func writeIBError(w http.ResponseWriter, status int, msg string) {
	writeIBJSON(w, status, map[string]string{"error": msg})
}

// ─── POST /interbank ─────────────────────────────────────────────────────────

func (h *InterbankHandler) HandleInterbank(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeIBError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.checkAPIKey(r) {
		writeIBError(w, http.StatusUnauthorized, "invalid X-Api-Key")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeIBError(w, http.StatusBadRequest, "ne mogu pročitati telo")
		return
	}
	defer r.Body.Close()

	var msg domain.InterbankMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		writeIBError(w, http.StatusBadRequest, "nevalidan JSON: "+err.Error())
		return
	}
	if msg.IdempotenceKey.RoutingNumber == 0 || msg.IdempotenceKey.LocallyGeneratedKey == "" {
		writeIBError(w, http.StatusBadRequest, "idempotenceKey obavezan")
		return
	}
	if len(msg.IdempotenceKey.LocallyGeneratedKey) > 64 {
		writeIBError(w, http.StatusBadRequest, "locallyGeneratedKey > 64 bajta")
		return
	}

	statusCode, response, err := h.coordinator.HandleIncomingMessage(r.Context(), msg)
	if err != nil {
		writeIBError(w, statusCode, err.Error())
		return
	}
	writeIBJSON(w, statusCode, response)
}

// ─── GET /public-stock ───────────────────────────────────────────────────────

func (h *InterbankHandler) HandlePublicStock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeIBError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.checkAPIKey(r) {
		writeIBError(w, http.StatusUnauthorized, "invalid X-Api-Key")
		return
	}
	stocks, err := h.repo.ListPublicStocks(r.Context())
	if err != nil {
		writeIBError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Popuni routingNumber prodavaca našim routing-om.
	for i := range stocks {
		for j := range stocks[i].Sellers {
			stocks[i].Sellers[j].Seller.RoutingNumber = h.ourRoutingNumber
		}
	}
	writeIBJSON(w, http.StatusOK, stocks)
}

// ─── /negotiations dispatcher ────────────────────────────────────────────────

func (h *InterbankHandler) HandleNegotiations(w http.ResponseWriter, r *http.Request) {
	if !h.checkAPIKey(r) {
		writeIBError(w, http.StatusUnauthorized, "invalid X-Api-Key")
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/negotiations")
	rest = strings.TrimPrefix(rest, "/")
	parts := strings.Split(rest, "/")
	// Putanje:
	//   ""                              POST  → create
	//   "{routing}/{id}"                GET / PUT / DELETE
	//   "{routing}/{id}/accept"         GET   → accept
	if rest == "" {
		if r.Method == http.MethodPost {
			h.handleNegotiationCreate(w, r)
			return
		}
		writeIBError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if len(parts) < 2 {
		writeIBError(w, http.StatusBadRequest, "URL: /negotiations/{routing}/{id}[/accept]")
		return
	}
	routing, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeIBError(w, http.StatusBadRequest, "nevalidan routingNumber")
		return
	}
	id := parts[1]
	if len(parts) == 3 && parts[2] == "accept" {
		if r.Method != http.MethodGet {
			writeIBError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.handleNegotiationAccept(w, r, routing, id)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.handleNegotiationGet(w, r, routing, id)
	case http.MethodPut:
		h.handleNegotiationCounter(w, r, routing, id)
	case http.MethodDelete:
		h.handleNegotiationCancel(w, r, routing, id)
	default:
		writeIBError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *InterbankHandler) handleNegotiationCreate(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeIBError(w, http.StatusBadRequest, "ne mogu pročitati telo")
		return
	}
	defer r.Body.Close()
	var offer domain.OtcOffer
	if err := json.Unmarshal(body, &offer); err != nil {
		writeIBError(w, http.StatusBadRequest, "nevalidan OtcOffer: "+err.Error())
		return
	}
	id, err := h.otcService.CreateNegotiation(r.Context(), offer)
	if err != nil {
		writeIBError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeIBJSON(w, http.StatusOK, id)
}

func (h *InterbankHandler) handleNegotiationGet(w http.ResponseWriter, r *http.Request, routing int64, id string) {
	n, err := h.otcService.GetNegotiation(r.Context(), routing, id)
	if err != nil {
		if errors.Is(err, domain.ErrInterbankNotFound) {
			writeIBError(w, http.StatusNotFound, "negotiation not found")
			return
		}
		writeIBError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeIBJSON(w, http.StatusOK, n)
}

func (h *InterbankHandler) handleNegotiationCounter(w http.ResponseWriter, r *http.Request, routing int64, id string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeIBError(w, http.StatusBadRequest, "ne mogu pročitati telo")
		return
	}
	defer r.Body.Close()
	var offer domain.OtcOffer
	if err := json.Unmarshal(body, &offer); err != nil {
		writeIBError(w, http.StatusBadRequest, "nevalidan OtcOffer: "+err.Error())
		return
	}
	if err := h.otcService.CounterNegotiation(r.Context(), routing, id, offer); err != nil {
		switch {
		case errors.Is(err, domain.ErrInterbankNotFound):
			writeIBError(w, http.StatusNotFound, "negotiation not found")
		case errors.Is(err, domain.ErrInterbankConflict):
			writeIBError(w, http.StatusConflict, err.Error())
		default:
			writeIBError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *InterbankHandler) handleNegotiationCancel(w http.ResponseWriter, r *http.Request, routing int64, id string) {
	if err := h.otcService.CancelNegotiation(r.Context(), routing, id); err != nil {
		switch {
		case errors.Is(err, domain.ErrInterbankNotFound):
			writeIBError(w, http.StatusNotFound, "negotiation not found")
		default:
			writeIBError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *InterbankHandler) handleNegotiationAccept(w http.ResponseWriter, r *http.Request, routing int64, id string) {
	if err := h.optionService.AcceptNegotiation(r.Context(), routing, id); err != nil {
		switch {
		case errors.Is(err, domain.ErrInterbankNotFound):
			writeIBError(w, http.StatusNotFound, "negotiation not found")
		case errors.Is(err, domain.ErrInterbankConflict):
			writeIBError(w, http.StatusConflict, err.Error())
		default:
			writeIBError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── GET /user/{routing}/{id} ────────────────────────────────────────────────

func (h *InterbankHandler) HandleUserInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeIBError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.checkAPIKey(r) {
		writeIBError(w, http.StatusUnauthorized, "invalid X-Api-Key")
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/user/")
	parts := strings.Split(rest, "/")
	if len(parts) < 2 {
		writeIBError(w, http.StatusBadRequest, "URL: /user/{routing}/{id}")
		return
	}
	routing, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeIBError(w, http.StatusBadRequest, "nevalidan routingNumber")
		return
	}
	if routing != h.ourRoutingNumber {
		writeIBError(w, http.StatusNotFound, "user not in this bank")
		return
	}
	userIDInt, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		writeIBError(w, http.StatusNotFound, "user not found")
		return
	}
	displayName := lookupUserDisplayName(r.Context(), h.userClient, userIDInt)
	if displayName == "" {
		writeIBError(w, http.StatusNotFound, "user not found")
		return
	}
	writeIBJSON(w, http.StatusOK, domain.UserInfoResponse{
		BankDisplayName: h.bankDisplayName,
		DisplayName:     displayName,
	})
}

// lookupUserDisplayName best-effort poziv user-service-a; ako nije dostupan,
// vraća neutralan string "User <id>" da ne padne ceo endpoint.
func lookupUserDisplayName(ctx context.Context, userClient *transport.UserServiceClient, userID int64) string {
	if userClient == nil {
		return fmt.Sprintf("User %d", userID)
	}
	first, last, err := userClient.GetClientName(ctx, userID)
	if err != nil || (first == "" && last == "") {
		return fmt.Sprintf("User %d", userID)
	}
	return strings.TrimSpace(first + " " + last)
}

// suppress unused import warning for time when build trims.
var _ = time.Now

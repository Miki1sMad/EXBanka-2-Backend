// Package handler — interbank_payment_handler.go
//
// Klijentski endpoint-i nad interbank modulom (autentifikacija JWT):
//
//   POST /bank/interbank/payments         — kreiranje međubankarskog plaćanja
//   GET  /bank/interbank/public-stocks    — agregirana lista (lokalno + peer banka)
//   POST /bank/interbank/negotiations             — kupac inicira pregovaranje (forward to peer)
//   PUT  /bank/interbank/negotiations/{routing}/{id}   — kontraponuda
//   GET  /bank/interbank/negotiations/{routing}/{id}   — stanje
//   DELETE /bank/interbank/negotiations/{routing}/{id} — povlačenje
//   POST /bank/interbank/negotiations/{routing}/{id}/accept — accept
//   GET  /bank/interbank/contracts        — moji opcioni ugovori
//   POST /bank/interbank/contracts/{routing}/{id}/exercise — izvršavanje opcije
package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"banka-backend/services/bank-service/internal/domain"
	"banka-backend/services/bank-service/internal/service"
	"banka-backend/services/bank-service/internal/transport"
	auth "banka-backend/shared/auth"

	"github.com/shopspring/decimal"
)

// InterbankPaymentHandler — JWT-autentikovan handler za interbank operacije.
type InterbankPaymentHandler struct {
	coordinator      *service.TransactionCoordinator
	otcSvc           *service.InterbankOTCService
	optionSvc        *service.InterbankOptionContractService
	client           domain.InterbankClient
	repo             domain.InterbankRepository
	jwtSecret        string
	ourRoutingNumber int64
	userClient       *transport.UserServiceClient
}

// NewInterbankPaymentHandler konstruktor.
func NewInterbankPaymentHandler(
	coordinator *service.TransactionCoordinator,
	otc *service.InterbankOTCService,
	option *service.InterbankOptionContractService,
	client domain.InterbankClient,
	repo domain.InterbankRepository,
	jwtSecret string,
	ourRoutingNumber int64,
	userClient *transport.UserServiceClient,
) *InterbankPaymentHandler {
	return &InterbankPaymentHandler{
		coordinator:      coordinator,
		otcSvc:           otc,
		optionSvc:        option,
		client:           client,
		repo:             repo,
		jwtSecret:        jwtSecret,
		ourRoutingNumber: ourRoutingNumber,
		userClient:       userClient,
	}
}

// authenticate izvlači userID i tip korisnika iz JWT-a.
func (h *InterbankPaymentHandler) authenticate(r *http.Request) (int64, string, error) {
	hdr := r.Header.Get("Authorization")
	if !strings.HasPrefix(hdr, "Bearer ") {
		return 0, "", errors.New("no token")
	}
	claims, err := auth.VerifyToken(strings.TrimPrefix(hdr, "Bearer "), h.jwtSecret)
	if err != nil {
		return 0, "", err
	}
	id, err := strconv.ParseInt(claims.Subject, 10, 64)
	return id, claims.UserType, err
}

func (h *InterbankPaymentHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	userID, _, err := h.authenticate(r)
	if err != nil {
		writeIBError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	path := r.URL.Path
	switch {
	case path == "/bank/interbank/payments" && r.Method == http.MethodPost:
		h.handleCreatePayment(w, r, userID)
	case path == "/bank/interbank/public-stocks" && r.Method == http.MethodGet:
		h.handlePublicStocks(w, r)
	case path == "/bank/interbank/negotiations" && r.Method == http.MethodPost:
		h.handleCreateNegotiation(w, r, userID)
	case path == "/bank/interbank/contracts" && r.Method == http.MethodGet:
		h.handleListContracts(w, r, userID)
	case strings.HasPrefix(path, "/bank/interbank/negotiations/"):
		h.handleNegotiationsSubpath(w, r, userID)
	case strings.HasPrefix(path, "/bank/interbank/contracts/"):
		h.handleContractsSubpath(w, r, userID)
	default:
		writeIBError(w, http.StatusNotFound, "not found")
	}
}

// ─── POST /bank/interbank/payments ───────────────────────────────────────────

type createInterbankPaymentReq struct {
	SenderAccountID    int64           `json:"senderAccountId"`
	RecipientAccountNo string          `json:"recipientAccountNumber"`
	RecipientName      string          `json:"recipientName"`
	Amount             decimal.Decimal `json:"amount"`
	Currency           string          `json:"currency"`
	PaymentCode        string          `json:"paymentCode"`
	PaymentPurpose     string          `json:"paymentPurpose"`
	CallNumber         string          `json:"callNumber"`
	Message            string          `json:"message"`
}

type createInterbankPaymentRes struct {
	InterbankTxID            int64  `json:"interbankTxId"`
	TransactionRoutingNumber int64  `json:"transactionRoutingNumber"`
	TransactionForeignID     string `json:"transactionForeignId"`
	Status                   string `json:"status"`
	FailureReason            string `json:"failureReason,omitempty"`
}

func (h *InterbankPaymentHandler) handleCreatePayment(w http.ResponseWriter, r *http.Request, userID int64) {
	var req createInterbankPaymentReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeIBError(w, http.StatusBadRequest, "nevalidan JSON: "+err.Error())
		return
	}
	if req.RecipientAccountNo == "" || req.Amount.IsZero() || req.Currency == "" {
		writeIBError(w, http.StatusBadRequest, "račun primaoca, iznos i valuta su obavezni")
		return
	}
	in := domain.InterbankPaymentInput{
		SenderAccountID:    req.SenderAccountID,
		SenderUserID:       userID,
		RecipientAccountNo: req.RecipientAccountNo,
		RecipientName:      req.RecipientName,
		Amount:             req.Amount,
		Currency:           req.Currency,
		PaymentCode:        req.PaymentCode,
		PaymentPurpose:     req.PaymentPurpose,
		CallNumber:         req.CallNumber,
		Message:            req.Message,
	}
	ibTx, err := h.coordinator.InitiateInterbankPayment(r.Context(), in)
	if err != nil {
		// I dalje vraćamo telo sa informacijom o tx zapisu radi UI.
		status := http.StatusInternalServerError
		if errors.Is(err, domain.ErrInterbankPeerNotConfigured) {
			status = http.StatusServiceUnavailable
		}
		var failure string
		var routing int64
		var fid string
		var ibStatus string
		if ibTx != nil {
			routing = ibTx.TransactionRoutingNumber
			fid = ibTx.TransactionForeignID
			ibStatus = string(ibTx.Status)
			failure = ibTx.FailureReason
		}
		writeIBJSON(w, status, createInterbankPaymentRes{
			TransactionRoutingNumber: routing,
			TransactionForeignID:     fid,
			Status:                   ibStatus,
			FailureReason:            failure,
		})
		_ = err // logged
		return
	}
	writeIBJSON(w, http.StatusOK, createInterbankPaymentRes{
		InterbankTxID:            ibTx.ID,
		TransactionRoutingNumber: ibTx.TransactionRoutingNumber,
		TransactionForeignID:     ibTx.TransactionForeignID,
		Status:                   string(ibTx.Status),
	})
}

// ─── GET /bank/interbank/public-stocks ───────────────────────────────────────

type publicStocksDTO struct {
	Local  []domain.PublicStock `json:"local"`
	Remote []domain.PublicStock `json:"remote"`
}

func (h *InterbankPaymentHandler) handlePublicStocks(w http.ResponseWriter, r *http.Request) {
	local, err := h.repo.ListPublicStocks(r.Context())
	if err != nil {
		writeIBError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for i := range local {
		for j := range local[i].Sellers {
			local[i].Sellers[j].Seller.RoutingNumber = h.ourRoutingNumber
		}
	}
	remote, _ := h.client.GetPublicStock(r.Context()) // best-effort; ignore err
	if remote == nil {
		remote = []domain.PublicStock{}
	}
	writeIBJSON(w, http.StatusOK, publicStocksDTO{Local: local, Remote: remote})
}

// ─── /bank/interbank/negotiations ────────────────────────────────────────────

type createNegotiationReq struct {
	Stock          string          `json:"ticker"`
	SettlementDate string          `json:"settlementDate"`
	PriceCurrency  string          `json:"priceCurrency"`
	PriceAmount    decimal.Decimal `json:"priceAmount"`
	PremiumCurrency string         `json:"premiumCurrency"`
	PremiumAmount   decimal.Decimal `json:"premiumAmount"`
	BuyerRouting   int64           `json:"buyerRoutingNumber"`
	BuyerID        string          `json:"buyerId"`
	SellerRouting  int64           `json:"sellerRoutingNumber"`
	SellerID       string          `json:"sellerId"`
	Amount         int32           `json:"amount"`
}

func (h *InterbankPaymentHandler) handleCreateNegotiation(w http.ResponseWriter, r *http.Request, userID int64) {
	var req createNegotiationReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeIBError(w, http.StatusBadRequest, "nevalidan JSON: "+err.Error())
		return
	}
	if req.BuyerRouting == 0 {
		req.BuyerRouting = h.ourRoutingNumber
		req.BuyerID = strconv.FormatInt(userID, 10)
	}
	offer := domain.OtcOffer{
		Stock:          domain.StockDescription{Ticker: req.Stock},
		SettlementDate: req.SettlementDate,
		PricePerUnit:   domain.MonetaryValue{Currency: req.PriceCurrency, Amount: req.PriceAmount},
		Premium:        domain.MonetaryValue{Currency: req.PremiumCurrency, Amount: req.PremiumAmount},
		BuyerID:        domain.ForeignBankId{RoutingNumber: req.BuyerRouting, ID: req.BuyerID},
		SellerID:       domain.ForeignBankId{RoutingNumber: req.SellerRouting, ID: req.SellerID},
		Amount:         req.Amount,
		LastModifiedBy: domain.ForeignBankId{RoutingNumber: req.BuyerRouting, ID: req.BuyerID},
	}

	// Ako je prodavac na drugoj banci → prosledi.
	if req.SellerRouting != h.ourRoutingNumber {
		id, err := h.client.CreateNegotiation(r.Context(), offer)
		if err != nil {
			writeIBError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeIBJSON(w, http.StatusOK, id)
		return
	}
	// Inače, lokalno smo banka prodavca.
	id, err := h.otcSvc.CreateNegotiation(r.Context(), offer)
	if err != nil {
		writeIBError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeIBJSON(w, http.StatusOK, id)
}

func (h *InterbankPaymentHandler) handleNegotiationsSubpath(w http.ResponseWriter, r *http.Request, userID int64) {
	rest := strings.TrimPrefix(r.URL.Path, "/bank/interbank/negotiations/")
	parts := strings.Split(rest, "/")
	if len(parts) < 2 {
		writeIBError(w, http.StatusBadRequest, "očekuje /bank/interbank/negotiations/{routing}/{id}")
		return
	}
	routing, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeIBError(w, http.StatusBadRequest, "nevalidan routingNumber")
		return
	}
	id := parts[1]
	if len(parts) == 3 && parts[2] == "accept" {
		if r.Method != http.MethodPost && r.Method != http.MethodGet {
			writeIBError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.handleAcceptNegotiation(w, r, routing, id)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.handleGetNegotiation(w, r, routing, id)
	case http.MethodPut:
		h.handleCounterNegotiation(w, r, routing, id, userID)
	case http.MethodDelete:
		h.handleCancelNegotiation(w, r, routing, id)
	default:
		writeIBError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *InterbankPaymentHandler) handleGetNegotiation(w http.ResponseWriter, r *http.Request, routing int64, id string) {
	if routing == h.ourRoutingNumber {
		n, err := h.otcSvc.GetNegotiation(r.Context(), routing, id)
		if err != nil {
			if errors.Is(err, domain.ErrInterbankNotFound) {
				writeIBError(w, http.StatusNotFound, "not found")
				return
			}
			writeIBError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeIBJSON(w, http.StatusOK, n)
		return
	}
	// Forward ka peer banci
	n, err := h.client.GetNegotiation(r.Context(), domain.ForeignBankId{RoutingNumber: routing, ID: id})
	if err != nil {
		if errors.Is(err, domain.ErrInterbankNotFound) {
			writeIBError(w, http.StatusNotFound, "not found")
			return
		}
		writeIBError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeIBJSON(w, http.StatusOK, n)
}

func (h *InterbankPaymentHandler) handleCounterNegotiation(w http.ResponseWriter, r *http.Request, routing int64, id string, userID int64) {
	var req createNegotiationReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeIBError(w, http.StatusBadRequest, "nevalidan JSON: "+err.Error())
		return
	}
	offer := domain.OtcOffer{
		Stock:          domain.StockDescription{Ticker: req.Stock},
		SettlementDate: req.SettlementDate,
		PricePerUnit:   domain.MonetaryValue{Currency: req.PriceCurrency, Amount: req.PriceAmount},
		Premium:        domain.MonetaryValue{Currency: req.PremiumCurrency, Amount: req.PremiumAmount},
		BuyerID:        domain.ForeignBankId{RoutingNumber: req.BuyerRouting, ID: req.BuyerID},
		SellerID:       domain.ForeignBankId{RoutingNumber: req.SellerRouting, ID: req.SellerID},
		Amount:         req.Amount,
		LastModifiedBy: domain.ForeignBankId{RoutingNumber: h.ourRoutingNumber, ID: strconv.FormatInt(userID, 10)},
	}
	if routing == h.ourRoutingNumber {
		if err := h.otcSvc.CounterNegotiation(r.Context(), routing, id, offer); err != nil {
			h.writeNegotiationErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.client.CounterNegotiation(r.Context(), domain.ForeignBankId{RoutingNumber: routing, ID: id}, offer); err != nil {
		h.writeNegotiationErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *InterbankPaymentHandler) handleCancelNegotiation(w http.ResponseWriter, r *http.Request, routing int64, id string) {
	if routing == h.ourRoutingNumber {
		if err := h.otcSvc.CancelNegotiation(r.Context(), routing, id); err != nil {
			h.writeNegotiationErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.client.CancelNegotiation(r.Context(), domain.ForeignBankId{RoutingNumber: routing, ID: id}); err != nil {
		h.writeNegotiationErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *InterbankPaymentHandler) handleAcceptNegotiation(w http.ResponseWriter, r *http.Request, routing int64, id string) {
	if routing == h.ourRoutingNumber {
		if err := h.optionSvc.AcceptNegotiation(r.Context(), routing, id); err != nil {
			h.writeNegotiationErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.client.AcceptNegotiation(r.Context(), domain.ForeignBankId{RoutingNumber: routing, ID: id}); err != nil {
		h.writeNegotiationErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *InterbankPaymentHandler) writeNegotiationErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInterbankNotFound):
		writeIBError(w, http.StatusNotFound, "not found")
	case errors.Is(err, domain.ErrInterbankConflict):
		writeIBError(w, http.StatusConflict, err.Error())
	case errors.Is(err, domain.ErrInterbankPeerNotConfigured):
		writeIBError(w, http.StatusServiceUnavailable, "peer bank URL nije konfigurisan")
	default:
		writeIBError(w, http.StatusInternalServerError, err.Error())
	}
}

// ─── /bank/interbank/contracts ───────────────────────────────────────────────

func (h *InterbankPaymentHandler) handleListContracts(w http.ResponseWriter, r *http.Request, userID int64) {
	rows, err := h.optionSvc.ListContracts(r.Context(), userID)
	if err != nil {
		writeIBError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeIBJSON(w, http.StatusOK, rows)
}

func (h *InterbankPaymentHandler) handleContractsSubpath(w http.ResponseWriter, r *http.Request, userID int64) {
	rest := strings.TrimPrefix(r.URL.Path, "/bank/interbank/contracts/")
	parts := strings.Split(rest, "/")
	if len(parts) < 2 {
		writeIBError(w, http.StatusBadRequest, "očekuje /bank/interbank/contracts/{routing}/{id}[/exercise]")
		return
	}
	routing, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeIBError(w, http.StatusBadRequest, "nevalidan routingNumber")
		return
	}
	id := parts[1]

	if len(parts) == 3 && parts[2] == "exercise" {
		if r.Method != http.MethodPost {
			writeIBError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		ibTx, err := h.optionSvc.ExerciseContract(r.Context(), userID, routing, id)
		if err != nil {
			writeIBError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeIBJSON(w, http.StatusOK, map[string]interface{}{
			"interbankTxId": ibTx.ID,
			"status":        ibTx.Status,
		})
		return
	}
	if r.Method == http.MethodGet {
		c, err := h.optionSvc.GetContract(r.Context(), routing, id)
		if err != nil {
			if errors.Is(err, domain.ErrInterbankNotFound) {
				writeIBError(w, http.StatusNotFound, "not found")
				return
			}
			writeIBError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeIBJSON(w, http.StatusOK, c)
		return
	}
	writeIBError(w, http.StatusMethodNotAllowed, "method not allowed")
	_ = fmt.Sprintf // unused
}

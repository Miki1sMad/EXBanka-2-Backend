package handler

// otc_handler.go — HTTP handler za Fazu 2 (OTC pregovaranje akcijama).
//
// Endpoint-i (verbatim po specifikaciji Faze 2):
//   POST   /api/otc/offers              — kupac inicira ponudu
//   PATCH  /api/otc/offers/{id}/counter — slanje kontraponude
//   PATCH  /api/otc/offers/{id}/accept  — atomski accept + ugovor + premija
//   PATCH  /api/otc/offers/{id}/decline — odbijanje (REJECTED) ili povlačenje (DEACTIVATED)
//   GET    /api/otc/offers              — lista ponuda za trenutnog korisnika
//
// Auth: Bearer JWT, isti pattern kao u my_orders_handler.go (auth.VerifyToken,
// callerID se izvlači iz claims.Subject).

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"banka-backend/services/bank-service/internal/domain"
	auth "banka-backend/shared/auth"
)

// OTCHandler ruta za /api/otc/offers* sa vlastitim malim mux-om.
type OTCHandler struct {
	svc       domain.OTCService
	jwtSecret string
}

func NewOTCHandler(svc domain.OTCService, jwtSecret string) *OTCHandler {
	return &OTCHandler{svc: svc, jwtSecret: jwtSecret}
}

// ServeHTTP dispetcher. Putanju cepamo ručno da bismo ostali konzistentni sa
// ostatkom servisa (net/http servemux bez gin-a).
func (h *OTCHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	callerID, err := h.authenticate(r)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// /api/otc/marketplace — lista javno dostupnih akcija za OTC kupovinu.
	if r.URL.Path == "/api/otc/marketplace" {
		if r.Method != http.MethodGet {
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.handleMarketplace(w, r, callerID)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/otc/offers")
	switch path {
	case "", "/":
		switch r.Method {
		case http.MethodPost:
			h.handleCreate(w, r, callerID)
		case http.MethodGet:
			h.handleList(w, r, callerID)
		default:
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	// /api/otc/offers/{id}[/{action}]
	rest := strings.TrimPrefix(path, "/")
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	idStr := parts[0]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "neispravan ID ponude")
		return
	}

	if len(parts) == 1 {
		// GET /api/otc/offers/{id}
		if r.Method != http.MethodGet {
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		item, err := h.svc.GetOffer(r.Context(), id, callerID)
		if err != nil {
			h.writeServiceError(w, err)
			return
		}
		writeOTCJSON(w, http.StatusOK, listItemDTO(*item, callerID))
		return
	}

	// PATCH /api/otc/offers/{id}/{action}
	if r.Method != http.MethodPatch {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	switch parts[1] {
	case "counter":
		h.handleCounter(w, r, id, callerID)
	case "accept":
		h.handleAccept(w, r, id, callerID)
	case "decline":
		h.handleDecline(w, r, id, callerID)
	default:
		writeJSONError(w, http.StatusNotFound, "nepoznata akcija")
	}
}

// ─── Auth helper ──────────────────────────────────────────────────────────────

func (h *OTCHandler) authenticate(r *http.Request) (int64, error) {
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

// ─── DTO request/response ─────────────────────────────────────────────────────

type createOfferReq struct {
	ListingID      int64   `json:"listingId"`
	SellerID       int64   `json:"sellerId"`
	BuyerAccountID int64   `json:"buyerAccountId"`
	Amount         int32   `json:"amount"`
	PricePerStock  float64 `json:"pricePerStock"`
	Premium        float64 `json:"premium"`
	SettlementDate string  `json:"settlementDate"` // YYYY-MM-DD
}

type counterOfferReq struct {
	Amount          int32   `json:"amount"`
	PricePerStock   float64 `json:"pricePerStock"`
	Premium         float64 `json:"premium"`
	SettlementDate  string  `json:"settlementDate"`
	SellerAccountID *int64  `json:"sellerAccountId,omitempty"`
}

type acceptOfferReq struct {
	SellerAccountID *int64 `json:"sellerAccountId,omitempty"`
}

type offerDTO struct {
	ID              int64   `json:"id"`
	ListingID       int64   `json:"listingId"`
	Ticker          string  `json:"ticker,omitempty"`
	StockName       string  `json:"stockName,omitempty"`
	Exchange        string  `json:"exchange,omitempty"`
	SellerID        int64   `json:"sellerId"`
	BuyerID         int64   `json:"buyerId"`
	BuyerAccountID  int64   `json:"buyerAccountId"`
	SellerAccountID *int64  `json:"sellerAccountId,omitempty"`
	Amount          int32   `json:"amount"`
	PricePerStock   float64 `json:"pricePerStock"`
	Premium         float64 `json:"premium"`
	SettlementDate  string  `json:"settlementDate"`
	Status          string  `json:"status"`
	LastModified    string  `json:"lastModified"`
	ModifiedBy      int64   `json:"modifiedBy"`

	// Vizuelni indikatori (Faza 2): odstupanje cene + boja + needsReview.
	MarketPriceUSD    float64 `json:"marketPriceUsd,omitempty"`
	PriceDeviationPct float64 `json:"priceDeviationPct,omitempty"`
	PriceColor        string  `json:"priceColor,omitempty"`
	NeedsReview       bool    `json:"needsReview"`

	SellerBankID *int64 `json:"sellerBankId,omitempty"`
	BuyerBankID  *int64 `json:"buyerBankId,omitempty"`
}

type contractDTO struct {
	ID              int64   `json:"id"`
	OfferID         int64   `json:"offerId"`
	ListingID       int64   `json:"listingId"`
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
}

func offerToDTO(o domain.OTCOffer, callerID int64) offerDTO {
	return offerDTO{
		ID:              o.ID,
		ListingID:       o.ListingID,
		SellerID:        o.SellerID,
		BuyerID:         o.BuyerID,
		BuyerAccountID:  o.BuyerAccountID,
		SellerAccountID: o.SellerAccountID,
		Amount:          o.Amount,
		PricePerStock:   o.PricePerStock,
		Premium:         o.Premium,
		SettlementDate:  o.SettlementDate.Format("2006-01-02"),
		Status:          string(o.Status),
		LastModified:    o.LastModified.UTC().Format(time.RFC3339),
		ModifiedBy:      o.ModifiedBy,
		NeedsReview:     o.ModifiedBy != callerID,
		SellerBankID:    o.SellerBankID,
		BuyerBankID:     o.BuyerBankID,
	}
}

func listItemDTO(it domain.OTCOfferListItem, callerID int64) offerDTO {
	d := offerToDTO(it.OTCOffer, callerID)
	d.Ticker = it.Ticker
	d.StockName = it.StockName
	d.Exchange = it.Exchange
	d.MarketPriceUSD = it.MarketPriceUSD
	d.PriceDeviationPct = it.PriceDeviationPct
	d.PriceColor = it.PriceColor
	d.NeedsReview = it.NeedsReview
	return d
}

func contractToDTO(c domain.OTCContract) contractDTO {
	return contractDTO{
		ID:              c.ID,
		OfferID:         c.OfferID,
		ListingID:       c.ListingID,
		SellerID:        c.SellerID,
		BuyerID:         c.BuyerID,
		BuyerAccountID:  c.BuyerAccountID,
		SellerAccountID: c.SellerAccountID,
		Amount:          c.Amount,
		StrikePrice:     c.StrikePrice,
		Premium:         c.Premium,
		SettlementDate:  c.SettlementDate.Format("2006-01-02"),
		Status:          string(c.Status),
		CreatedAt:       c.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// ─── Handlers ────────────────────────────────────────────────────────────────

func (h *OTCHandler) handleCreate(w http.ResponseWriter, r *http.Request, callerID int64) {
	var req createOfferReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "neispravan JSON")
		return
	}
	settle, err := time.Parse("2006-01-02", req.SettlementDate)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "neispravan settlementDate (očekivan YYYY-MM-DD)")
		return
	}
	if req.SellerID == 0 || req.ListingID == 0 || req.BuyerAccountID == 0 {
		writeJSONError(w, http.StatusBadRequest, "nedostaje listingId, sellerId ili buyerAccountId")
		return
	}

	offer, err := h.svc.CreateOffer(r.Context(), domain.CreateOTCOfferInput{
		ListingID:      req.ListingID,
		BuyerID:        callerID,
		SellerID:       req.SellerID,
		BuyerAccountID: req.BuyerAccountID,
		Amount:         req.Amount,
		PricePerStock:  req.PricePerStock,
		Premium:        req.Premium,
		SettlementDate: settle,
	})
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeOTCJSON(w, http.StatusCreated, offerToDTO(*offer, callerID))
}

func (h *OTCHandler) handleCounter(w http.ResponseWriter, r *http.Request, id, callerID int64) {
	var req counterOfferReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "neispravan JSON")
		return
	}
	settle, err := time.Parse("2006-01-02", req.SettlementDate)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "neispravan settlementDate")
		return
	}
	offer, err := h.svc.CounterOffer(r.Context(), domain.CounterOTCOfferInput{
		OfferID:         id,
		CallerID:        callerID,
		Amount:          req.Amount,
		PricePerStock:   req.PricePerStock,
		Premium:         req.Premium,
		SettlementDate:  settle,
		SellerAccountID: req.SellerAccountID,
	})
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeOTCJSON(w, http.StatusOK, offerToDTO(*offer, callerID))
}

func (h *OTCHandler) handleAccept(w http.ResponseWriter, r *http.Request, id, callerID int64) {
	var req acceptOfferReq
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "neispravan JSON")
			return
		}
	}
	contract, err := h.svc.AcceptOffer(r.Context(), domain.AcceptOTCOfferInput{
		OfferID:         id,
		CallerID:        callerID,
		SellerAccountID: req.SellerAccountID,
	})
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeOTCJSON(w, http.StatusOK, contractToDTO(*contract))
}

func (h *OTCHandler) handleDecline(w http.ResponseWriter, r *http.Request, id, callerID int64) {
	offer, err := h.svc.DeclineOffer(r.Context(), id, callerID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeOTCJSON(w, http.StatusOK, offerToDTO(*offer, callerID))
}

type marketplaceItemDTO struct {
	ListingID         int64   `json:"listingId"`
	Ticker            string  `json:"ticker"`
	StockName         string  `json:"stockName"`
	Exchange          string  `json:"exchange,omitempty"`
	MarketPriceUSD    float64 `json:"marketPriceUsd"`
	SellerID          int64   `json:"sellerId"`
	SellerName        string  `json:"sellerName,omitempty"`
	AvailableQuantity int32   `json:"availableQuantity"`
}

func (h *OTCHandler) handleMarketplace(w http.ResponseWriter, r *http.Request, callerID int64) {
	items, err := h.svc.ListMarketplace(r.Context(), callerID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "greška pri dohvatu marketplace-a")
		return
	}
	out := make([]marketplaceItemDTO, 0, len(items))
	for _, it := range items {
		out = append(out, marketplaceItemDTO{
			ListingID:         it.ListingID,
			Ticker:            it.Ticker,
			StockName:         it.StockName,
			Exchange:          it.Exchange,
			MarketPriceUSD:    it.MarketPriceUSD,
			SellerID:          it.SellerID,
			SellerName:        it.SellerName,
			AvailableQuantity: it.AvailableQuantity,
		})
	}
	writeOTCJSON(w, http.StatusOK, out)
}

func (h *OTCHandler) handleList(w http.ResponseWriter, r *http.Request, callerID int64) {
	q := r.URL.Query()
	filter := domain.ListOTCOffersFilter{UserID: callerID}
	if s := q.Get("status"); s != "" {
		v := domain.OTCOfferStatus(strings.ToUpper(s))
		filter.Status = &v
	}
	if role := strings.ToUpper(q.Get("role")); role == "BUYER" || role == "SELLER" {
		filter.Role = role
	}
	if q.Get("onlyMyTurn") == "true" {
		filter.OnlyMyTurn = true
	}

	items, err := h.svc.ListOffers(r.Context(), filter)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "greška pri dohvatu ponuda")
		return
	}
	out := make([]offerDTO, 0, len(items))
	for _, it := range items {
		out = append(out, listItemDTO(it, callerID))
	}
	writeOTCJSON(w, http.StatusOK, out)
}

// ─── Error mapping ────────────────────────────────────────────────────────────

func (h *OTCHandler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrOTCOfferNotFound):
		writeJSONError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, domain.ErrOTCInvalidStatus),
		errors.Is(err, domain.ErrOTCInsufficientCapacity),
		errors.Is(err, domain.ErrOTCInsufficientFunds),
		errors.Is(err, domain.ErrOTCNotInPublicRegime),
		errors.Is(err, domain.ErrOTCSellerAccountMissing):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, domain.ErrOTCNotParticipant),
		errors.Is(err, domain.ErrOTCNotCounterparty),
		errors.Is(err, domain.ErrOTCSelfAccept),
		errors.Is(err, domain.ErrOTCSelfTrade),
		errors.Is(err, domain.ErrOTCAccountNotOwned):
		writeJSONError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, domain.ErrOTCInvalidInput),
		errors.Is(err, domain.ErrOTCAccountCurrency),
		errors.Is(err, domain.ErrOTCListingNotFound):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	default:
		writeJSONError(w, http.StatusInternalServerError, "interna greška")
	}
}

// writeOTCJSON — alias da izbegnemo koliziju imena sa drugim handlerima.
func writeOTCJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

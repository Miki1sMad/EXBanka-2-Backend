package handler

// investment_fund_handler.go — HTTP handleri za investicione fondove (Discovery & Details).
//
// Endpointi:
//   GET  /bank/investment-funds                  — Discovery: lista svih fondova sa filterima i sortiranjem
//   GET  /bank/investment-funds/{id}             — Detaljan prikaz fonda sa listom hartija
//   POST /bank/investment-funds                  — Kreiranje novog fonda (samo supervizori)
//   POST /bank/investment-funds/orders           — Kreiranje naloga za kupovinu hartija za fond (samo supervizori)

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"banka-backend/services/bank-service/internal/domain"
	"banka-backend/services/bank-service/internal/trading"
	"banka-backend/services/bank-service/internal/transport"
	auth "banka-backend/shared/auth"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc/metadata"
)

// InvestmentFundHandler opslužuje sve /bank/investment-funds/* rute.
type InvestmentFundHandler struct {
	service         domain.InvestmentFundService
	fundRepo        domain.InvestmentFundRepository
	tradingService  trading.TradingService
	listingService  domain.ListingService
	exchangeService domain.ExchangeService
	userClient      *transport.UserServiceClient
	jwtSecret       string
}

// NewInvestmentFundHandler kreira handler sa svim zavisnostima.
func NewInvestmentFundHandler(
	service domain.InvestmentFundService,
	fundRepo domain.InvestmentFundRepository,
	tradingService trading.TradingService,
	listingService domain.ListingService,
	exchangeService domain.ExchangeService,
	userClient *transport.UserServiceClient,
	jwtSecret string,
) *InvestmentFundHandler {
	return &InvestmentFundHandler{
		service:         service,
		fundRepo:        fundRepo,
		tradingService:  tradingService,
		listingService:  listingService,
		exchangeService: exchangeService,
		userClient:      userClient,
		jwtSecret:       jwtSecret,
	}
}

// resolveManagerName vraća "Ime Prezime" za zadati employee ID; ako lookup pukne, vraća "".
// Prosleđuje Authorization header iz HTTP zahteva kao gRPC metadata da bi user-service
// videlo isti JWT kao i bank-service (auth interceptor user-service-a).
func (h *InvestmentFundHandler) resolveManagerName(r *http.Request, managerID int64) string {
	if h.userClient == nil || managerID == 0 {
		return ""
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if authHdr := r.Header.Get("Authorization"); authHdr != "" {
		ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", authHdr))
	}
	info, err := h.userClient.GetEmployeeInfo(ctx, managerID)
	if err != nil || info == nil {
		return ""
	}
	return strings.TrimSpace(info.FirstName + " " + info.LastName)
}

// BankAccountsHandler je tanki HTTP handler — vraća RSD račune banke (vlasnik_id=2)
// koji nisu fond-namenski. Koriste ih supervizori pri "investiranju u ime banke" i pri
// kupovini hartija u ime banke.
//
// Mapped to: GET /bank/bank-accounts (samo SUPERVISOR / ADMIN).
func (h *InvestmentFundHandler) BankAccountsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

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
	if !isSupervisorPermission(claims) {
		writeJSONError(w, http.StatusForbidden, "samo supervizori i admini mogu listati račune banke")
		return
	}

	items, err := h.fundRepo.ListBankRSDAccounts(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "greška pri dohvatu računa banke")
		return
	}

	type bankAccountResp struct {
		ID                  string  `json:"id"`
		BrojRacuna          string  `json:"brojRacuna"`
		NazivRacuna         string  `json:"nazivRacuna"`
		ValutaOznaka        string  `json:"valutaOznaka"`
		StanjeRacuna        float64 `json:"stanjeRacuna"`
		RezervisanaSredstva float64 `json:"rezervisanaSredstva"`
		RaspolozivoStanje   float64 `json:"raspolozivoStanje"`
	}
	resp := make([]bankAccountResp, 0, len(items))
	for _, it := range items {
		resp = append(resp, bankAccountResp{
			ID:                  strconv.FormatInt(it.ID, 10),
			BrojRacuna:          it.BrojRacuna,
			NazivRacuna:         it.NazivRacuna,
			ValutaOznaka:        "RSD",
			StanjeRacuna:        it.StanjeRacuna,
			RezervisanaSredstva: it.RezervovanaSredstva,
			RaspolozivoStanje:   it.RaspolozivoStanje,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": resp})
}

// resolveAccountNumber vraća broj_racuna za fund.AccountID; ako lookup pukne, vraća "".
func (h *InvestmentFundHandler) resolveAccountNumber(r *http.Request, accountID int64) string {
	if h.fundRepo == nil || accountID == 0 {
		return ""
	}
	num, err := h.fundRepo.GetAccountNumber(r.Context(), accountID)
	if err != nil {
		return ""
	}
	return num
}

// ServeHTTP dispečuje zahteve na odgovarajuće sub-handlere.
func (h *InvestmentFundHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

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

	path := strings.TrimSuffix(r.URL.Path, "/")

	switch {
	case path == "/bank/investment-funds" && r.Method == http.MethodGet:
		h.discoveryList(w, r, claims)
	case path == "/bank/investment-funds" && r.Method == http.MethodPost:
		h.createFund(w, r, claims)
	case path == "/bank/investment-funds/orders" && r.Method == http.MethodPost:
		h.createFundOrder(w, r, claims)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/bank/investment-funds/"):
		h.fundDetails(w, r, claims, extractInvestmentFundID(path))
	default:
		writeJSONError(w, http.StatusNotFound, "not found")
	}
}

// extractInvestmentFundID parsira ID fonda iz putanje /bank/investment-funds/{id}.
func extractInvestmentFundID(path string) int64 {
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		return 0
	}
	id, _ := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	return id
}

// ─── GET /bank/investment-funds (Discovery) ───────────────────────────────────

// fundDiscoveryItem je JSON reprezentacija fonda u listi.
type fundDiscoveryItem struct {
	ID                  string  `json:"id"`
	Name                string  `json:"name"`
	Description         string  `json:"description"`
	FundValueRSD        float64 `json:"fundValueRsd"`
	Profit              float64 `json:"profit"`
	LiquidAssets        float64 `json:"liquidAssets"`
	MinimumContribution float64 `json:"minimumContribution"`
	ManagerID           string  `json:"managerId"`
	CreatedAt           string  `json:"createdAt"`
}

type fundDiscoveryResponse struct {
	Funds []fundDiscoveryItem `json:"funds"`
}

// discoveryList vraća sve fondove sa opcionalnim filtriranjem i sortiranjem.
// Query parametri:
//
//	search    — pretraga po nazivu/opisu
//	sortBy    — name | description | fundValue | profit | minimumContribution
//	sortOrder — ASC | DESC
func (h *InvestmentFundHandler) discoveryList(w http.ResponseWriter, r *http.Request, _ *auth.AccessClaims) {
	q := r.URL.Query()
	filter := domain.FundFilter{
		Search:    q.Get("search"),
		SortBy:    q.Get("sortBy"),
		SortOrder: q.Get("sortOrder"),
	}

	items, err := h.service.ListFunds(r.Context(), filter)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "greška pri dohvatu fondova")
		return
	}

	resp := make([]fundDiscoveryItem, 0, len(items))
	for _, item := range items {
		resp = append(resp, fundDiscoveryItem{
			ID:                  strconv.FormatInt(item.ID, 10),
			Name:                item.Name,
			Description:         item.Description,
			FundValueRSD:        item.FundValueRSD,
			Profit:              item.Profit,
			LiquidAssets:        item.LiquidAssets,
			MinimumContribution: item.MinimumContribution,
			ManagerID:           strconv.FormatInt(item.ManagerID, 10),
			CreatedAt:           item.CreatedAt.Format(time.RFC3339),
		})
	}

	writeJSON(w, http.StatusOK, fundDiscoveryResponse{Funds: resp})
}

// ─── GET /bank/investment-funds/{id} (Details) ────────────────────────────────

// fundDetailsResponse je JSON reprezentacija detalja fonda.
type fundDetailsResponse struct {
	ID                  string             `json:"id"`
	Name                string             `json:"name"`
	Description         string             `json:"description"`
	FundValueRSD        float64            `json:"fundValueRsd"`
	Profit              float64            `json:"profit"`
	MinimumContribution float64            `json:"minimumContribution"`
	ManagerID           string             `json:"managerId"`
	ManagerName         string             `json:"managerName"`
	LiquidAssets        float64            `json:"liquidAssets"`
	AccountID           string             `json:"accountId"`
	AccountNumber       string             `json:"accountNumber"`
	CreatedAt           string             `json:"createdAt"`
	Securities          []securityItemResp `json:"securities"`
}

// securityItemResp je JSON reprezentacija jedne hartije u posedu fonda.
// Polja su po specifikaciji: Ticker, Price, Change, Volume, initialMarginCost, acquisitionDate.
type securityItemResp struct {
	ListingID         string  `json:"listingId"`
	Ticker            string  `json:"ticker"`
	PriceUSD          float64 `json:"priceUsd"`
	PriceRSD          float64 `json:"priceRsd"`
	Change            float64 `json:"change"`
	Volume            int64   `json:"volume"`
	InitialMarginCost float64 `json:"initialMarginCost"`
	Quantity          float64 `json:"quantity"`
	AcquisitionDate   string  `json:"acquisitionDate"`
}

// fundDetails vraća kompletan prikaz fonda sa svim hartijama i izvedenim vrednostima.
func (h *InvestmentFundHandler) fundDetails(w http.ResponseWriter, r *http.Request, _ *auth.AccessClaims, fundID int64) {
	if fundID <= 0 {
		writeJSONError(w, http.StatusBadRequest, "neispravan ID fonda")
		return
	}

	details, err := h.service.GetFundDetails(r.Context(), fundID)
	if err != nil {
		if err == domain.ErrFundNotFound {
			writeJSONError(w, http.StatusNotFound, "fond nije pronađen")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "greška pri dohvatu detalja fonda")
		return
	}

	secs := make([]securityItemResp, 0, len(details.Securities))
	for _, sec := range details.Securities {
		secs = append(secs, securityItemResp{
			ListingID:         strconv.FormatInt(sec.ListingID, 10),
			Ticker:            sec.Ticker,
			PriceUSD:          sec.CurrentPriceUSD,
			PriceRSD:          sec.CurrentPriceRSD,
			Change:            sec.ChangePercent,
			Volume:            sec.Volume,
			InitialMarginCost: sec.InitialMarginCost,
			Quantity:          sec.Quantity,
			AcquisitionDate:   sec.AcquisitionDate.Format(time.RFC3339),
		})
	}

	writeJSON(w, http.StatusOK, fundDetailsResponse{
		ID:                  strconv.FormatInt(details.ID, 10),
		Name:                details.Name,
		Description:         details.Description,
		FundValueRSD:        details.FundValueRSD,
		Profit:              details.Profit,
		MinimumContribution: details.MinimumContribution,
		ManagerID:           strconv.FormatInt(details.ManagerID, 10),
		ManagerName:         h.resolveManagerName(r, details.ManagerID),
		LiquidAssets:        details.LiquidAssets,
		AccountID:           strconv.FormatInt(details.AccountID, 10),
		AccountNumber:       h.resolveAccountNumber(r, details.AccountID),
		CreatedAt:           details.CreatedAt.Format(time.RFC3339),
		Securities:          secs,
	})
}

// ─── POST /bank/investment-funds (Create) ─────────────────────────────────────

type createInvestmentFundRequest struct {
	Name                string  `json:"name"`
	Description         string  `json:"description"`
	MinimumContribution float64 `json:"minimumContribution"`
}

type createInvestmentFundResponse struct {
	ID                  string  `json:"id"`
	Name                string  `json:"name"`
	Description         string  `json:"description"`
	MinimumContribution float64 `json:"minimumContribution"`
	ManagerID           string  `json:"managerId"`
	ManagerName         string  `json:"managerName"`
	AccountID           string  `json:"accountId"`
	AccountNumber       string  `json:"accountNumber"`
	CreatedAt           string  `json:"createdAt"`
}

// createFund kreira novi fond. Dostupno samo supervizorima.
// Supervizor koji kreira fond automatski postaje prvi menadžer.
func (h *InvestmentFundHandler) createFund(w http.ResponseWriter, r *http.Request, claims *auth.AccessClaims) {
	if !isSupervisorPermission(claims) {
		writeJSONError(w, http.StatusForbidden, "samo supervizori mogu kreirati fondove")
		return
	}

	managerID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "neispravan korisnički ID u tokenu")
		return
	}

	var req createInvestmentFundRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "neispravan JSON zahtev")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeJSONError(w, http.StatusBadRequest, "naziv fonda je obavezan")
		return
	}
	if req.MinimumContribution <= 0 {
		writeJSONError(w, http.StatusBadRequest, "minimalni ulog mora biti veći od nule")
		return
	}

	fund, err := h.service.CreateFund(r.Context(), domain.CreateFundInput{
		Name:                strings.TrimSpace(req.Name),
		Description:         strings.TrimSpace(req.Description),
		MinimumContribution: req.MinimumContribution,
		ManagerID:           managerID,
	})
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "greška pri kreiranju fonda: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, createInvestmentFundResponse{
		ID:                  strconv.FormatInt(fund.ID, 10),
		Name:                fund.Name,
		Description:         fund.Description,
		MinimumContribution: fund.MinimumContribution,
		ManagerID:           strconv.FormatInt(fund.ManagerID, 10),
		ManagerName:         h.resolveManagerName(r, fund.ManagerID),
		AccountID:           strconv.FormatInt(fund.AccountID, 10),
		AccountNumber:       h.resolveAccountNumber(r, fund.AccountID),
		CreatedAt:           fund.CreatedAt.Format(time.RFC3339),
	})
}

// ─── POST /bank/investment-funds/orders (Fund Order) ─────────────────────────

type createFundOrderRequest struct {
	FundID       int64   `json:"fund_id"`
	ListingID    int64   `json:"listing_id"`
	OrderType    string  `json:"order_type"`    // MARKET | LIMIT | STOP | STOP_LIMIT
	Quantity     int32   `json:"quantity"`
	ContractSize int32   `json:"contract_size"`
	PricePerUnit *string `json:"price_per_unit"` // za LIMIT/STOP_LIMIT; nil inače
	StopPrice    *string `json:"stop_price"`     // za STOP/STOP_LIMIT; nil inače
	AfterHours   bool    `json:"after_hours"`
	AllOrNone    bool    `json:"all_or_none"`
}

type createFundOrderResponse struct {
	OrderID   string `json:"order_id"`
	FundID    string `json:"fund_id"`
	Status    string `json:"status"`
	Direction string `json:"direction"`
	Quantity  int32  `json:"quantity"`
	Message   string `json:"message"`
}

// createFundOrder handles POST /bank/investment-funds/orders.
// Supervisor specifies which fund to buy securities for; the handler validates
// that the fund has sufficient liquid_assets, then creates a trading order whose
// AccountID is the fund's RSD account. The trading engine links each fill to the
// fund via Order.FundID.
func (h *InvestmentFundHandler) createFundOrder(w http.ResponseWriter, r *http.Request, claims *auth.AccessClaims) {
	if !isSupervisorPermission(claims) {
		writeJSONError(w, http.StatusForbidden, "samo supervizori mogu kreirati naloge za fond")
		return
	}

	supervisorID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "neispravan korisnički ID u tokenu")
		return
	}

	var req createFundOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "neispravan JSON zahtev")
		return
	}
	if req.FundID <= 0 {
		writeJSONError(w, http.StatusBadRequest, "fund_id je obavezan")
		return
	}
	if req.ListingID <= 0 {
		writeJSONError(w, http.StatusBadRequest, "listing_id je obavezan")
		return
	}
	if req.Quantity <= 0 {
		writeJSONError(w, http.StatusBadRequest, "quantity mora biti veći od nule")
		return
	}
	if req.ContractSize <= 0 {
		req.ContractSize = 1
	}

	// Look up the investment fund.
	fund, err := h.service.GetFundByID(r.Context(), req.FundID)
	if err != nil {
		if err == domain.ErrFundNotFound {
			writeJSONError(w, http.StatusNotFound, "fond nije pronađen")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "greška pri dohvatu fonda")
		return
	}
	if fund.AccountID == 0 {
		writeJSONError(w, http.StatusInternalServerError, "fond nema dinarski račun")
		return
	}

	// Fetch listing to determine execution price and listing type.
	listing, err := h.listingService.GetListingByID(r.Context(), req.ListingID)
	if err != nil {
		if err == domain.ErrListingNotFound {
			writeJSONError(w, http.StatusNotFound, "hartija od vrednosti nije pronađena")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "greška pri dohvatu hartije")
		return
	}

	// Determine the execution price used for the liquidity pre-check.
	var execPriceUSD float64
	switch strings.ToUpper(req.OrderType) {
	case "MARKET":
		execPriceUSD = listing.Ask
		if execPriceUSD <= 0 {
			execPriceUSD = listing.Price
		}
	case "LIMIT", "STOP_LIMIT":
		if req.PricePerUnit == nil {
			writeJSONError(w, http.StatusBadRequest, "price_per_unit je obavezan za LIMIT/STOP_LIMIT nalog")
			return
		}
		ppu, parseErr := strconv.ParseFloat(*req.PricePerUnit, 64)
		if parseErr != nil || ppu <= 0 {
			writeJSONError(w, http.StatusBadRequest, "neispravan price_per_unit")
			return
		}
		execPriceUSD = ppu
	case "STOP":
		if req.StopPrice == nil {
			writeJSONError(w, http.StatusBadRequest, "stop_price je obavezan za STOP nalog")
			return
		}
		sp, parseErr := strconv.ParseFloat(*req.StopPrice, 64)
		if parseErr != nil || sp <= 0 {
			writeJSONError(w, http.StatusBadRequest, "neispravan stop_price")
			return
		}
		execPriceUSD = sp
	default:
		writeJSONError(w, http.StatusBadRequest, "nepoznat order_type; koristite MARKET, LIMIT, STOP ili STOP_LIMIT")
		return
	}

	estimatedUSD := execPriceUSD * float64(req.Quantity) * float64(req.ContractSize)

	// Convert estimated cost to RSD for fund liquidity check.
	estimatedRSD, convErr := estimateListingChargeInAccountCurrency(r.Context(), h.exchangeService, estimatedUSD, "RSD")
	if convErr != nil {
		log.Printf("[fund-order] USD→RSD konverzija: %v", convErr)
		writeJSONError(w, http.StatusFailedDependency, "nije moguće proceniti iznos u RSD: "+convErr.Error())
		return
	}

	if fund.LiquidAssets+1e-4 < estimatedRSD {
		writeJSONError(w, http.StatusUnprocessableEntity,
			"fond nema dovoljno likvidnih sredstava (potrebno ~"+strconv.FormatFloat(estimatedRSD, 'f', 2, 64)+" RSD, raspoloživo "+strconv.FormatFloat(fund.LiquidAssets, 'f', 2, 64)+" RSD)")
		return
	}

	// Build the trading order request. The fund's RSD account is used as AccountID
	// so that FundsManager.SettleBuyFill debits the fund account directly.
	domainReq := &trading.CreateOrderRequest{
		UserID:       supervisorID,
		AccountID:    fund.AccountID,
		ListingID:    req.ListingID,
		OrderType:    trading.OrderType(strings.ToUpper(req.OrderType)),
		Direction:    trading.OrderDirectionBuy, // fund orders are always BUY
		Quantity:     req.Quantity,
		ContractSize: req.ContractSize,
		AfterHours:   req.AfterHours,
		AllOrNone:    req.AllOrNone,
		IsSupervisor: true,
		IsClient:     false,
		IsForex:      listing.ListingType == domain.ListingTypeForex,
		FundID:       &req.FundID,
	}

	if req.PricePerUnit != nil {
		d, parseErr := decimal.NewFromString(*req.PricePerUnit)
		if parseErr == nil {
			domainReq.PricePerUnit = &d
		}
	}
	if req.StopPrice != nil {
		d, parseErr := decimal.NewFromString(*req.StopPrice)
		if parseErr == nil {
			domainReq.StopPrice = &d
		}
	}

	order, err := h.tradingService.CreateOrder(r.Context(), domainReq)
	if err != nil {
		log.Printf("[fund-order] kreiranje naloga za fond %d: %v", req.FundID, err)
		writeJSONError(w, http.StatusInternalServerError, "greška pri kreiranju naloga: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, createFundOrderResponse{
		OrderID:   strconv.FormatInt(order.ID, 10),
		FundID:    strconv.FormatInt(req.FundID, 10),
		Status:    string(order.Status),
		Direction: string(order.Direction),
		Quantity:  order.Quantity,
		Message:   "Nalog za kupovinu hartija za fond kreiran (procena ~" + strconv.FormatFloat(estimatedRSD, 'f', 2, 64) + " RSD).",
	})
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// isSupervisorPermission vraća true ako korisnik ima SUPERVISOR permisiju ili je ADMIN.
func isSupervisorPermission(claims *auth.AccessClaims) bool {
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

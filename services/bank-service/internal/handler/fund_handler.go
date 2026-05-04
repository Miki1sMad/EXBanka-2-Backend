package handler

// fund_handler.go — HTTP handlers for investment funds (Moji fondovi).
//
// Endpoints:
//   GET  /bank/funds                       — list funds (clients: own positions; supervisors: managed funds)
//   POST /bank/funds/{id}/invest           — invest in a fund
//   POST /bank/funds/{id}/withdraw         — withdraw from a fund

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"banka-backend/services/bank-service/internal/domain"
	auth "banka-backend/shared/auth"

	"gorm.io/gorm"
)

// parseSellPath extracts fundID and ticker from /bank/funds/{id}/sell/{ticker}.
func parseSellPath(path string) (int64, string) {
	// Strip prefix "/bank/funds/" → "{id}/sell/{ticker}"
	rest := strings.TrimPrefix(path, "/bank/funds/")
	parts := strings.SplitN(rest, "/", 3) // ["3", "sell", "AAPL"]
	if len(parts) < 3 || parts[1] != "sell" {
		return 0, ""
	}
	id, _ := strconv.ParseInt(parts[0], 10, 64)
	return id, parts[2]
}

// fundWithdrawalCommissionRate is the redemption fee charged to clients on withdrawal.
// The commission stays in the fund as profit; supervisors are exempt.
const fundWithdrawalCommissionRate = 0.01

// ─── GORM models ──────────────────────────────────────────────────────────────

type investmentFundModel struct {
	ID                  int64     `gorm:"column:id;primaryKey"`
	Name                string    `gorm:"column:name"`
	Description         string    `gorm:"column:description"`
	MinimumContribution float64   `gorm:"column:minimum_contribution"`
	LiquidAssets        float64   `gorm:"column:liquid_assets"`
	AccountID           *int64    `gorm:"column:account_id"`
	ManagerID           int64     `gorm:"column:manager_id"`
	CreatedAt           time.Time `gorm:"column:created_at"`
}

func (investmentFundModel) TableName() string { return "core_banking.investment_funds" }

type fundPositionModel struct {
	ID          int64     `gorm:"column:id;primaryKey"`
	FundID      int64     `gorm:"column:fund_id"`
	UserID      int64     `gorm:"column:user_id"`
	AccountID   int64     `gorm:"column:account_id"`
	InvestedRSD float64   `gorm:"column:invested_rsd"`
	CreatedAt   time.Time `gorm:"column:created_at"`
}

func (fundPositionModel) TableName() string { return "core_banking.fund_positions" }

type clientFundTransactionModel struct {
	ID        int64     `gorm:"column:id;primaryKey"`
	FundID    int64     `gorm:"column:fund_id"`
	UserID    int64     `gorm:"column:user_id"`
	AmountRSD float64   `gorm:"column:amount_rsd"`
	Status    string    `gorm:"column:status"`
	CreatedAt time.Time `gorm:"column:created_at"`
	IsInflow  bool      `gorm:"column:is_inflow"`
}

func (clientFundTransactionModel) TableName() string {
	return "core_banking.client_fund_transactions"
}

// ─── FundHandler ──────────────────────────────────────────────────────────────

// FundHandler serves all /bank/funds/* endpoints.
type FundHandler struct {
	db              *gorm.DB
	exchangeService domain.ExchangeService
	fundService     domain.InvestmentFundService
	jwtSecret       string
}

// NewFundHandler constructs the handler.
func NewFundHandler(
	db *gorm.DB,
	exchangeService domain.ExchangeService,
	fundService domain.InvestmentFundService,
	jwtSecret string,
) *FundHandler {
	return &FundHandler{
		db:              db,
		exchangeService: exchangeService,
		fundService:     fundService,
		jwtSecret:       jwtSecret,
	}
}

// ServeHTTP dispatches /bank/funds/* requests.
func (h *FundHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

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

	path := r.URL.Path // e.g. /bank/funds or /bank/funds/3/invest

	switch {
	case path == "/bank/funds" && r.Method == http.MethodGet:
		h.listFunds(w, r, claims)
	case strings.HasSuffix(path, "/invest") && r.Method == http.MethodPost:
		h.invest(w, r, claims, extractFundID(path, "/invest"))
	case strings.HasSuffix(path, "/withdraw") && r.Method == http.MethodPost:
		h.withdraw(w, r, claims, extractFundID(path, "/withdraw"))
	case strings.Contains(path, "/sell/") && r.Method == http.MethodPost:
		fundID, ticker := parseSellPath(path)
		h.sellSecurity(w, r, claims, fundID, ticker)
	case strings.HasSuffix(path, "/performance") && r.Method == http.MethodGet:
		h.fundPerformance(w, r, extractFundID(path, "/performance"))
	default:
		writeJSONError(w, http.StatusNotFound, "not found")
	}
}

// extractFundID parses the fund ID from a path like /bank/funds/3/invest.
func extractFundID(path, suffix string) int64 {
	trimmed := strings.TrimSuffix(path, suffix) // /bank/funds/3
	parts := strings.Split(trimmed, "/")        // ["", "bank", "funds", "3"]
	if len(parts) == 0 {
		return 0
	}
	id, _ := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	return id
}

// ─── GET /bank/funds ──────────────────────────────────────────────────────────

type fundForClient struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	FundValueRSD float64 `json:"fundValueRsd"`
	SharePercent float64 `json:"sharePercent"`
	ShareRSD     float64 `json:"shareRsd"`
	Profit       float64 `json:"profit"`
	InvestedRSD  float64 `json:"investedRsd"`
}

type fundForManager struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	FundValueRSD float64 `json:"fundValueRsd"`
	LiquidityRSD float64 `json:"liquidityRsd"`
}

type fundsResponse struct {
	ClientFunds  []fundForClient  `json:"clientFunds,omitempty"`
	ManagedFunds []fundForManager `json:"managedFunds,omitempty"`
}

func (h *FundHandler) listFunds(w http.ResponseWriter, r *http.Request, claims *auth.AccessClaims) {
	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "invalid user id")
		return
	}

	ctx := r.Context()
	isSupervisor := h.isSupervisor(claims)

	if isSupervisor {
		var funds []investmentFundModel
		h.db.WithContext(ctx).Where("manager_id = ?", userID).Find(&funds)

		managedFunds := make([]fundForManager, 0, len(funds))
		for _, f := range funds {
			// Use GetFundDetails for accurate FundValueRSD (liquid_assets + securities market value).
			fundValueRSD := f.LiquidAssets
			liquidityRSD := f.LiquidAssets
			if details, err := h.fundService.GetFundDetails(ctx, f.ID); err == nil && details != nil {
				fundValueRSD = details.FundValueRSD
				liquidityRSD = details.LiquidAssets
			}
			managedFunds = append(managedFunds, fundForManager{
				ID:           strconv.FormatInt(f.ID, 10),
				Name:         f.Name,
				Description:  f.Description,
				FundValueRSD: fundValueRSD,
				LiquidityRSD: liquidityRSD,
			})
		}
		writeJSON(w, http.StatusOK, fundsResponse{ManagedFunds: managedFunds})
		return
	}

	// Client / Actuary Agent: show their positions
	var positions []fundPositionModel
	h.db.WithContext(ctx).Where("user_id = ?", userID).Find(&positions)

	if len(positions) == 0 {
		writeJSON(w, http.StatusOK, fundsResponse{ClientFunds: []fundForClient{}})
		return
	}

	clientFunds := make([]fundForClient, 0, len(positions))
	for _, pos := range positions {
		var fund investmentFundModel
		if err := h.db.WithContext(ctx).First(&fund, pos.FundID).Error; err != nil {
			continue
		}

		// Total capital invested by all clients in this fund (denominator for share %).
		var totalFundInvested float64
		h.db.WithContext(ctx).Raw(`
			SELECT COALESCE(SUM(invested_rsd), 0) FROM core_banking.fund_positions WHERE fund_id = ?
		`, pos.FundID).Scan(&totalFundInvested)

		// Actual current fund value: liquid_assets + securities at market price.
		fundValueRSD := totalFundInvested // fallback when service is unavailable
		if details, err := h.fundService.GetFundDetails(ctx, pos.FundID); err == nil && details != nil {
			fundValueRSD = details.FundValueRSD
		}

		// Client's proportional share of the fund's current value.
		sharePercent := 0.0
		shareRSD := pos.InvestedRSD // fallback: at least return cost basis
		if totalFundInvested > 0 {
			sharePercent = (pos.InvestedRSD / totalFundInvested) * 100
			shareRSD = (pos.InvestedRSD / totalFundInvested) * fundValueRSD
		}
		profit := shareRSD - pos.InvestedRSD

		clientFunds = append(clientFunds, fundForClient{
			ID:           strconv.FormatInt(fund.ID, 10),
			Name:         fund.Name,
			Description:  fund.Description,
			FundValueRSD: fundValueRSD,
			SharePercent: sharePercent,
			ShareRSD:     shareRSD,
			Profit:       profit,
			InvestedRSD:  pos.InvestedRSD,
		})
	}
	writeJSON(w, http.StatusOK, fundsResponse{ClientFunds: clientFunds})
}

// ─── POST /bank/funds/{id}/invest ────────────────────────────────────────────

type investRequest struct {
	AccountID string  `json:"accountId"`
	Amount    float64 `json:"amount"` // in account currency
}

func (h *FundHandler) invest(w http.ResponseWriter, r *http.Request, claims *auth.AccessClaims, fundID int64) {
	if fundID <= 0 {
		writeJSONError(w, http.StatusBadRequest, "invalid fund id")
		return
	}

	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "invalid user id")
		return
	}

	var req investRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Amount <= 0 {
		writeJSONError(w, http.StatusBadRequest, "amount must be greater than 0")
		return
	}
	accountID, err := strconv.ParseInt(req.AccountID, 10, 64)
	if err != nil || accountID <= 0 {
		writeJSONError(w, http.StatusBadRequest, "invalid accountId")
		return
	}

	ctx := r.Context()

	// Verify fund exists and fetch minimum_contribution.
	var fund investmentFundModel
	if err := h.db.WithContext(ctx).First(&fund, fundID).Error; err != nil {
		writeJSONError(w, http.StatusNotFound, "fond nije pronađen")
		return
	}

	// Get account currency for conversion.
	var accCurrency string
	h.db.WithContext(ctx).Raw(`
		SELECT v.oznaka FROM core_banking.racun r
		JOIN core_banking.valuta v ON v.id = r.id_valute WHERE r.id = ?
	`, accountID).Scan(&accCurrency)

	// Convert to RSD.
	amountRSD := req.Amount
	if accCurrency != "" && accCurrency != "RSD" {
		rates, err := h.exchangeService.GetRates(ctx)
		if err == nil {
			for _, rt := range rates {
				if rt.Oznaka == accCurrency && rt.Srednji > 0 {
					amountRSD = req.Amount * rt.Srednji
					break
				}
			}
		}
	}

	// 2.3: Minimum contribution validation.
	if fund.MinimumContribution > 0 && amountRSD < fund.MinimumContribution {
		writeJSONError(w, http.StatusBadRequest,
			fmt.Sprintf("minimalni ulog je %.2f RSD", fund.MinimumContribution))
		return
	}

	// Check sufficient funds.
	var available float64
	h.db.WithContext(ctx).Raw(`
		SELECT stanje_racuna - rezervisana_sredstva FROM core_banking.racun WHERE id = ?
	`, accountID).Scan(&available)
	if available < req.Amount {
		writeJSONError(w, http.StatusBadRequest, "nedovoljno sredstava na računu")
		return
	}

	// Resolve fund's RSD account.
	var fundAccountID int64
	if fund.AccountID != nil {
		fundAccountID = *fund.AccountID
	}

	txErr := h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. Debit client account (in their currency).
		if err := tx.Exec(`
			UPDATE core_banking.racun SET stanje_racuna = stanje_racuna - ? WHERE id = ?
		`, req.Amount, accountID).Error; err != nil {
			return err
		}

		// 2. Credit fund's RSD account.
		if fundAccountID > 0 {
			if err := tx.Exec(`
				UPDATE core_banking.racun SET stanje_racuna = stanje_racuna + ? WHERE id = ?
			`, amountRSD, fundAccountID).Error; err != nil {
				return err
			}
		}

		// 3. Increase fund liquid_assets.
		if err := tx.Exec(`
			UPDATE core_banking.investment_funds SET liquid_assets = liquid_assets + ? WHERE id = ?
		`, amountRSD, fundID).Error; err != nil {
			return err
		}

		// 4. Upsert client position.
		if err := tx.Exec(`
			INSERT INTO core_banking.fund_positions (fund_id, user_id, account_id, invested_rsd)
			VALUES (?, ?, ?, ?)
			ON CONFLICT (fund_id, user_id)
			DO UPDATE SET invested_rsd = fund_positions.invested_rsd + EXCLUDED.invested_rsd
		`, fundID, userID, accountID, amountRSD).Error; err != nil {
			return err
		}

		// 5. Persist transaction record (2.1).
		return tx.Create(&clientFundTransactionModel{
			FundID:    fundID,
			UserID:    userID,
			AmountRSD: amountRSD,
			Status:    "completed",
			CreatedAt: time.Now().UTC(),
			IsInflow:  true,
		}).Error
	})
	if txErr != nil {
		writeJSONError(w, http.StatusInternalServerError, "greška pri uplati u fond: "+txErr.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":   "Investicija je uspešno obavljena.",
		"amountRsd": amountRSD,
		"fundId":    strconv.FormatInt(fundID, 10),
	})
}

// ─── POST /bank/funds/{id}/withdraw ──────────────────────────────────────────

type withdrawRequest struct {
	AccountID   string  `json:"accountId"`
	AmountRSD   float64 `json:"amountRsd"` // amount to withdraw in RSD (0 = full withdrawal)
	WithdrawAll bool    `json:"withdrawAll"`
}

func (h *FundHandler) withdraw(w http.ResponseWriter, r *http.Request, claims *auth.AccessClaims, fundID int64) {
	if fundID <= 0 {
		writeJSONError(w, http.StatusBadRequest, "invalid fund id")
		return
	}

	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "invalid user id")
		return
	}

	var req withdrawRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	accountID, err := strconv.ParseInt(req.AccountID, 10, 64)
	if err != nil || accountID <= 0 {
		writeJSONError(w, http.StatusBadRequest, "invalid accountId")
		return
	}

	ctx := r.Context()

	// Load client's position.
	var pos fundPositionModel
	if err := h.db.WithContext(ctx).
		Where("fund_id = ? AND user_id = ?", fundID, userID).
		First(&pos).Error; err != nil {
		writeJSONError(w, http.StatusNotFound, "nemate poziciju u ovom fondu")
		return
	}

	withdrawRSD := req.AmountRSD
	if req.WithdrawAll || withdrawRSD <= 0 {
		withdrawRSD = pos.InvestedRSD
	}
	if withdrawRSD > pos.InvestedRSD {
		writeJSONError(w, http.StatusBadRequest, "iznos povlačenja veći od investiranog iznosa")
		return
	}

	// 2.6: Commission for clients; supervisors/admins are exempt.
	isSupervisor := h.isSupervisor(claims)
	commissionRate := 0.0
	if !isSupervisor {
		commissionRate = fundWithdrawalCommissionRate
	}
	commissionRSD := withdrawRSD * commissionRate
	netWithdrawRSD := withdrawRSD - commissionRSD // amount the client actually receives

	// Get account currency for crediting.
	var accCurrency string
	h.db.WithContext(ctx).Raw(`
		SELECT v.oznaka FROM core_banking.racun r
		JOIN core_banking.valuta v ON v.id = r.id_valute WHERE r.id = ?
	`, accountID).Scan(&accCurrency)

	// Convert net amount to account currency.
	creditAmount := netWithdrawRSD
	if accCurrency != "" && accCurrency != "RSD" {
		rates, err := h.exchangeService.GetRates(ctx)
		if err == nil {
			for _, rt := range rates {
				if rt.Oznaka == accCurrency && rt.Srednji > 0 {
					creditAmount = netWithdrawRSD / rt.Srednji
					break
				}
			}
		}
	}

	// 2.5: Fetch fund details before transaction to get securities prices for potential liquidation.
	details, _ := h.fundService.GetFundDetails(ctx, fundID)

	txErr := h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Lock fund row to prevent concurrent withdrawals.
		var fundLocked struct {
			LiquidAssets float64 `gorm:"column:liquid_assets"`
			AccountID    *int64  `gorm:"column:account_id"`
		}
		if err := tx.Raw(`
			SELECT liquid_assets, account_id
			FROM core_banking.investment_funds
			WHERE id = ?
			FOR UPDATE
		`, fundID).Scan(&fundLocked).Error; err != nil {
			return fmt.Errorf("zaključavanje fonda: %w", err)
		}

		// 2.5: Auto-liquidate securities when not enough liquid cash.
		if fundLocked.LiquidAssets < netWithdrawRSD {
			if details == nil || len(details.Securities) == 0 {
				return errors.New("fond nema dovoljno likvidnih sredstava za povlačenje")
			}

			// Sort by total value descending — liquidate largest positions first.
			secs := make([]domain.FundSecurityDetail, len(details.Securities))
			copy(secs, details.Securities)
			sort.Slice(secs, func(i, j int) bool {
				return secs[i].Quantity*secs[i].CurrentPriceRSD > secs[j].Quantity*secs[j].CurrentPriceRSD
			})

			needed := netWithdrawRSD - fundLocked.LiquidAssets
			for _, sec := range secs {
				if needed <= 0 {
					break
				}
				if sec.CurrentPriceRSD <= 0 {
					continue
				}
				canSell := needed / sec.CurrentPriceRSD
				if canSell > sec.Quantity {
					canSell = sec.Quantity
				}
				proceeds := canSell * sec.CurrentPriceRSD

				if err := tx.Exec(`
					UPDATE core_banking.fund_securities
					SET quantity = quantity - ?
					WHERE fund_id = ? AND listing_id = ?
				`, canSell, fundID, sec.ListingID).Error; err != nil {
					return fmt.Errorf("likvidacija hartije %d: %w", sec.ListingID, err)
				}
				tx.Exec(`
					DELETE FROM core_banking.fund_securities
					WHERE fund_id = ? AND listing_id = ? AND quantity <= 0
				`, fundID, sec.ListingID)

				if err := tx.Exec(`
					UPDATE core_banking.investment_funds
					SET liquid_assets = liquid_assets + ?
					WHERE id = ?
				`, proceeds, fundID).Error; err != nil {
					return fmt.Errorf("ažuriranje liquid_assets nakon likvidacije: %w", err)
				}
				fundLocked.LiquidAssets += proceeds
				needed -= proceeds
			}

			if needed > 0.01 {
				return errors.New("fond nema dovoljno sredstava za povlačenje")
			}
		}

		// Debit fund's liquid_assets by the net amount (commission stays in fund as profit).
		if err := tx.Exec(`
			UPDATE core_banking.investment_funds
			SET liquid_assets = GREATEST(0, liquid_assets - ?)
			WHERE id = ?
		`, netWithdrawRSD, fundID).Error; err != nil {
			return fmt.Errorf("umanjenje liquid_assets fonda: %w", err)
		}

		// Debit fund's RSD account.
		if fundLocked.AccountID != nil && *fundLocked.AccountID > 0 {
			if err := tx.Exec(`
				UPDATE core_banking.racun SET stanje_racuna = stanje_racuna - ? WHERE id = ?
			`, netWithdrawRSD, *fundLocked.AccountID).Error; err != nil {
				return fmt.Errorf("umanjenje stanja računa fonda: %w", err)
			}
		}

		// Credit client's account.
		if err := tx.Exec(`
			UPDATE core_banking.racun SET stanje_racuna = stanje_racuna + ? WHERE id = ?
		`, creditAmount, accountID).Error; err != nil {
			return fmt.Errorf("odobravanje klijentovog računa: %w", err)
		}

		// Update or remove client position.
		remaining := pos.InvestedRSD - withdrawRSD
		if remaining <= 0.001 {
			if err := tx.Delete(&pos).Error; err != nil {
				return fmt.Errorf("brisanje pozicije: %w", err)
			}
		} else {
			if err := tx.Model(&pos).Update("invested_rsd", remaining).Error; err != nil {
				return fmt.Errorf("ažuriranje pozicije: %w", err)
			}
		}

		// Persist transaction record (2.1).
		return tx.Create(&clientFundTransactionModel{
			FundID:    fundID,
			UserID:    userID,
			AmountRSD: withdrawRSD,
			Status:    "completed",
			CreatedAt: time.Now().UTC(),
			IsInflow:  false,
		}).Error
	})
	if txErr != nil {
		writeJSONError(w, http.StatusInternalServerError, "greška pri povlačenju: "+txErr.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":         "Povlačenje je uspešno obavljeno.",
		"withdrawnRsd":    withdrawRSD,
		"netWithdrawnRsd": netWithdrawRSD,
		"commissionRsd":   commissionRSD,
		"creditedAmount":  creditAmount,
		"currency":        accCurrency,
	})
}

// ─── POST /bank/funds/{id}/sell/{ticker} ─────────────────────────────────────

// sellSecurity liquidates a fund's entire position in a single security at current market price.
// Only supervisors/admins may call this endpoint.
func (h *FundHandler) sellSecurity(w http.ResponseWriter, r *http.Request, claims *auth.AccessClaims, fundID int64, ticker string) {
	if fundID <= 0 || ticker == "" {
		writeJSONError(w, http.StatusBadRequest, "neispravan fundId ili ticker")
		return
	}
	if !h.isSupervisor(claims) {
		writeJSONError(w, http.StatusForbidden, "samo supervizori mogu prodavati hartije fonda")
		return
	}

	ctx := r.Context()

	// Use GetFundDetails to get the security's current price and listingID.
	details, err := h.fundService.GetFundDetails(ctx, fundID)
	if err != nil || details == nil {
		writeJSONError(w, http.StatusNotFound, "fond nije pronađen")
		return
	}

	var target *domain.FundSecurityDetail
	for i := range details.Securities {
		if strings.EqualFold(details.Securities[i].Ticker, ticker) {
			target = &details.Securities[i]
			break
		}
	}
	if target == nil {
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("hartija %s nije pronađena u portfoliju fonda", ticker))
		return
	}
	if target.CurrentPriceRSD <= 0 || target.Quantity <= 0 {
		writeJSONError(w, http.StatusBadRequest, "tržišna cena ili količina nije dostupna")
		return
	}

	proceeds := target.Quantity * target.CurrentPriceRSD

	txErr := h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Remove security from fund portfolio.
		if err := tx.Exec(`
			DELETE FROM core_banking.fund_securities
			WHERE fund_id = ? AND listing_id = ?
		`, fundID, target.ListingID).Error; err != nil {
			return fmt.Errorf("brisanje hartije: %w", err)
		}

		// Credit fund's liquid_assets with sale proceeds.
		if err := tx.Exec(`
			UPDATE core_banking.investment_funds
			SET liquid_assets = liquid_assets + ?
			WHERE id = ?
		`, proceeds, fundID).Error; err != nil {
			return fmt.Errorf("ažuriranje liquid_assets: %w", err)
		}

		// Credit fund's RSD account.
		var fundAccountID *int64
		tx.Raw(`SELECT account_id FROM core_banking.investment_funds WHERE id = ?`, fundID).Scan(&fundAccountID)
		if fundAccountID != nil && *fundAccountID > 0 {
			if err := tx.Exec(`
				UPDATE core_banking.racun SET stanje_racuna = stanje_racuna + ? WHERE id = ?
			`, proceeds, *fundAccountID).Error; err != nil {
				return fmt.Errorf("ažuriranje računa fonda: %w", err)
			}
		}
		return nil
	})
	if txErr != nil {
		writeJSONError(w, http.StatusInternalServerError, "greška pri prodaji hartije: "+txErr.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":  fmt.Sprintf("Hartija %s je uspešno prodata.", ticker),
		"ticker":   ticker,
		"quantity": target.Quantity,
		"proceeds": proceeds,
		"priceRsd": target.CurrentPriceRSD,
	})
}

// ─── GET /bank/funds/{id}/performance ────────────────────────────────────────

type fundPerformancePoint struct {
	Period string  `json:"period"`
	Value  float64 `json:"value"`
}

type snapshotRow struct {
	Period string  `gorm:"column:period"`
	Value  float64 `gorm:"column:value"`
}

// fundPerformance returns aggregated FundValueRSD snapshots for a given period.
// Query param: period=monthly|quarterly|yearly (default: monthly).
func (h *FundHandler) fundPerformance(w http.ResponseWriter, r *http.Request, fundID int64) {
	if fundID <= 0 {
		writeJSONError(w, http.StatusBadRequest, "invalid fund id")
		return
	}

	period := r.URL.Query().Get("period")
	if period == "" {
		period = "monthly"
	}

	var query string
	switch period {
	case "quarterly":
		query = `
			SELECT
				EXTRACT(YEAR FROM snapshot_date)::TEXT || '-Q' ||
				EXTRACT(QUARTER FROM snapshot_date)::TEXT AS period,
				AVG(fund_value_rsd) AS value
			FROM core_banking.fund_performance_snapshots
			WHERE fund_id = ?
			  AND snapshot_date >= NOW() - INTERVAL '1 year'
			GROUP BY
				EXTRACT(YEAR FROM snapshot_date),
				EXTRACT(QUARTER FROM snapshot_date)
			ORDER BY
				EXTRACT(YEAR FROM snapshot_date),
				EXTRACT(QUARTER FROM snapshot_date)
		`
	case "yearly":
		query = `
			SELECT
				EXTRACT(YEAR FROM snapshot_date)::TEXT AS period,
				AVG(fund_value_rsd) AS value
			FROM core_banking.fund_performance_snapshots
			WHERE fund_id = ?
			  AND snapshot_date >= NOW() - INTERVAL '5 years'
			GROUP BY EXTRACT(YEAR FROM snapshot_date)
			ORDER BY EXTRACT(YEAR FROM snapshot_date)
		`
	default: // monthly
		query = `
			SELECT
				TO_CHAR(snapshot_date, 'YYYY-MM') AS period,
				AVG(fund_value_rsd) AS value
			FROM core_banking.fund_performance_snapshots
			WHERE fund_id = ?
			  AND snapshot_date >= NOW() - INTERVAL '12 months'
			GROUP BY TO_CHAR(snapshot_date, 'YYYY-MM')
			ORDER BY TO_CHAR(snapshot_date, 'YYYY-MM')
		`
	}

	var rows []snapshotRow
	if err := h.db.WithContext(r.Context()).Raw(query, fundID).Scan(&rows).Error; err != nil {
		writeJSONError(w, http.StatusInternalServerError, "greška pri dohvatu performansi fonda")
		return
	}

	out := make([]fundPerformancePoint, 0, len(rows))
	for _, row := range rows {
		out = append(out, fundPerformancePoint(row))
	}
	writeJSON(w, http.StatusOK, out)
}

// isSupervisor checks if the user is a supervisor (has SUPERVISOR permission).
func (h *FundHandler) isSupervisor(claims *auth.AccessClaims) bool {
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

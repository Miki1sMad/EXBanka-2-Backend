package handler

// fund_handler.go — HTTP handlers for investment funds (Moji fondovi).
//
// Endpoints:
//   GET  /bank/funds                       — list funds (clients: own positions; supervisors: managed funds)
//   POST /bank/funds/{id}/invest           — invest in a fund
//   POST /bank/funds/{id}/withdraw         — withdraw from a fund

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
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
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")

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

	callerID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "invalid user id")
		return
	}
	// Supervisor ulaže u ime banke — pozicija se vodi pod bankSystemUserID (2).
	userID := callerID
	if isBankSupervisorClaims(claims) {
		userID = bankSystemUserID
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

	// req.Amount is always in RSD (the fund's denomination).
	// For non-RSD accounts: calculate how much account currency to debit so that
	// the fund receives exactly req.Amount RSD. Commission (0.5%) is charged in
	// account currency on top of the RSD/kupovni equivalent.
	const fundInvestCommissionRate = 0.005
	amountRSD := req.Amount
	amountToDebit := req.Amount // in account currency; equals amountRSD for RSD accounts
	var commissionInAccCurrency float64
	isFX := accCurrency != "" && accCurrency != "RSD"

	var treasuryFromID, treasuryRSDID int64
	if isFX {
		rates, rateErr := h.exchangeService.GetRates(ctx)
		if rateErr != nil {
			writeJSONError(w, http.StatusInternalServerError, "kurs nije dostupan")
			return
		}
		var kupovni float64
		for _, rt := range rates {
			if rt.Oznaka == accCurrency {
				kupovni = rt.Kupovni // mid * (1 - spread); bank buys foreign from client
				break
			}
		}
		if kupovni <= 0 {
			writeJSONError(w, http.StatusBadRequest, "kurs nije dostupan za valutu "+accCurrency)
			return
		}
		baseAmount := amountRSD / kupovni                               // account currency without commission
		commissionInAccCurrency = baseAmount * fundInvestCommissionRate // 0.5% stays in bank treasury
		amountToDebit = baseAmount + commissionInAccCurrency

		var err error
		treasuryFromID, err = h.fetchTreasuryID(ctx, accCurrency)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "trezorski račun za valutu "+accCurrency+" nije pronađen")
			return
		}
		treasuryRSDID, err = h.fetchTreasuryID(ctx, "RSD")
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "trezorski RSD račun nije pronađen")
			return
		}
	}

	// Minimum contribution validation (always in RSD).
	if fund.MinimumContribution > 0 && amountRSD < fund.MinimumContribution {
		writeJSONError(w, http.StatusBadRequest,
			fmt.Sprintf("minimalni ulog je %.2f RSD", fund.MinimumContribution))
		return
	}

	// Check sufficient funds (in account currency).
	var available float64
	h.db.WithContext(ctx).Raw(`
		SELECT stanje_racuna - rezervisana_sredstva FROM core_banking.racun WHERE id = ?
	`, accountID).Scan(&available)
	if available < amountToDebit {
		writeJSONError(w, http.StatusBadRequest, "nedovoljno sredstava na računu")
		return
	}

	// Resolve fund's RSD account.
	var fundAccountID int64
	if fund.AccountID != nil {
		fundAccountID = *fund.AccountID
	}

	txErr := h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if isFX {
			// Non-RSD: 4-way FX journal entry through bank treasuries.
			// Client pays amountToDebit (foreign currency) → bank foreign treasury.
			// Bank RSD treasury pays amountRSD → fund RSD account.
			opis := fmt.Sprintf("Uplata u investicioni fond (%.4g RSD, plaćeno %.4g %s, provizija: %.4g %s)",
				amountRSD, amountToDebit, accCurrency, commissionInAccCurrency, accCurrency)
			now := time.Now().UTC()

			if err := tx.Exec(`UPDATE core_banking.racun SET stanje_racuna = stanje_racuna - ? WHERE id = ?`,
				amountToDebit, accountID).Error; err != nil {
				return fmt.Errorf("zaduži klijentski račun: %w", err)
			}
			if err := tx.Exec(`UPDATE core_banking.racun SET stanje_racuna = stanje_racuna + ? WHERE id = ?`,
				amountToDebit, treasuryFromID).Error; err != nil {
				return fmt.Errorf("odobri trezor %s: %w", accCurrency, err)
			}
			if err := tx.Exec(`UPDATE core_banking.racun SET stanje_racuna = stanje_racuna - ? WHERE id = ?`,
				amountRSD, treasuryRSDID).Error; err != nil {
				return fmt.Errorf("zaduži trezor RSD: %w", err)
			}
			if fundAccountID > 0 {
				if err := tx.Exec(`UPDATE core_banking.racun SET stanje_racuna = stanje_racuna + ? WHERE id = ?`,
					amountRSD, fundAccountID).Error; err != nil {
					return fmt.Errorf("odobri račun fonda: %w", err)
				}
			}
			for _, rec := range []struct {
				id    int64
				iznos float64
			}{
				{accountID, amountToDebit},
				{treasuryFromID, amountToDebit},
				{treasuryRSDID, amountRSD},
				{fundAccountID, amountRSD},
			} {
				if rec.id == 0 {
					continue
				}
				if err := tx.Exec(`
					INSERT INTO core_banking.transakcija (racun_id, tip_transakcije, iznos, opis, vreme_izvrsavanja, status)
					VALUES (?, 'MENJACNICA', ?, ?, ?, 'IZVRSEN')
				`, rec.id, rec.iznos, opis, now).Error; err != nil {
					return fmt.Errorf("upiši transakciju menjačnice: %w", err)
				}
			}
		} else {
			// RSD: direct debit/credit.
			if err := tx.Exec(`UPDATE core_banking.racun SET stanje_racuna = stanje_racuna - ? WHERE id = ?`,
				amountRSD, accountID).Error; err != nil {
				return err
			}
			if fundAccountID > 0 {
				if err := tx.Exec(`UPDATE core_banking.racun SET stanje_racuna = stanje_racuna + ? WHERE id = ?`,
					amountRSD, fundAccountID).Error; err != nil {
					return err
				}
			}
			if err := tx.Exec(`
				INSERT INTO core_banking.transakcija (racun_id, tip_transakcije, iznos, opis, vreme_izvrsavanja, status)
				VALUES (?, 'ISPLATA', ?, 'Uplata u investicioni fond', NOW(), 'IZVRSEN')
			`, accountID, amountRSD).Error; err != nil {
				return err
			}
			if fundAccountID > 0 {
				if err := tx.Exec(`
					INSERT INTO core_banking.transakcija (racun_id, tip_transakcije, iznos, opis, vreme_izvrsavanja, status)
					VALUES (?, 'UPLATA', ?, 'Prihod od investitora', NOW(), 'IZVRSEN')
				`, fundAccountID, amountRSD).Error; err != nil {
					return err
				}
			}
		}

		// Increase fund liquid_assets.
		if err := tx.Exec(`
			UPDATE core_banking.investment_funds SET liquid_assets = liquid_assets + ? WHERE id = ?
		`, amountRSD, fundID).Error; err != nil {
			return err
		}

		// Upsert client position.
		if err := tx.Exec(`
			INSERT INTO core_banking.fund_positions (fund_id, user_id, account_id, invested_rsd)
			VALUES (?, ?, ?, ?)
			ON CONFLICT (fund_id, user_id)
			DO UPDATE SET invested_rsd = fund_positions.invested_rsd + EXCLUDED.invested_rsd
		`, fundID, userID, accountID, amountRSD).Error; err != nil {
			return err
		}

		// Persist transaction record (2.1).
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

	callerID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "invalid user id")
		return
	}
	userID := callerID
	if isBankSupervisorClaims(claims) {
		userID = bankSystemUserID
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

	// FX conversion for withdrawal.
	// Supervisors (bank accounts): use Srednji rate, no commission.
	// Clients (own accounts): use Prodajni rate (bank sells FX to client), 1% commission already deducted above.
	isWithdrawFX := accCurrency != "" && accCurrency != "RSD"
	creditAmount := netWithdrawRSD
	var matchRate float64 // declared at outer scope so transaction closure can access it
	var withdrawTreasuryRSDID, withdrawTreasuryFXID int64
	if isWithdrawFX {
		rates, rateErr := h.exchangeService.GetRates(ctx)
		if rateErr != nil {
			writeJSONError(w, http.StatusInternalServerError, "kurs nije dostupan")
			return
		}
		for _, rt := range rates {
			if rt.Oznaka == accCurrency {
				if isSupervisor {
					matchRate = rt.Srednji
				} else {
					matchRate = rt.Prodajni // bank sells FX to client
				}
				break
			}
		}
		if matchRate <= 0 {
			writeJSONError(w, http.StatusBadRequest, "kurs nije dostupan za valutu "+accCurrency)
			return
		}
		creditAmount = netWithdrawRSD / matchRate

		var errT error
		withdrawTreasuryRSDID, errT = h.fetchTreasuryID(ctx, "RSD")
		if errT != nil {
			writeJSONError(w, http.StatusInternalServerError, "trezorski RSD račun nije pronađen")
			return
		}
		withdrawTreasuryFXID, errT = h.fetchTreasuryID(ctx, accCurrency)
		if errT != nil {
			writeJSONError(w, http.StatusInternalServerError, "trezorski račun za valutu "+accCurrency+" nije pronađen")
			return
		}
	}

	// 2.5: Fetch fund details before transaction to get securities prices for potential liquidation.
	details, _ := h.fundService.GetFundDetails(ctx, fundID)

	txErr := h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Lock fund row to prevent concurrent withdrawals.
		var fundLocked struct {
			LiquidAssets float64 `gorm:"column:liquid_assets"`
			AccountID    *int64  `gorm:"column:account_id"`
			ManagerID    int64   `gorm:"column:manager_id"`
		}
		if err := tx.Raw(`
			SELECT liquid_assets, account_id, manager_id
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

			// Proportional liquidation: sell each security in proportion to its share of total
			// portfolio market value. Falls back to initial_cost_rsd/quantity when market
			// price is unavailable (external API down), so liquidation can proceed in dev/test.
			secs := details.Securities

			// effectiveUnitPrice returns market price or acquisition price as fallback.
			effectiveUnitPrice := func(sec domain.FundSecurityDetail) float64 {
				if sec.CurrentPriceRSD > 0 {
					return sec.CurrentPriceRSD
				}
				if sec.Quantity > 0 && sec.InitialCostRSD > 0 {
					return sec.InitialCostRSD / sec.Quantity
				}
				return 0
			}

			totalSecValue := 0.0
			for _, sec := range secs {
				totalSecValue += sec.Quantity * effectiveUnitPrice(sec)
			}

			needed := netWithdrawRSD - fundLocked.LiquidAssets
			originalNeeded := needed
			for _, sec := range secs {
				unitPrice := effectiveUnitPrice(sec)
				if unitPrice <= 0 || totalSecValue <= 0 {
					continue
				}
				secValue := sec.Quantity * unitPrice
				targetProceeds := originalNeeded * (secValue / totalSecValue)
				if targetProceeds > secValue {
					targetProceeds = secValue
				}
				canSell := targetProceeds / unitPrice
				if targetProceeds >= secValue {
					canSell = sec.Quantity // sell entire position; avoids fp residuals
				}
				proceeds := canSell * unitPrice

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
				// Credit the fund's RSD account so the later debit can succeed.
				if fundLocked.AccountID != nil && *fundLocked.AccountID > 0 {
					if err := tx.Exec(`
						UPDATE core_banking.racun SET stanje_racuna = stanje_racuna + ? WHERE id = ?
					`, proceeds, *fundLocked.AccountID).Error; err != nil {
						return fmt.Errorf("kreditovanje računa fonda nakon likvidacije: %w", err)
					}
				}
				// Synthetic SELL order so portfolio aggregation stays in sync.
				if fundLocked.ManagerID > 0 && fundLocked.AccountID != nil && *fundLocked.AccountID > 0 {
					sellQty := int64(math.Round(canSell))
					if sellQty > 0 {
						now := time.Now().UTC()
						tx.Exec(`
							INSERT INTO core_banking.orders
							  (user_id, account_id, listing_id, order_type, direction, quantity,
							   contract_size, status, is_done, remaining_portions,
							   after_hours, all_or_none, margin, fund_id, last_modified, created_at)
							VALUES (?, ?, ?, 'MARKET', 'SELL', ?, 1, 'DONE', TRUE, 0,
							        FALSE, FALSE, FALSE, ?, ?, ?)
						`, fundLocked.ManagerID, *fundLocked.AccountID, sec.ListingID,
							sellQty, fundID, now, now)
					}
				}
				fundLocked.LiquidAssets += proceeds
				needed -= proceeds
			}

			if needed > 0.01 {
				if !req.WithdrawAll {
					return errors.New("fond nema dovoljno sredstava za povlačenje")
				}
				// Pristup A: pay what the fund has; close position entirely.
			}
		}

		// Actual payout may be less than requested when withdrawAll and fund is short.
		actualNetRSD := netWithdrawRSD
		if fundLocked.LiquidAssets < actualNetRSD {
			actualNetRSD = fundLocked.LiquidAssets
		}
		actualCredit := creditAmount
		if actualNetRSD < netWithdrawRSD {
			if isWithdrawFX && matchRate > 0 {
				actualCredit = actualNetRSD / matchRate
			} else {
				actualCredit = actualNetRSD
			}
		}

		// Debit fund's liquid_assets by the net amount (commission stays in fund as profit).
		if err := tx.Exec(`
			UPDATE core_banking.investment_funds
			SET liquid_assets = GREATEST(0, liquid_assets - ?)
			WHERE id = ?
		`, actualNetRSD, fundID).Error; err != nil {
			return fmt.Errorf("umanjenje liquid_assets fonda: %w", err)
		}

		if isWithdrawFX {
			// 4-way FX journal: fund RSD → bank RSD treasury → bank FX treasury → recipient FX account.
			opis := fmt.Sprintf("Povlačenje iz investicionog fonda (%.4g RSD → %.4g %s)", actualNetRSD, actualCredit, accCurrency)
			now := time.Now().UTC()
			fundAccID := int64(0)
			if fundLocked.AccountID != nil {
				fundAccID = *fundLocked.AccountID
			}
			if fundAccID > 0 {
				if err := tx.Exec(`UPDATE core_banking.racun SET stanje_racuna = stanje_racuna - ? WHERE id = ?`,
					actualNetRSD, fundAccID).Error; err != nil {
					return fmt.Errorf("umanjenje stanja računa fonda: %w", err)
				}
			}
			if err := tx.Exec(`UPDATE core_banking.racun SET stanje_racuna = stanje_racuna + ? WHERE id = ?`,
				actualNetRSD, withdrawTreasuryRSDID).Error; err != nil {
				return fmt.Errorf("odobri trezor RSD: %w", err)
			}
			if err := tx.Exec(`UPDATE core_banking.racun SET stanje_racuna = stanje_racuna - ? WHERE id = ?`,
				actualCredit, withdrawTreasuryFXID).Error; err != nil {
				return fmt.Errorf("zaduži trezor %s: %w", accCurrency, err)
			}
			if err := tx.Exec(`UPDATE core_banking.racun SET stanje_racuna = stanje_racuna + ? WHERE id = ?`,
				actualCredit, accountID).Error; err != nil {
				return fmt.Errorf("odobravanje računa za isplatu: %w", err)
			}
			for _, rec := range []struct {
				id    int64
				iznos float64
			}{
				{fundAccID, actualNetRSD},
				{withdrawTreasuryRSDID, actualNetRSD},
				{withdrawTreasuryFXID, actualCredit},
				{accountID, actualCredit},
			} {
				if rec.id == 0 {
					continue
				}
				if err := tx.Exec(`
					INSERT INTO core_banking.transakcija (racun_id, tip_transakcije, iznos, opis, vreme_izvrsavanja, status)
					VALUES (?, 'MENJACNICA', ?, ?, ?, 'IZVRSEN')
				`, rec.id, rec.iznos, opis, now).Error; err != nil {
					return fmt.Errorf("upiši transakciju menjačnice: %w", err)
				}
			}
		} else {
			// RSD: direct debit fund account, credit recipient account.
			if fundLocked.AccountID != nil && *fundLocked.AccountID > 0 {
				if err := tx.Exec(`
					UPDATE core_banking.racun SET stanje_racuna = stanje_racuna - ? WHERE id = ?
				`, actualNetRSD, *fundLocked.AccountID).Error; err != nil {
					return fmt.Errorf("umanjenje stanja računa fonda: %w", err)
				}
			}
			if err := tx.Exec(`
				UPDATE core_banking.racun SET stanje_racuna = stanje_racuna + ? WHERE id = ?
			`, actualCredit, accountID).Error; err != nil {
				return fmt.Errorf("odobravanje klijentovog računa: %w", err)
			}
			if fundLocked.AccountID != nil && *fundLocked.AccountID > 0 {
				if err := tx.Exec(`
					INSERT INTO core_banking.transakcija (racun_id, tip_transakcije, iznos, opis, vreme_izvrsavanja, status)
					VALUES (?, 'ISPLATA', ?, 'Isplata investitoru iz fonda', NOW(), 'IZVRSEN')
				`, *fundLocked.AccountID, actualNetRSD).Error; err != nil {
					return fmt.Errorf("zapis transakcije fonda: %w", err)
				}
			}
			if err := tx.Exec(`
				INSERT INTO core_banking.transakcija (racun_id, tip_transakcije, iznos, opis, vreme_izvrsavanja, status)
				VALUES (?, 'UPLATA', ?, 'Povlačenje iz investicionog fonda', NOW(), 'IZVRSEN')
			`, accountID, actualCredit).Error; err != nil {
				return fmt.Errorf("zapis transakcije klijenta: %w", err)
			}
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
		var fundMeta struct {
			AccountID *int64 `gorm:"column:account_id"`
			ManagerID int64  `gorm:"column:manager_id"`
		}
		tx.Raw(`SELECT account_id, manager_id FROM core_banking.investment_funds WHERE id = ?`, fundID).Scan(&fundMeta)
		if fundMeta.AccountID != nil && *fundMeta.AccountID > 0 {
			if err := tx.Exec(`
				UPDATE core_banking.racun SET stanje_racuna = stanje_racuna + ? WHERE id = ?
			`, proceeds, *fundMeta.AccountID).Error; err != nil {
				return fmt.Errorf("ažuriranje računa fonda: %w", err)
			}
		}
		// Synthetic SELL order so portfolio aggregation stays in sync.
		if fundMeta.ManagerID > 0 && fundMeta.AccountID != nil && *fundMeta.AccountID > 0 {
			sellQty := int64(math.Round(target.Quantity))
			if sellQty > 0 {
				now := time.Now().UTC()
				tx.Exec(`
					INSERT INTO core_banking.orders
					  (user_id, account_id, listing_id, order_type, direction, quantity,
					   contract_size, status, is_done, remaining_portions,
					   after_hours, all_or_none, margin, fund_id, last_modified, created_at)
					VALUES (?, ?, ?, 'MARKET', 'SELL', ?, 1, 'DONE', TRUE, 0,
					        FALSE, FALSE, FALSE, ?, ?, ?)
				`, fundMeta.ManagerID, *fundMeta.AccountID, target.ListingID,
					sellQty, fundID, now, now)
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

// fetchTreasuryID returns the ID of the bank's treasury account for the given currency.
// The treasury owner is trezor@exbanka.rs (user ID 2 in user-service).
func (h *FundHandler) fetchTreasuryID(ctx context.Context, currencyOznaka string) (int64, error) {
	var id int64
	err := h.db.WithContext(ctx).Raw(`
		SELECT ra.id FROM core_banking.racun ra
		JOIN core_banking.valuta v ON v.id = ra.id_valute
		WHERE ra.id_vlasnika = 2
		  AND v.oznaka = ?
		  AND ra.status = 'AKTIVAN'
		LIMIT 1
	`, currencyOznaka).Scan(&id).Error
	if err != nil {
		return 0, err
	}
	if id == 0 {
		return 0, fmt.Errorf("trezorski račun za valutu %s nije pronađen", currencyOznaka)
	}
	return id, nil
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

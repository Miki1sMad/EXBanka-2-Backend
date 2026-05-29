package worker

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"time"

	"banka-backend/services/bank-service/internal/domain"

	"gorm.io/gorm"
)

// fundReinvestRatio is the fraction of a fund's gross dividend that is reinvested
// by purchasing additional shares. The remainder (1 - fundReinvestRatio) is
// distributed as cash to fund clients. Change this constant to adjust the split.
const fundReinvestRatio = 0.5

// DividendWorker pays quarterly dividends to all stockholders on the last
// business day of each quarter-end month (March, June, September, December).
type DividendWorker struct {
	repo        domain.DividendPayoutRepository
	fundRepo    domain.InvestmentFundRepository
	db          *gorm.DB
	exchangeSvc domain.ExchangeService
	amqpURL     string
}

func NewDividendWorker(
	repo domain.DividendPayoutRepository,
	fundRepo domain.InvestmentFundRepository,
	db *gorm.DB,
	exchangeSvc domain.ExchangeService,
	amqpURL string,
) *DividendWorker {
	return &DividendWorker{
		repo:        repo,
		fundRepo:    fundRepo,
		db:          db,
		exchangeSvc: exchangeSvc,
		amqpURL:     amqpURL,
	}
}

// Start runs the worker until ctx is cancelled, checking twice a day.
func (w *DividendWorker) Start(ctx context.Context) {
	ticker := time.NewTicker(12 * time.Hour)
	defer ticker.Stop()
	log.Println("[dividend] worker started")
	for {
		select {
		case <-ctx.Done():
			log.Println("[dividend] worker stopped")
			return
		case <-ticker.C:
			w.runIfDue(ctx, time.Now())
		}
	}
}

func (w *DividendWorker) runIfDue(ctx context.Context, now time.Time) {
	if isLastBusinessDayOfQuarterEnd(now) {
		w.distribute(ctx, now)
	}
}

func isLastBusinessDayOfQuarterEnd(t time.Time) bool {
	m := t.Month()
	if m != time.March && m != time.June && m != time.September && m != time.December {
		return false
	}
	if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
		return false
	}
	// Find the next business day
	next := t.AddDate(0, 0, 1)
	for next.Weekday() == time.Saturday || next.Weekday() == time.Sunday {
		next = next.AddDate(0, 0, 1)
	}
	return next.Month() != t.Month()
}

type dividendListingRow struct {
	ID          int64   `gorm:"column:id"`
	Ticker      string  `gorm:"column:ticker"`
	Price       float64 `gorm:"column:price"`
	Ask         float64 `gorm:"column:ask"`
	DetailsJSON string  `gorm:"column:details_json"`
	CurrencyID  int64   `gorm:"column:currency_id"`
	Currency    string  `gorm:"column:currency"`
}

type dividendHolderRow struct {
	UserID    int64  `gorm:"column:user_id"`
	AccountID *int64 `gorm:"column:account_id"`
	IsClient  bool   `gorm:"column:is_client"`
	NetShares int64  `gorm:"column:net_shares"`
}

func (w *DividendWorker) distribute(ctx context.Context, now time.Time) {
	today := now.Truncate(24 * time.Hour)
	log.Printf("[dividend] distributing dividends for %s", today.Format("2006-01-02"))

	// Build a set of fund account IDs so we can skip them in the individual payout loop.
	fundAccountIDs := w.loadFundAccountIDs(ctx)

	// 1. Find all STOCK listings with dividend_yield > 0
	var listings []dividendListingRow
	if err := w.db.WithContext(ctx).Raw(`
		SELECT l.id, l.ticker, l.price, l.ask, l.details_json,
		       e.currency_id,
		       v.oznaka AS currency
		FROM core_banking.listing l
		JOIN core_banking.exchange e ON e.id = l.exchange_id
		JOIN core_banking.valuta v ON v.id = e.currency_id
		WHERE l.listing_type = 'STOCK'
		  AND (l.details_json::jsonb ->> 'dividend_yield')::float > 0
	`).Scan(&listings).Error; err != nil {
		log.Printf("[dividend] listing query error: %v", err)
		return
	}

	for _, listing := range listings {
		// 2. Skip if already paid today
		exists, err := w.repo.ExistsForPeriod(listing.ID, today)
		if err != nil {
			log.Printf("[dividend] exists check for listing %d: %v", listing.ID, err)
			continue
		}
		if exists {
			log.Printf("[dividend] listing %d already paid for %s, skipping", listing.ID, today.Format("2006-01-02"))
			continue
		}

		// 3. Parse dividend_yield
		var details domain.StockDetails
		if err := json.Unmarshal([]byte(listing.DetailsJSON), &details); err != nil {
			log.Printf("[dividend] parse details for listing %d: %v", listing.ID, err)
			continue
		}
		dividendYield := details.DividendYield
		if dividendYield <= 0 {
			continue
		}

		// 4. Get all current STOCK holders for this listing
		var holders []dividendHolderRow
		if err := w.db.WithContext(ctx).Raw(`
			WITH buy_agg AS (
				SELECT
					o.user_id,
					o.account_id,
					o.is_client,
					SUM(CASE WHEN o.status = 'DONE' THEN o.quantity ELSE (o.quantity - o.remaining_portions) END) AS bought
				FROM core_banking.orders o
				WHERE o.listing_id = ?
				  AND o.direction = 'BUY'
				  AND (
				      (o.status = 'DONE' AND o.is_done = TRUE)
				      OR (o.status = 'CANCELED' AND (o.quantity - o.remaining_portions) > 0)
				  )
				GROUP BY o.user_id, o.account_id, o.is_client
			),
			sell_agg AS (
				SELECT user_id, SUM(CASE WHEN status = 'DONE' THEN quantity ELSE (quantity - remaining_portions) END) AS sold
				FROM core_banking.orders
				WHERE listing_id = ?
				  AND direction = 'SELL'
				  AND fund_id IS NULL
				  AND (
				      (status = 'DONE' AND is_done = TRUE)
				      OR (status = 'CANCELED' AND (quantity - remaining_portions) > 0)
				  )
				GROUP BY user_id
			)
			SELECT
				b.user_id,
				b.account_id,
				b.is_client,
				(b.bought - COALESCE(s.sold, 0)) AS net_shares
			FROM buy_agg b
			LEFT JOIN sell_agg s ON s.user_id = b.user_id
			WHERE (b.bought - COALESCE(s.sold, 0)) > 0
		`, listing.ID, listing.ID).Scan(&holders).Error; err != nil {
			log.Printf("[dividend] holders query for listing %d: %v", listing.ID, err)
			continue
		}

		// 5. Pay each individual holder (skip fund accounts)
		for _, h := range holders {
			if h.AccountID != nil && fundAccountIDs[*h.AccountID] {
				continue // fund accounts are handled separately below
			}
			w.payHolder(ctx, listing, h, dividendYield, today)
		}

		// 6. Pay funds that hold this listing
		w.payFundDividends(ctx, listing, dividendYield, today)
	}
}

// loadFundAccountIDs fetches the account_id of every investment fund into a set.
func (w *DividendWorker) loadFundAccountIDs(ctx context.Context) map[int64]bool {
	var ids []int64
	w.db.WithContext(ctx).Raw(
		`SELECT account_id FROM core_banking.investment_funds WHERE account_id IS NOT NULL`,
	).Scan(&ids)
	m := make(map[int64]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

func (w *DividendWorker) payHolder(ctx context.Context, listing dividendListingRow, h dividendHolderRow, dividendYield float64, today time.Time) {
	// a. Price: prefer Ask, fall back to Price
	price := listing.Ask
	if price <= 0 {
		price = listing.Price
	}

	// b. Gross amount (quarterly = annual yield / 4)
	grossAmount := float64(h.NetShares) * price * (dividendYield / 4.0)

	// c/d. Tax
	var taxAmountNative float64
	netAmount := grossAmount
	isActuary := !h.IsClient
	if h.IsClient {
		taxAmountNative = grossAmount * 0.15
		netAmount = grossAmount * 0.85
	}

	// e. Convert tax to RSD
	taxAmountRSD := w.toRSD(ctx, taxAmountNative, listing.Currency)

	// f. Find account to credit
	creditAccountID, creditCurrency, creditNet := w.findCreditAccount(ctx, h, listing, netAmount, grossAmount)
	if creditAccountID == 0 {
		log.Printf("[dividend] no active account for userID=%d listing=%s, skipping", h.UserID, listing.Ticker)
		return
	}

	// g. Credit the account
	if err := w.db.WithContext(ctx).Exec(
		"UPDATE core_banking.racun SET stanje_racuna = stanje_racuna + ? WHERE id = ?",
		creditNet, creditAccountID,
	).Error; err != nil {
		log.Printf("[dividend] credit account %d for userID=%d: %v", creditAccountID, h.UserID, err)
		return
	}

	// h. Save payout record
	accountIDPtr := &creditAccountID
	if err := w.repo.Create(domain.DividendPayout{
		UserID:       h.UserID,
		ListingID:    listing.ID,
		Ticker:       listing.Ticker,
		Quantity:     h.NetShares,
		PriceOnDate:  price,
		GrossAmount:  grossAmount,
		TaxAmountRSD: taxAmountRSD,
		NetAmount:    creditNet,
		Currency:     creditCurrency,
		AccountID:    accountIDPtr,
		IsActuary:    isActuary,
		PaymentDate:  today,
	}); err != nil {
		log.Printf("[dividend] save payout for userID=%d listing=%s: %v", h.UserID, listing.Ticker, err)
	}

	// i. Log
	log.Printf("[dividend] paid %.2f %s (net %.2f %s, tax %.2f RSD) to userID=%d for %s",
		grossAmount, listing.Currency, creditNet, creditCurrency, taxAmountRSD, h.UserID, listing.Ticker)
}

// payFundDividends processes dividend income for all investment funds holding the given listing.
// Each fund receives its gross dividend, half is reinvested (buy more shares), half is
// distributed proportionally in RSD to fund clients.
func (w *DividendWorker) payFundDividends(ctx context.Context, listing dividendListingRow, dividendYield float64, today time.Time) {
	holdings, err := w.fundRepo.ListFundsByListingID(ctx, listing.ID)
	if err != nil {
		log.Printf("[dividend] list fund holdings for listing %d: %v", listing.ID, err)
		return
	}

	price := listing.Ask
	if price <= 0 {
		price = listing.Price
	}
	priceRSD := w.toRSD(ctx, price, listing.Currency)

	for _, holding := range holdings {
		if holding.AccountID == 0 {
			log.Printf("[dividend] fund %d has no bank account, skipping", holding.FundID)
			continue
		}

		grossRSD := w.toRSD(ctx, holding.Quantity*price*(dividendYield/4.0), listing.Currency)
		if grossRSD <= 0 {
			continue
		}

		// Credit fund's bank account and liquid_assets with the full dividend
		if err := w.db.WithContext(ctx).Exec(
			`UPDATE core_banking.racun SET stanje_racuna = stanje_racuna + ? WHERE id = ?`,
			grossRSD, holding.AccountID,
		).Error; err != nil {
			log.Printf("[dividend] credit fund %d account: %v", holding.FundID, err)
			continue
		}
		if err := w.fundRepo.AddLiquidAssets(ctx, holding.FundID, grossRSD); err != nil {
			log.Printf("[dividend] add liquid assets fund %d: %v", holding.FundID, err)
		}

		reinvestRSD := grossRSD * fundReinvestRatio
		distributeRSD := grossRSD * (1 - fundReinvestRatio)

		// Reinvest 50% by buying more shares for the fund
		if priceRSD > 0 {
			sharesBought := reinvestRSD / priceRSD
			if err := w.fundRepo.AddSecurityQuantity(ctx, holding.FundID, listing.ID, sharesBought, today, reinvestRSD); err != nil {
				log.Printf("[dividend] reinvest fund %d listing %d: %v", holding.FundID, listing.ID, err)
			} else {
				// Cash leaves fund (used to buy shares)
				_ = w.db.WithContext(ctx).Exec(
					`UPDATE core_banking.racun SET stanje_racuna = stanje_racuna - ? WHERE id = ?`,
					reinvestRSD, holding.AccountID,
				).Error
				_ = w.fundRepo.DeductLiquidAssets(ctx, holding.FundID, reinvestRSD)
				log.Printf("[dividend] fund %d reinvested %.2f RSD (%.4f shares) of %s", holding.FundID, reinvestRSD, sharesBought, listing.Ticker)
			}
		}

		// Distribute 50% proportionally to fund clients
		w.distributeFundDividend(ctx, listing, holding, distributeRSD, price, today)
	}
}

// distributeFundDividend credits each client's account proportionally and records payouts.
func (w *DividendWorker) distributeFundDividend(ctx context.Context, listing dividendListingRow, holding domain.FundHolding, distributeRSD float64, price float64, today time.Time) {
	positions, err := w.fundRepo.GetPositions(ctx, holding.FundID)
	if err != nil || len(positions) == 0 {
		log.Printf("[dividend] get positions for fund %d: %v", holding.FundID, err)
		return
	}

	totalInvested, err := w.fundRepo.GetTotalInvested(ctx, holding.FundID)
	if err != nil || totalInvested <= 0 {
		log.Printf("[dividend] total invested for fund %d: %v", holding.FundID, err)
		return
	}

	var totalDistributed float64
	for _, pos := range positions {
		if pos.TotalInvestedRSD <= 0 {
			continue
		}
		share := pos.TotalInvestedRSD / totalInvested
		clientRSD := distributeRSD * share

		// Find client's active RSD account
		var rsdAccountID int64
		w.db.WithContext(ctx).Raw(`
			SELECT r.id FROM core_banking.racun r
			JOIN core_banking.valuta v ON v.id = r.id_valute
			WHERE r.id_vlasnika = ? AND v.oznaka = 'RSD' AND r.status = 'AKTIVAN'
			LIMIT 1
		`, pos.UserID).Scan(&rsdAccountID)
		if rsdAccountID == 0 {
			log.Printf("[dividend] no RSD account for fund client userID=%d, skipping", pos.UserID)
			continue
		}

		taxRSD := clientRSD * 0.15
		netRSD := clientRSD * 0.85

		if err := w.db.WithContext(ctx).Exec(
			`UPDATE core_banking.racun SET stanje_racuna = stanje_racuna + ? WHERE id = ?`,
			netRSD, rsdAccountID,
		).Error; err != nil {
			log.Printf("[dividend] credit client %d for fund %d: %v", pos.UserID, holding.FundID, err)
			continue
		}
		totalDistributed += netRSD

		// Proportional share quantity (for record keeping)
		effectiveShares := int64(math.Round(holding.Quantity * share))

		if err := w.repo.Create(domain.DividendPayout{
			UserID:       pos.UserID,
			ListingID:    listing.ID,
			Ticker:       listing.Ticker,
			Quantity:     effectiveShares,
			PriceOnDate:  price,
			GrossAmount:  clientRSD,
			TaxAmountRSD: taxRSD,
			NetAmount:    netRSD,
			Currency:     "RSD",
			AccountID:    &rsdAccountID,
			IsActuary:    false,
			PaymentDate:  today,
		}); err != nil {
			log.Printf("[dividend] save fund payout for userID=%d: %v", pos.UserID, err)
		}
	}

	// Debit fund's account and liquid_assets for total distributed to clients
	if totalDistributed > 0 {
		_ = w.db.WithContext(ctx).Exec(
			`UPDATE core_banking.racun SET stanje_racuna = stanje_racuna - ? WHERE id = ?`,
			totalDistributed, holding.AccountID,
		).Error
		_ = w.fundRepo.DeductLiquidAssets(ctx, holding.FundID, totalDistributed)
		log.Printf("[dividend] fund %d distributed %.2f RSD of %s to %d clients", holding.FundID, totalDistributed, listing.Ticker, len(positions))
	}
}

// findCreditAccount finds the best account to credit and returns (accountID, currency, netAmount).
// Returns (0, "", 0) if no suitable account is found.
func (w *DividendWorker) findCreditAccount(ctx context.Context, h dividendHolderRow, listing dividendListingRow, netAmount float64, grossAmount float64) (int64, string, float64) {
	// Try accountID from the order first
	if h.AccountID != nil && *h.AccountID != 0 {
		var cnt int64
		w.db.WithContext(ctx).Raw(
			"SELECT COUNT(*) FROM core_banking.racun WHERE id = ? AND status = 'AKTIVAN'",
			*h.AccountID,
		).Scan(&cnt)
		if cnt == 1 {
			return *h.AccountID, listing.Currency, netAmount
		}
	}

	// Try any active account in the same currency
	var sameCurrencyID int64
	w.db.WithContext(ctx).Raw(`
		SELECT r.id FROM core_banking.racun r
		JOIN core_banking.valuta v ON v.id = r.id_valute
		WHERE r.id_vlasnika = ? AND v.oznaka = ? AND r.status = 'AKTIVAN'
		LIMIT 1
	`, h.UserID, listing.Currency).Scan(&sameCurrencyID)
	if sameCurrencyID != 0 {
		return sameCurrencyID, listing.Currency, netAmount
	}

	// Fall back to any active RSD account, converting netAmount to RSD
	var rsdAccountID int64
	w.db.WithContext(ctx).Raw(`
		SELECT r.id FROM core_banking.racun r
		JOIN core_banking.valuta v ON v.id = r.id_valute
		WHERE r.id_vlasnika = ? AND v.oznaka = 'RSD' AND r.status = 'AKTIVAN'
		LIMIT 1
	`, h.UserID).Scan(&rsdAccountID)
	if rsdAccountID != 0 {
		netInRSD := w.toRSD(ctx, netAmount, listing.Currency)
		return rsdAccountID, "RSD", netInRSD
	}

	return 0, "", 0
}

// toRSD converts amount from currency to RSD using the mid rate.
func (w *DividendWorker) toRSD(ctx context.Context, amount float64, currency string) float64 {
	if currency == "RSD" {
		return amount
	}
	rates, err := w.exchangeSvc.GetRates(ctx)
	if err != nil {
		log.Printf("[dividend] GetRates error: %v, skipping payout", err)
		return 0
	}
	for _, r := range rates {
		if r.Oznaka == currency {
			return amount * r.Srednji
		}
	}
	log.Printf("[dividend] no mid rate for %s, skipping payout", currency)
	return 0
}

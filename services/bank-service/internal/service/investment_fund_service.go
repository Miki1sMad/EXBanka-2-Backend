package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"banka-backend/services/bank-service/internal/domain"
)

// investmentFundService implementira domain.InvestmentFundService.
type investmentFundService struct {
	repo            domain.InvestmentFundRepository
	listingService  domain.ListingService
	exchangeService domain.ExchangeService
	accountService  domain.AccountService
	currencyRepo    domain.CurrencyRepository
}

// NewInvestmentFundService kreira novi InvestmentFundService sa svim zavisnostima.
func NewInvestmentFundService(
	repo domain.InvestmentFundRepository,
	listingService domain.ListingService,
	exchangeService domain.ExchangeService,
	accountService domain.AccountService,
	currencyRepo domain.CurrencyRepository,
) domain.InvestmentFundService {
	return &investmentFundService{
		repo:            repo,
		listingService:  listingService,
		exchangeService: exchangeService,
		accountService:  accountService,
		currencyRepo:    currencyRepo,
	}
}

// usdToRSDRate vraća srednji kurs USD→RSD. Fallback: 1.0 ako kurs nije dostupan.
func (s *investmentFundService) usdToRSDRate(ctx context.Context) float64 {
	rates, err := s.exchangeService.GetRates(ctx)
	if err != nil {
		return 1.0
	}
	for _, r := range rates {
		if r.Oznaka == "USD" && r.Srednji > 0 {
			return r.Srednji
		}
	}
	return 1.0
}

// securitiesValueRSD izračunava ukupnu tržišnu vrednost svih hartija fonda u RSD.
func (s *investmentFundService) securitiesValueRSD(ctx context.Context, securities []domain.FundSecurity, usdRate float64) float64 {
	total := 0.0
	for _, sec := range securities {
		listing, err := s.listingService.GetListingByID(ctx, sec.ListingID)
		if err != nil {
			continue
		}
		total += sec.Quantity * listing.Price * usdRate
	}
	return total
}

// ─── CreateFund ───────────────────────────────────────────────────────────────

func (s *investmentFundService) CreateFund(ctx context.Context, input domain.CreateFundInput) (*domain.InvestmentFund, error) {
	// Pronađi ID RSD valute
	rsdID, err := s.rsdCurrencyID(ctx)
	if err != nil {
		return nil, err
	}

	// Kreiraj dinarski račun koji pripada banci (id_vlasnika=2 = trezor@exbanka.rs)
	// ali se koristi isključivo za ovaj fond.
	accountID, err := s.accountService.CreateAccount(ctx, domain.CreateAccountInput{
		ZaposleniID:      input.ManagerID,
		VlasnikID:        2, // bankin entitet (trezor@exbanka.rs)
		ValutaID:         rsdID,
		KategorijaRacuna: "TEKUCI",
		VrstaRacuna:      "POSLOVNI",
		Podvrsta:         "",
		NazivRacuna:      "Fond: " + input.Name,
		StanjeRacuna:     0,
	})
	if err != nil {
		return nil, fmt.Errorf("kreiranje RSD računa za fond: %w", err)
	}

	fund := domain.InvestmentFund{
		Name:                input.Name,
		Description:         input.Description,
		MinimumContribution: input.MinimumContribution,
		ManagerID:           input.ManagerID,
		LiquidAssets:        0,
		AccountID:           accountID,
		CreatedAt:           time.Now().UTC(),
	}
	return s.repo.Create(ctx, fund)
}

// rsdCurrencyID dohvata ID RSD valute iz šifarnika.
func (s *investmentFundService) rsdCurrencyID(ctx context.Context) (int64, error) {
	currencies, err := s.currencyRepo.GetAll(ctx)
	if err != nil {
		return 0, fmt.Errorf("dohvat šifarnika valuta: %w", err)
	}
	for _, c := range currencies {
		if c.Oznaka == "RSD" {
			return c.ID, nil
		}
	}
	return 0, fmt.Errorf("RSD valuta nije pronađena u šifarniku")
}

// ─── GetFundByID ──────────────────────────────────────────────────────────────

func (s *investmentFundService) GetFundByID(ctx context.Context, id int64) (*domain.InvestmentFund, error) {
	return s.repo.GetByID(ctx, id)
}

// ─── GetFundDetails ───────────────────────────────────────────────────────────

func (s *investmentFundService) GetFundDetails(ctx context.Context, id int64) (*domain.FundDetails, error) {
	fund, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	securities, err := s.repo.GetSecurities(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("dohvat hartija fonda: %w", err)
	}

	usdRate := s.usdToRSDRate(ctx)

	secDetails := make([]domain.FundSecurityDetail, 0, len(securities))
	securitiesRSD := 0.0

	for _, sec := range securities {
		listing, err := s.listingService.GetListingByID(ctx, sec.ListingID)
		if err != nil {
			// Hartija možda više ne postoji; preskačemo je ali ne prekidamo
			continue
		}
		priceRSD := listing.Price * usdRate
		securitiesRSD += sec.Quantity * priceRSD

		secDetails = append(secDetails, domain.FundSecurityDetail{
			FundSecurity:      sec,
			Ticker:            listing.Ticker,
			CurrentPriceUSD:   listing.Price,
			CurrentPriceRSD:   priceRSD,
			ChangePercent:     listing.ChangePercent,
			Volume:            listing.Volume,
			InitialMarginCost: listing.InitialMarginCost,
		})
	}

	fundValue := fund.LiquidAssets + securitiesRSD

	totalInvested, err := s.repo.GetTotalInvested(ctx, id)
	if err != nil {
		totalInvested = 0
	}
	profit := fundValue - totalInvested

	return &domain.FundDetails{
		InvestmentFund: *fund,
		FundValueRSD:   fundValue,
		Profit:         profit,
		Securities:     secDetails,
	}, nil
}

// ─── ListFunds ────────────────────────────────────────────────────────────────

func (s *investmentFundService) ListFunds(ctx context.Context, filter domain.FundFilter) ([]domain.FundListItem, error) {
	all, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}

	usdRate := s.usdToRSDRate(ctx)
	searchLower := strings.ToLower(strings.TrimSpace(filter.Search))

	items := make([]domain.FundListItem, 0, len(all))
	for _, fund := range all {
		// Filtriranje po pretrazi
		if searchLower != "" {
			nameMatch := strings.Contains(strings.ToLower(fund.Name), searchLower)
			descMatch := strings.Contains(strings.ToLower(fund.Description), searchLower)
			if !nameMatch && !descMatch {
				continue
			}
		}

		// Izračunaj VrednostFonda
		securities, _ := s.repo.GetSecurities(ctx, fund.ID)
		securitiesRSD := s.securitiesValueRSD(ctx, securities, usdRate)
		fundValue := fund.LiquidAssets + securitiesRSD

		// Izračunaj Profit
		totalInvested, _ := s.repo.GetTotalInvested(ctx, fund.ID)
		profit := fundValue - totalInvested

		items = append(items, domain.FundListItem{
			InvestmentFund: fund,
			FundValueRSD:   fundValue,
			Profit:         profit,
			Stats:          s.computeStats(ctx, fund.ID),
		})
	}

	s.sortFundItems(items, filter.SortBy, filter.SortOrder)

	return items, nil
}

// computeStats calculates performance statistics from historical snapshots.
// Returns nil when fewer than domain.MinSnapshotsForStats snapshots exist.
func (s *investmentFundService) computeStats(ctx context.Context, fundID int64) *domain.FundStats {
	snapshots, err := s.repo.GetSnapshots(ctx, fundID)
	if err != nil || len(snapshots) < domain.MinSnapshotsForStats {
		return nil
	}

	// ── Annualized return (CAGR) ──────────────────────────────────────────────
	first := snapshots[0].FundValueRSD
	last := snapshots[len(snapshots)-1].FundValueRSD
	days := snapshots[len(snapshots)-1].SnapshotDate.Sub(snapshots[0].SnapshotDate).Hours() / 24.0
	var annualizedReturn float64
	if first > 0 && days > 0 {
		annualizedReturn = math.Pow(last/first, 365.0/days) - 1
	}

	// ── Monthly returns (last snapshot per calendar month) ────────────────────
	type monthVal struct {
		year, month int
		value       float64
	}
	byMonth := map[[2]int]float64{}
	for _, s := range snapshots {
		key := [2]int{s.SnapshotDate.Year(), int(s.SnapshotDate.Month())}
		byMonth[key] = s.FundValueRSD // later snapshot overwrites — keeps last of month
	}
	// sort months
	monthKeys := make([][2]int, 0, len(byMonth))
	for k := range byMonth {
		monthKeys = append(monthKeys, k)
	}
	sort.Slice(monthKeys, func(i, j int) bool {
		if monthKeys[i][0] != monthKeys[j][0] {
			return monthKeys[i][0] < monthKeys[j][0]
		}
		return monthKeys[i][1] < monthKeys[j][1]
	})

	var returns []float64
	for i := 1; i < len(monthKeys); i++ {
		prev := byMonth[monthKeys[i-1]]
		cur := byMonth[monthKeys[i]]
		if prev > 0 {
			returns = append(returns, (cur-prev)/prev)
		}
	}

	// ── Volatility (annualised std-dev of monthly returns) ───────────────────
	var volatility float64
	if len(returns) >= 2 {
		mean := 0.0
		for _, r := range returns {
			mean += r
		}
		mean /= float64(len(returns))
		variance := 0.0
		for _, r := range returns {
			d := r - mean
			variance += d * d
		}
		variance /= float64(len(returns) - 1)
		volatility = math.Sqrt(variance) * math.Sqrt(12) // annualise
	}

	// ── Max drawdown ─────────────────────────────────────────────────────────
	peak := snapshots[0].FundValueRSD
	maxDrawdown := 0.0
	for _, s := range snapshots[1:] {
		if s.FundValueRSD > peak {
			peak = s.FundValueRSD
		}
		if peak > 0 {
			dd := (s.FundValueRSD - peak) / peak
			if dd < maxDrawdown {
				maxDrawdown = dd
			}
		}
	}
	maxDrawdown = math.Abs(maxDrawdown)

	// ── Reward-to-variability ─────────────────────────────────────────────────
	var rtv float64
	if volatility > 0 {
		rtv = annualizedReturn / volatility
	}

	return &domain.FundStats{
		AnnualizedReturn:    annualizedReturn,
		Volatility:          volatility,
		MaxDrawdown:         maxDrawdown,
		RewardToVariability: rtv,
		SnapshotCount:       len(snapshots),
	}
}

// sortFundItems sortira listu fondova prema zadatim parametrima.
func (s *investmentFundService) sortFundItems(items []domain.FundListItem, sortBy, sortOrder string) {
	if sortBy == "" {
		return
	}
	desc := strings.ToUpper(strings.TrimSpace(sortOrder)) == "DESC"

	statVal := func(item domain.FundListItem, field string) float64 {
		if item.Stats == nil {
			return math.Inf(-1) // funds without stats sort last when ascending
		}
		switch field {
		case "annualizedreturn":
			return item.Stats.AnnualizedReturn
		case "rewardtovariability":
			return item.Stats.RewardToVariability
		case "maxdrawdown":
			return item.Stats.MaxDrawdown
		case "volatility":
			return item.Stats.Volatility
		}
		return 0
	}

	sort.Slice(items, func(i, j int) bool {
		var less bool
		key := strings.ToLower(strings.TrimSpace(sortBy))
		switch key {
		case "name":
			less = strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
		case "description":
			less = strings.ToLower(items[i].Description) < strings.ToLower(items[j].Description)
		case "fundvalue":
			less = items[i].FundValueRSD < items[j].FundValueRSD
		case "profit":
			less = items[i].Profit < items[j].Profit
		case "minimumcontribution":
			less = items[i].MinimumContribution < items[j].MinimumContribution
		case "annualizedreturn", "rewardtovariability", "maxdrawdown", "volatility":
			less = statVal(items[i], key) < statVal(items[j], key)
		default:
			less = items[i].ID < items[j].ID
		}
		if desc {
			return !less
		}
		return less
	})
}

// ─── TransferManagerFunds ─────────────────────────────────────────────────────

// TransferManagerFunds prebacuje sve fondove sa oldManagerID na adminID.
// Poziva se automatski kada admin ukloni isSupervisor permisiju supervizoru.
func (s *investmentFundService) TransferManagerFunds(ctx context.Context, oldManagerID, adminID int64) error {
	return s.repo.TransferManagerFunds(ctx, oldManagerID, adminID)
}

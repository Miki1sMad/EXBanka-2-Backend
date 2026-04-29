package service

import (
	"context"
	"fmt"
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
		})
	}

	s.sortFundItems(items, filter.SortBy, filter.SortOrder)

	return items, nil
}

// sortFundItems sortira listu fondova prema zadatim parametrima.
func (s *investmentFundService) sortFundItems(items []domain.FundListItem, sortBy, sortOrder string) {
	if sortBy == "" {
		return
	}
	desc := strings.ToUpper(strings.TrimSpace(sortOrder)) == "DESC"

	sort.Slice(items, func(i, j int) bool {
		var less bool
		switch strings.ToLower(strings.TrimSpace(sortBy)) {
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

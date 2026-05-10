// Package service — interbank_option_contract_service.go
//
// Servis za prihvatanje OTC ponude i izvršavanje opcionog ugovora preko
// si-tx-proto transaction protocol-a.
//
// Accept tok (po protokolu):
//  1. Buyer credit premium     ← prema Posting protokolu
//  2. Seller debit premium
//  3. Buyer debit optionContract
//  4. Seller credit optionContract
//
// Exercise tok:
//   - debit OPTION account za pricePerUnit * amount novca
//   - credit buyer za pricePerUnit * amount novca
//   - credit OPTION account za amount stocks
//   - debit buyer za amount stocks
package service

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"banka-backend/services/bank-service/internal/domain"

	"github.com/shopspring/decimal"
)

// InterbankOptionContractService — kreiranje i izvršavanje opcionih ugovora.
type InterbankOptionContractService struct {
	repo             domain.InterbankRepository
	coordinator      *TransactionCoordinator
	ourRoutingNumber int64
}

// NewInterbankOptionContractService konstruktor.
func NewInterbankOptionContractService(
	repo domain.InterbankRepository,
	coordinator *TransactionCoordinator,
	ourRoutingNumber int64,
) *InterbankOptionContractService {
	return &InterbankOptionContractService{
		repo:             repo,
		coordinator:      coordinator,
		ourRoutingNumber: ourRoutingNumber,
	}
}

// AcceptNegotiation — GET /negotiations/{routing}/{id}/accept.
//
// Pravilo: prihvata onaj kome je trenutno red — strana koja NIJE poslednja
// menjala. Pošto ne znamo identitet caller-a (zahtev je bez payload-a), banka
// prodavca smatra prihvatanje validnim ako je negotiation OPEN i isOngoing.
//
// Po protokolu, ovo je idempotentna akcija; ako je već prihvaćena, vraćamo OK.
func (s *InterbankOptionContractService) AcceptNegotiation(ctx context.Context, routing int64, id string) error {
	n, err := s.repo.GetNegotiationByID(ctx, routing, id)
	if err != nil {
		return err
	}
	if n == nil {
		return domain.ErrInterbankNotFound
	}
	if !n.IsOngoing && n.Status != "ACCEPTED" {
		return domain.ErrInterbankConflict
	}
	if n.Status == "ACCEPTED" {
		return nil
	}

	// 1) Kreiraj opcioni ugovor (status ACTIVE).
	contract := &domain.InterbankOptionContract{
		NegotiationRoutingNumber: n.NegotiationRoutingNumber,
		NegotiationForeignID:     n.NegotiationForeignID,
		StockTicker:              n.StockTicker,
		PriceCurrency:            n.PriceCurrency,
		PriceAmount:              n.PriceAmount,
		PremiumCurrency:          n.PremiumCurrency,
		PremiumAmount:            n.PremiumAmount,
		SettlementDate:           n.SettlementDate,
		Amount:                   n.Amount,
		BuyerRoutingNumber:       n.BuyerRoutingNumber,
		BuyerID:                  n.BuyerID,
		SellerRoutingNumber:      n.SellerRoutingNumber,
		SellerID:                 n.SellerID,
		Status:                   "ACTIVE",
	}
	if err := s.repo.CreateOptionContract(ctx, contract); err != nil {
		return fmt.Errorf("kreiranje opcionog ugovora: %w", err)
	}

	// 2) Zatvori pregovaranje.
	n.IsOngoing = false
	n.Status = "ACCEPTED"
	if err := s.repo.UpdateNegotiation(ctx, n); err != nil {
		return err
	}

	// 3) Premija: po protokolu, premium se kreće preko transaction protocol-a.
	//    Banka koja je primila accept (mi, banka prodavca) inicira NEW_TX:
	//      seller credit premium
	//      buyer debit premium
	//    Ako su obe banke iste, ne šaljemo poruku — interno bi to obavila
	//    InterbankPaymentService. Ovo ostavljamo coordinator-u.

	if n.BuyerRoutingNumber != s.ourRoutingNumber {
		// Inter-bank: pokreni 2-phase commit za premiju.
		txID := domain.ForeignBankId{
			RoutingNumber: s.ourRoutingNumber,
			ID:            NewLocalKey(),
		}
		// Posting konstrukcija — premium tok.
		buyerPerson := &domain.ForeignBankId{RoutingNumber: n.BuyerRoutingNumber, ID: n.BuyerID}
		sellerPerson := &domain.ForeignBankId{RoutingNumber: n.SellerRoutingNumber, ID: n.SellerID}
		premium := domain.MonetaryAsset{Currency: n.PremiumCurrency}
		postings := []domain.Posting{
			{
				Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: buyerPerson},
				Amount:  n.PremiumAmount.Neg(),
				Asset:   domain.Asset{Type: domain.AssetTypeMonas, MonAs: &premium},
			},
			{
				Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: sellerPerson},
				Amount:  n.PremiumAmount,
				Asset:   domain.Asset{Type: domain.AssetTypeMonas, MonAs: &premium},
			},
		}
		txn := domain.Transaction{
			Postings:       postings,
			TransactionID:  txID,
			Message:        "OTC premium",
			PaymentCode:    "289",
			PaymentPurpose: "OTC_PREMIUM",
		}
		// Lokalna banka koordinira; coordinator će proći prepare/commit/rollback ciklus.
		// (Ako lokalna priprema ne uspe, accept i dalje ostaje — premium se može retry-ovati ručno.)
		_ = txn
		// Napomena: integraciono iskreirana premium-tx će biti obrađena preko
		// TransactionCoordinator.InitiateInterbankPayment-stila API-ja kada
		// klijent UI klikne "Prihvati"; ovde ne pokrećemo automatski jer ne
		// znamo sender račun. Frontend zove POST /bank/interbank/otc/{id}/accept.
	}

	return nil
}

// ListContracts — vraća sve aktivne (i istorijske) ugovore za korisnika.
func (s *InterbankOptionContractService) ListContracts(ctx context.Context, userID int64) ([]domain.InterbankOptionContract, error) {
	return s.repo.ListContractsForUser(ctx, s.ourRoutingNumber, strconv.FormatInt(userID, 10))
}

// GetContract — jedan ugovor.
func (s *InterbankOptionContractService) GetContract(ctx context.Context, routing int64, id string) (*domain.InterbankOptionContract, error) {
	c, err := s.repo.GetOptionContract(ctx, routing, id)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, domain.ErrInterbankNotFound
	}
	return c, nil
}

// ExerciseContract — kupac koristi opciju. Konstruiše Transaction po
// protokolu i pokreće 2-phase commit.
func (s *InterbankOptionContractService) ExerciseContract(ctx context.Context, callerUserID int64, routing int64, id string) (*domain.InterbankTransaction, error) {
	c, err := s.repo.GetOptionContract(ctx, routing, id)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, domain.ErrInterbankNotFound
	}
	if c.BuyerRoutingNumber != s.ourRoutingNumber {
		return nil, fmt.Errorf("kupac nije na ovoj banci")
	}
	if c.BuyerID != strconv.FormatInt(callerUserID, 10) {
		return nil, fmt.Errorf("samo kupac može iskoristiti ugovor")
	}
	if c.Status != "ACTIVE" {
		return nil, fmt.Errorf("opcija nije ACTIVE: %s", c.Status)
	}
	if !c.SettlementDate.After(time.Now().UTC()) {
		return nil, fmt.Errorf("settlementDate je prošao")
	}

	// Konstruiši Transaction:
	//   debit  OPTION (negotiation) za pricePerUnit * amount novca
	//   credit buyer (PERSON)       za pricePerUnit * amount novca   ← novac kupcu (od opcije)
	//   credit OPTION (negotiation) za amount akcija
	//   debit  buyer (PERSON)       za amount akcija
	//
	// Po protokolu, OPTION pseudo-account ima i novac i akcije; "debit OPTION
	// za novac" znači da se novac skida sa OPTION-a, ali to je interna
	// validacija banke koja drži OPTION (= banka prodavca).
	totalCash := c.PriceAmount.Mul(decimal.NewFromInt(int64(c.Amount)))

	optAccount := &domain.ForeignBankId{
		RoutingNumber: c.NegotiationRoutingNumber,
		ID:            c.NegotiationForeignID,
	}
	buyerPerson := &domain.ForeignBankId{
		RoutingNumber: c.BuyerRoutingNumber,
		ID:            c.BuyerID,
	}

	cashAsset := domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: c.PriceCurrency}}
	stockAsset := domain.Asset{Type: domain.AssetTypeStock, Stock: &domain.StockDescription{Ticker: c.StockTicker}}

	postings := []domain.Posting{
		{
			Account: domain.TxAccount{Type: domain.AccountKindOption, ID: optAccount},
			Amount:  totalCash.Neg(),
			Asset:   cashAsset,
		},
		{
			Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: buyerPerson},
			Amount:  totalCash,
			Asset:   cashAsset,
		},
		{
			Account: domain.TxAccount{Type: domain.AccountKindOption, ID: optAccount},
			Amount:  decimal.NewFromInt(int64(c.Amount)),
			Asset:   stockAsset,
		},
		{
			Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: buyerPerson},
			Amount:  decimal.NewFromInt(int64(c.Amount)).Neg(),
			Asset:   stockAsset,
		},
	}

	transaction := domain.Transaction{
		Postings: postings,
		TransactionID: domain.ForeignBankId{
			RoutingNumber: s.ourRoutingNumber,
			ID:            NewLocalKey(),
		},
		Message:        "Exercise OTC option " + c.NegotiationForeignID,
		PaymentCode:    "289",
		PaymentPurpose: "OTC_EXERCISE",
	}

	// Da bismo iskoristili coordinator infrastrukturu, koristimo specijalnu
	// rutu InitiateInterbankTransaction (analogno InitiateInterbankPayment).
	return s.coordinator.InitiateInterbankTransaction(ctx, transaction, &callerUserID)
}

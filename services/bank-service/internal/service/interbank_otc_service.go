// Package service — interbank_otc_service.go
//
// OTC pregovaranje između banaka po protokolu si-tx-proto.
//
// Pravila (autoritativna kopija je kod banke prodavca):
//   - pregovaranje pokreće kupac (POST /negotiations).
//   - kontraponuda može poslati samo strana koja nije poslednja menjala (PUT).
//   - GET vraća OtcNegotiation = OtcOffer + isOngoing.
//   - DELETE zatvara pregovaranje (isOngoing = false).
//   - GET .../accept inicira prelazak u opcioni ugovor (vidi InterbankOptionContractService).
package service

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"banka-backend/services/bank-service/internal/domain"
)

// InterbankOTCService — servis za inter-bank OTC pregovaranje.
type InterbankOTCService struct {
	repo             domain.InterbankRepository
	ourRoutingNumber int64
}

// NewInterbankOTCService konstruktor.
func NewInterbankOTCService(repo domain.InterbankRepository, ourRoutingNumber int64) *InterbankOTCService {
	return &InterbankOTCService{repo: repo, ourRoutingNumber: ourRoutingNumber}
}

// CreateNegotiation — banka prodavca prima POST /negotiations od banke kupca.
// Vraća lokalno generisan ForeignBankId pregovaranja.
func (s *InterbankOTCService) CreateNegotiation(ctx context.Context, offer domain.OtcOffer) (*domain.ForeignBankId, error) {
	// Provera: prodavac mora biti na ovoj banci.
	if offer.SellerID.RoutingNumber != s.ourRoutingNumber {
		return nil, fmt.Errorf("interbank otc: prodavac nije na ovoj banci (sellerId.routingNumber=%d, our=%d)",
			offer.SellerID.RoutingNumber, s.ourRoutingNumber)
	}
	if offer.Amount <= 0 {
		return nil, fmt.Errorf("interbank otc: amount mora biti > 0")
	}
	if offer.Stock.Ticker == "" {
		return nil, fmt.Errorf("interbank otc: ticker je obavezan")
	}
	if offer.SettlementDate == "" {
		return nil, fmt.Errorf("interbank otc: settlementDate je obavezan")
	}
	settlement, err := time.Parse(time.RFC3339, offer.SettlementDate)
	if err != nil {
		return nil, fmt.Errorf("interbank otc: nevalidan settlementDate: %w", err)
	}

	id := domain.ForeignBankId{
		RoutingNumber: s.ourRoutingNumber,
		ID:            NewLocalKey(),
	}
	n := &domain.InterbankNegotiation{
		NegotiationRoutingNumber:  id.RoutingNumber,
		NegotiationForeignID:      id.ID,
		StockTicker:               offer.Stock.Ticker,
		SettlementDate:            settlement,
		PriceCurrency:             offer.PricePerUnit.Currency,
		PriceAmount:               offer.PricePerUnit.Amount,
		PremiumCurrency:           offer.Premium.Currency,
		PremiumAmount:             offer.Premium.Amount,
		Amount:                    offer.Amount,
		BuyerRoutingNumber:        offer.BuyerID.RoutingNumber,
		BuyerID:                   offer.BuyerID.ID,
		SellerRoutingNumber:       offer.SellerID.RoutingNumber,
		SellerID:                  offer.SellerID.ID,
		LastModifiedRoutingNumber: offer.LastModifiedBy.RoutingNumber,
		LastModifiedID:            offer.LastModifiedBy.ID,
		IsOngoing:                 true,
		Status:                    "OPEN",
	}
	if err := s.repo.CreateNegotiation(ctx, n); err != nil {
		return nil, err
	}
	return &id, nil
}

// CounterNegotiation — PUT /negotiations/{routing}/{id}.
// Vraća 409 (ErrInterbankConflict) ako: pregovaranje je zatvoreno ili je
// caller upravo zadnji menjao ponudu.
func (s *InterbankOTCService) CounterNegotiation(ctx context.Context, routing int64, id string, offer domain.OtcOffer) error {
	n, err := s.repo.GetNegotiationByID(ctx, routing, id)
	if err != nil {
		return err
	}
	if n == nil {
		return domain.ErrInterbankNotFound
	}
	if !n.IsOngoing || n.Status != "OPEN" {
		return domain.ErrInterbankConflict
	}
	// Naizmenično pregovaranje: caller (lastModifiedBy) ne sme biti onaj koji
	// je menjao prethodno.
	if n.LastModifiedRoutingNumber == offer.LastModifiedBy.RoutingNumber &&
		n.LastModifiedID == offer.LastModifiedBy.ID {
		return domain.ErrInterbankConflict
	}
	settlement, err := time.Parse(time.RFC3339, offer.SettlementDate)
	if err != nil {
		return fmt.Errorf("nevalidan settlementDate: %w", err)
	}
	n.StockTicker = offer.Stock.Ticker
	n.SettlementDate = settlement
	n.PriceCurrency = offer.PricePerUnit.Currency
	n.PriceAmount = offer.PricePerUnit.Amount
	n.PremiumCurrency = offer.Premium.Currency
	n.PremiumAmount = offer.Premium.Amount
	n.Amount = offer.Amount
	n.LastModifiedRoutingNumber = offer.LastModifiedBy.RoutingNumber
	n.LastModifiedID = offer.LastModifiedBy.ID
	return s.repo.UpdateNegotiation(ctx, n)
}

// GetNegotiation — GET /negotiations/{routing}/{id} → OtcNegotiation.
func (s *InterbankOTCService) GetNegotiation(ctx context.Context, routing int64, id string) (*domain.OtcNegotiation, error) {
	n, err := s.repo.GetNegotiationByID(ctx, routing, id)
	if err != nil {
		return nil, err
	}
	if n == nil {
		return nil, domain.ErrInterbankNotFound
	}
	return &domain.OtcNegotiation{
		OtcOffer: domain.OtcOffer{
			Stock:          domain.StockDescription{Ticker: n.StockTicker},
			SettlementDate: n.SettlementDate.Format(time.RFC3339),
			PricePerUnit:   domain.MonetaryValue{Currency: n.PriceCurrency, Amount: n.PriceAmount},
			Premium:        domain.MonetaryValue{Currency: n.PremiumCurrency, Amount: n.PremiumAmount},
			BuyerID:        domain.ForeignBankId{RoutingNumber: n.BuyerRoutingNumber, ID: n.BuyerID},
			SellerID:       domain.ForeignBankId{RoutingNumber: n.SellerRoutingNumber, ID: n.SellerID},
			Amount:         n.Amount,
			LastModifiedBy: domain.ForeignBankId{RoutingNumber: n.LastModifiedRoutingNumber, ID: n.LastModifiedID},
		},
		IsOngoing: n.IsOngoing,
	}, nil
}

// CancelNegotiation — DELETE /negotiations/{routing}/{id}; postavlja IsOngoing=false.
func (s *InterbankOTCService) CancelNegotiation(ctx context.Context, routing int64, id string) error {
	n, err := s.repo.GetNegotiationByID(ctx, routing, id)
	if err != nil {
		return err
	}
	if n == nil {
		return domain.ErrInterbankNotFound
	}
	n.IsOngoing = false
	n.Status = "CANCELLED"
	return s.repo.UpdateNegotiation(ctx, n)
}

// suppress strconv unused
var _ = strconv.Itoa

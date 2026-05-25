package service

import (
	"context"
	"errors"
	"fmt"

	"banka-backend/services/bank-service/internal/domain"
)

// PriceAlertService handles business logic for price alert management.
type PriceAlertService struct {
	repo    domain.PriceAlertRepository
	listing domain.ListingService
}

func NewPriceAlertService(repo domain.PriceAlertRepository, listing domain.ListingService) *PriceAlertService {
	return &PriceAlertService{repo: repo, listing: listing}
}

type CreatePriceAlertRequest struct {
	UserID    int64
	ListingID int64
	Threshold float64
	Direction domain.PriceAlertDirection
	Email     string
}

func (s *PriceAlertService) CreateAlert(ctx context.Context, req CreatePriceAlertRequest) (domain.PriceAlert, error) {
	if req.Threshold <= 0 {
		return domain.PriceAlert{}, errors.New("prag cene mora biti pozitivan broj")
	}
	if req.Direction != domain.PriceAlertAbove && req.Direction != domain.PriceAlertBelow {
		return domain.PriceAlert{}, fmt.Errorf("nevalidan smer alarma: %q (dozvoljeno: ABOVE, BELOW)", req.Direction)
	}
	if req.Email == "" {
		return domain.PriceAlert{}, errors.New("email je obavezan")
	}

	// Fetch ticker from listing for denormalization.
	listing, err := s.listing.GetListingByID(ctx, req.ListingID)
	if err != nil {
		return domain.PriceAlert{}, fmt.Errorf("hartija nije pronađena: %w", err)
	}

	return s.repo.Create(domain.PriceAlert{
		UserID:    req.UserID,
		ListingID: req.ListingID,
		Ticker:    listing.Ticker,
		Threshold: req.Threshold,
		Direction: req.Direction,
		Email:     req.Email,
	})
}

func (s *PriceAlertService) ListAlerts(userID int64) ([]domain.PriceAlert, error) {
	return s.repo.ListByUser(userID)
}

func (s *PriceAlertService) DeleteAlert(id int64, userID int64) error {
	return s.repo.Delete(id, userID)
}

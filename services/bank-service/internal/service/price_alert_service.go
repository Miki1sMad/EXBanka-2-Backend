package service

import (
	"context"
	"errors"
	"fmt"

	"banka-backend/services/bank-service/internal/domain"
)

// PriceCheckPublisher is called after a new alert is created to trigger an immediate price check.
// PriceAlertWorker satisfies this interface.
type PriceCheckPublisher interface {
	Publish(listingID int64, ask, bid float64)
}

// PriceAlertService handles business logic for price alert management.
type PriceAlertService struct {
	repo      domain.PriceAlertRepository
	listing   domain.ListingService
	publisher PriceCheckPublisher // optional; nil = no immediate check on creation
}

func NewPriceAlertService(repo domain.PriceAlertRepository, listing domain.ListingService, publisher PriceCheckPublisher) *PriceAlertService {
	return &PriceAlertService{repo: repo, listing: listing, publisher: publisher}
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

	alert, err := s.repo.Create(domain.PriceAlert{
		UserID:    req.UserID,
		ListingID: req.ListingID,
		Ticker:    listing.Ticker,
		Threshold: req.Threshold,
		Direction: req.Direction,
		Email:     req.Email,
	})
	if err != nil {
		return domain.PriceAlert{}, err
	}

	// Immediately check if the current price already satisfies the condition.
	// Falls back to Price if Ask/Bid are not populated (seeded without spread).
	if s.publisher != nil {
		ask, bid := listing.Ask, listing.Bid
		if ask <= 0 {
			ask = listing.Price
		}
		if bid <= 0 {
			bid = listing.Price
		}
		if ask > 0 && bid > 0 {
			s.publisher.Publish(listing.ID, ask, bid)
		}
	}

	return alert, nil
}

func (s *PriceAlertService) ListAlerts(userID int64) ([]domain.PriceAlert, error) {
	return s.repo.ListByUser(userID)
}

func (s *PriceAlertService) DeleteAlert(id int64, userID int64) error {
	return s.repo.Delete(id, userID)
}

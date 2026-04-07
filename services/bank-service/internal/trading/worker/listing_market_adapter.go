package worker

import (
	"context"

	"banka-backend/services/bank-service/internal/domain"
)

// listingMarketDataProvider adapts domain.ListingRepository to the
// MarketDataProvider interface needed by the trading engine.
type listingMarketDataProvider struct {
	repo domain.ListingRepository
}

// NewListingMarketDataProvider creates a MarketDataProvider backed by the
// listing repository.  Settlement-date expiry is not yet supported (returns
// nil for all listing types), so FUTURE/OPTION orders will not be auto-declined
// by the engine based on settlement date.
func NewListingMarketDataProvider(repo domain.ListingRepository) MarketDataProvider {
	return &listingMarketDataProvider{repo: repo}
}

// GetMarketSnapshot returns Ask, Bid, and Volume for the requested listing.
func (p *listingMarketDataProvider) GetMarketSnapshot(ctx context.Context, listingID int64) (MarketSnapshot, error) {
	listing, err := p.repo.GetByID(ctx, listingID)
	if err != nil {
		return MarketSnapshot{}, err
	}
	return MarketSnapshot{
		Ask:            listing.Ask,
		Bid:            listing.Bid,
		Volume:         listing.Volume,
		SettlementDate: nil, // settlement-date expiry not yet implemented
	}, nil
}

package service

import (
	"context"
	"errors"
	"testing"

	"banka-backend/services/bank-service/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ─── Mocks ────────────────────────────────────────────────────────────────────

type mockPriceAlertRepo struct{ mock.Mock }

func (m *mockPriceAlertRepo) Create(alert domain.PriceAlert) (domain.PriceAlert, error) {
	args := m.Called(alert)
	return args.Get(0).(domain.PriceAlert), args.Error(1)
}
func (m *mockPriceAlertRepo) ListByUser(userID int64) ([]domain.PriceAlert, error) {
	args := m.Called(userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.PriceAlert), args.Error(1)
}
func (m *mockPriceAlertRepo) Delete(id int64, userID int64) error {
	return m.Called(id, userID).Error(0)
}
func (m *mockPriceAlertRepo) ListActiveForListing(listingID int64) ([]domain.PriceAlert, error) {
	args := m.Called(listingID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.PriceAlert), args.Error(1)
}
func (m *mockPriceAlertRepo) Deactivate(id int64) error {
	return m.Called(id).Error(0)
}

type mockPricePublisher struct{ mock.Mock }

func (m *mockPricePublisher) Publish(listingID int64, ask, bid float64) {
	m.Called(listingID, ask, bid)
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func validListing() *domain.ListingCalculated {
	return &domain.ListingCalculated{
		Listing: domain.Listing{
			ID:     5,
			Ticker: "AAPL",
			Price:  150.0,
			Ask:    151.0,
			Bid:    149.0,
		},
	}
}

func validAlertReq() CreatePriceAlertRequest {
	return CreatePriceAlertRequest{
		UserID:    1,
		ListingID: 5,
		Threshold: 200.0,
		Direction: domain.PriceAlertAbove,
		Email:     "test@example.com",
	}
}

// ─── CreateAlert ──────────────────────────────────────────────────────────────

func TestCreateAlert_NegativeThreshold(t *testing.T) {
	svc := NewPriceAlertService(&mockPriceAlertRepo{}, &mockListingServiceIF{}, nil)
	req := validAlertReq()
	req.Threshold = -1

	_, err := svc.CreateAlert(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pozitivan")
}

func TestCreateAlert_ZeroThreshold(t *testing.T) {
	svc := NewPriceAlertService(&mockPriceAlertRepo{}, &mockListingServiceIF{}, nil)
	req := validAlertReq()
	req.Threshold = 0

	_, err := svc.CreateAlert(context.Background(), req)
	require.Error(t, err)
}

func TestCreateAlert_InvalidDirection(t *testing.T) {
	svc := NewPriceAlertService(&mockPriceAlertRepo{}, &mockListingServiceIF{}, nil)
	req := validAlertReq()
	req.Direction = "SIDEWAYS"

	_, err := svc.CreateAlert(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nevalidan smer")
}

func TestCreateAlert_EmptyEmail(t *testing.T) {
	svc := NewPriceAlertService(&mockPriceAlertRepo{}, &mockListingServiceIF{}, nil)
	req := validAlertReq()
	req.Email = ""

	_, err := svc.CreateAlert(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "email")
}

func TestCreateAlert_ListingNotFound(t *testing.T) {
	ls := &mockListingServiceIF{}
	ls.On("GetListingByID", mock.Anything, int64(5)).Return(nil, errors.New("not found"))
	svc := NewPriceAlertService(&mockPriceAlertRepo{}, ls, nil)

	_, err := svc.CreateAlert(context.Background(), validAlertReq())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hartija")
	ls.AssertExpectations(t)
}

func TestCreateAlert_RepoError(t *testing.T) {
	ls := &mockListingServiceIF{}
	ls.On("GetListingByID", mock.Anything, int64(5)).Return(validListing(), nil)
	repo := &mockPriceAlertRepo{}
	repo.On("Create", mock.AnythingOfType("domain.PriceAlert")).Return(domain.PriceAlert{}, errors.New("db error"))
	svc := NewPriceAlertService(repo, ls, nil)

	_, err := svc.CreateAlert(context.Background(), validAlertReq())
	require.Error(t, err)
	repo.AssertExpectations(t)
}

func TestCreateAlert_OK_NilPublisher(t *testing.T) {
	ls := &mockListingServiceIF{}
	ls.On("GetListingByID", mock.Anything, int64(5)).Return(validListing(), nil)
	repo := &mockPriceAlertRepo{}
	expected := domain.PriceAlert{ID: 1, UserID: 1, ListingID: 5, Ticker: "AAPL", Threshold: 200.0, Direction: domain.PriceAlertAbove, Email: "test@example.com"}
	repo.On("Create", mock.AnythingOfType("domain.PriceAlert")).Return(expected, nil)
	svc := NewPriceAlertService(repo, ls, nil)

	got, err := svc.CreateAlert(context.Background(), validAlertReq())
	require.NoError(t, err)
	assert.Equal(t, expected.ID, got.ID)
	assert.Equal(t, "AAPL", got.Ticker)
}

func TestCreateAlert_OK_PublisherCalled(t *testing.T) {
	ls := &mockListingServiceIF{}
	ls.On("GetListingByID", mock.Anything, int64(5)).Return(validListing(), nil)
	repo := &mockPriceAlertRepo{}
	repo.On("Create", mock.AnythingOfType("domain.PriceAlert")).Return(domain.PriceAlert{ID: 2}, nil)
	pub := &mockPricePublisher{}
	pub.On("Publish", int64(5), 151.0, 149.0).Once()
	svc := NewPriceAlertService(repo, ls, pub)

	_, err := svc.CreateAlert(context.Background(), validAlertReq())
	require.NoError(t, err)
	pub.AssertExpectations(t)
}

func TestCreateAlert_OK_PublisherFallbackToPrice(t *testing.T) {
	listing := validListing()
	listing.Ask = 0
	listing.Bid = 0
	listing.Price = 150.0

	ls := &mockListingServiceIF{}
	ls.On("GetListingByID", mock.Anything, int64(5)).Return(listing, nil)
	repo := &mockPriceAlertRepo{}
	repo.On("Create", mock.AnythingOfType("domain.PriceAlert")).Return(domain.PriceAlert{ID: 3}, nil)
	pub := &mockPricePublisher{}
	pub.On("Publish", int64(5), 150.0, 150.0).Once()
	svc := NewPriceAlertService(repo, ls, pub)

	_, err := svc.CreateAlert(context.Background(), validAlertReq())
	require.NoError(t, err)
	pub.AssertExpectations(t)
}

func TestCreateAlert_BelowDirection_OK(t *testing.T) {
	ls := &mockListingServiceIF{}
	ls.On("GetListingByID", mock.Anything, int64(5)).Return(validListing(), nil)
	repo := &mockPriceAlertRepo{}
	repo.On("Create", mock.AnythingOfType("domain.PriceAlert")).Return(domain.PriceAlert{ID: 4}, nil)
	svc := NewPriceAlertService(repo, ls, nil)

	req := validAlertReq()
	req.Direction = domain.PriceAlertBelow
	_, err := svc.CreateAlert(context.Background(), req)
	require.NoError(t, err)
}

// ─── ListAlerts ───────────────────────────────────────────────────────────────

func TestListAlerts_OK(t *testing.T) {
	repo := &mockPriceAlertRepo{}
	alerts := []domain.PriceAlert{{ID: 1, UserID: 7}, {ID: 2, UserID: 7}}
	repo.On("ListByUser", int64(7)).Return(alerts, nil)
	svc := NewPriceAlertService(repo, &mockListingServiceIF{}, nil)

	got, err := svc.ListAlerts(7)
	require.NoError(t, err)
	assert.Len(t, got, 2)
	repo.AssertExpectations(t)
}

func TestListAlerts_RepoError(t *testing.T) {
	repo := &mockPriceAlertRepo{}
	repo.On("ListByUser", int64(7)).Return(nil, errors.New("db error"))
	svc := NewPriceAlertService(repo, &mockListingServiceIF{}, nil)

	_, err := svc.ListAlerts(7)
	require.Error(t, err)
}

func TestListAlerts_Empty(t *testing.T) {
	repo := &mockPriceAlertRepo{}
	repo.On("ListByUser", int64(99)).Return([]domain.PriceAlert{}, nil)
	svc := NewPriceAlertService(repo, &mockListingServiceIF{}, nil)

	got, err := svc.ListAlerts(99)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// ─── DeleteAlert ──────────────────────────────────────────────────────────────

func TestDeleteAlert_OK(t *testing.T) {
	repo := &mockPriceAlertRepo{}
	repo.On("Delete", int64(10), int64(7)).Return(nil)
	svc := NewPriceAlertService(repo, &mockListingServiceIF{}, nil)

	err := svc.DeleteAlert(10, 7)
	require.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestDeleteAlert_NotFound(t *testing.T) {
	repo := &mockPriceAlertRepo{}
	repo.On("Delete", int64(99), int64(7)).Return(domain.ErrPriceAlertNotFound)
	svc := NewPriceAlertService(repo, &mockListingServiceIF{}, nil)

	err := svc.DeleteAlert(99, 7)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrPriceAlertNotFound)
}

func TestDeleteAlert_RepoError(t *testing.T) {
	repo := &mockPriceAlertRepo{}
	repo.On("Delete", int64(1), int64(1)).Return(errors.New("db error"))
	svc := NewPriceAlertService(repo, &mockListingServiceIF{}, nil)

	err := svc.DeleteAlert(1, 1)
	require.Error(t, err)
}

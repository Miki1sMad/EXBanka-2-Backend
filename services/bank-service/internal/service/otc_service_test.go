package service

// White-box tests for otc_service.go.
// Uses package service to access validateOfferFields and newGormDB.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"banka-backend/services/bank-service/internal/domain"
)

// ─── Mock OTCRepository ───────────────────────────────────────────────────────

type mockOTCRepo struct{ mock.Mock }

func (m *mockOTCRepo) WithTx(tx interface{}) domain.OTCRepository { return m }
func (m *mockOTCRepo) CreateOffer(ctx context.Context, offer domain.OTCOffer) (*domain.OTCOffer, error) {
	args := m.Called(ctx, offer)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.OTCOffer), args.Error(1)
}
func (m *mockOTCRepo) GetOfferByID(ctx context.Context, id int64) (*domain.OTCOffer, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.OTCOffer), args.Error(1)
}
func (m *mockOTCRepo) GetOfferByIDForUpdate(ctx context.Context, id int64) (*domain.OTCOffer, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.OTCOffer), args.Error(1)
}
func (m *mockOTCRepo) UpdateOfferOnCounter(ctx context.Context, offer domain.OTCOffer) error {
	return m.Called(ctx, offer).Error(0)
}
func (m *mockOTCRepo) UpdateOfferStatus(ctx context.Context, id int64, status domain.OTCOfferStatus, modifiedBy int64) error {
	return m.Called(ctx, id, status, modifiedBy).Error(0)
}
func (m *mockOTCRepo) AvailablePublicShares(ctx context.Context, sellerID, listingID, excludeOfferID int64) (int32, error) {
	args := m.Called(ctx, sellerID, listingID, excludeOfferID)
	return args.Get(0).(int32), args.Error(1)
}
func (m *mockOTCRepo) ListOffers(ctx context.Context, filter domain.ListOTCOffersFilter) ([]domain.OTCOfferListItem, error) {
	args := m.Called(ctx, filter)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.OTCOfferListItem), args.Error(1)
}
func (m *mockOTCRepo) GetOfferListItem(ctx context.Context, id int64, callerID int64) (*domain.OTCOfferListItem, error) {
	args := m.Called(ctx, id, callerID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.OTCOfferListItem), args.Error(1)
}
func (m *mockOTCRepo) ListMarketplace(ctx context.Context, callerID int64) ([]domain.OTCMarketplaceItem, error) {
	args := m.Called(ctx, callerID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.OTCMarketplaceItem), args.Error(1)
}
func (m *mockOTCRepo) CreateContract(ctx context.Context, contract domain.OTCContract) (*domain.OTCContract, error) {
	args := m.Called(ctx, contract)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.OTCContract), args.Error(1)
}
func (m *mockOTCRepo) GetAccountInfo(ctx context.Context, accountID int64) (int64, string, error) {
	args := m.Called(ctx, accountID)
	return args.Get(0).(int64), args.String(1), args.Error(2)
}
func (m *mockOTCRepo) GetListingCurrency(ctx context.Context, listingID int64) (string, error) {
	args := m.Called(ctx, listingID)
	return args.String(0), args.Error(1)
}
func (m *mockOTCRepo) ListContracts(ctx context.Context, userID int64) ([]domain.OTCContract, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.OTCContract), args.Error(1)
}
func (m *mockOTCRepo) GetContractByID(ctx context.Context, id int64) (*domain.OTCContract, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.OTCContract), args.Error(1)
}
func (m *mockOTCRepo) GetContractByIDForUpdate(ctx context.Context, id int64) (*domain.OTCContract, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.OTCContract), args.Error(1)
}
func (m *mockOTCRepo) UpdateContractStatus(ctx context.Context, id int64, status domain.OTCContractStatus) error {
	return m.Called(ctx, id, status).Error(0)
}
func (m *mockOTCRepo) ExpireOverdueContracts(ctx context.Context) (int, error) {
	args := m.Called(ctx)
	return args.Int(0), args.Error(1)
}

// ─── Mock OTCPaymentPort ──────────────────────────────────────────────────────

type mockOTCPaymentPort struct{ mock.Mock }

func (m *mockOTCPaymentPort) ExecuteOTCPremiumTransfer(ctx context.Context, tx interface{}, in domain.OTCPremiumTransferInput) error {
	return m.Called(ctx, tx, in).Error(0)
}

// ─── validateOfferFields ──────────────────────────────────────────────────────

func TestValidateOfferFields_OK(t *testing.T) {
	future := time.Now().UTC().Add(24 * time.Hour)
	err := validateOfferFields(10, 100.0, 5.0, future)
	require.NoError(t, err)
}

func TestValidateOfferFields_ZeroAmount(t *testing.T) {
	future := time.Now().UTC().Add(24 * time.Hour)
	err := validateOfferFields(0, 100.0, 5.0, future)
	require.ErrorIs(t, err, domain.ErrOTCInvalidInput)
}

func TestValidateOfferFields_NegativeAmount(t *testing.T) {
	future := time.Now().UTC().Add(24 * time.Hour)
	err := validateOfferFields(-1, 100.0, 5.0, future)
	require.ErrorIs(t, err, domain.ErrOTCInvalidInput)
}

func TestValidateOfferFields_ZeroPrice(t *testing.T) {
	future := time.Now().UTC().Add(24 * time.Hour)
	err := validateOfferFields(10, 0, 5.0, future)
	require.ErrorIs(t, err, domain.ErrOTCInvalidInput)
}

func TestValidateOfferFields_NegativePremium(t *testing.T) {
	future := time.Now().UTC().Add(24 * time.Hour)
	err := validateOfferFields(10, 100.0, -1.0, future)
	require.ErrorIs(t, err, domain.ErrOTCInvalidInput)
}

func TestValidateOfferFields_PastDate(t *testing.T) {
	past := time.Now().UTC().Add(-48 * time.Hour)
	err := validateOfferFields(10, 100.0, 5.0, past)
	require.ErrorIs(t, err, domain.ErrOTCInvalidInput)
}

func TestValidateOfferFields_ZeroDate_OK(t *testing.T) {
	err := validateOfferFields(10, 100.0, 0.0, time.Time{})
	require.NoError(t, err)
}

// ─── ListOffers / GetOffer / ListMarketplace ──────────────────────────────────

func TestOTCService_ListOffers(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockOTCRepo{}
	svc := NewOTCService(db, repo, &mockOTCPaymentPort{})
	ctx := context.Background()

	filter := domain.ListOTCOffersFilter{UserID: 5}
	repo.On("ListOffers", ctx, filter).Return([]domain.OTCOfferListItem{{OTCOffer: domain.OTCOffer{ID: 1}}}, nil)

	items, err := svc.ListOffers(ctx, filter)
	require.NoError(t, err)
	assert.Len(t, items, 1)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestOTCService_GetOffer(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockOTCRepo{}
	svc := NewOTCService(db, repo, &mockOTCPaymentPort{})
	ctx := context.Background()

	expected := &domain.OTCOfferListItem{OTCOffer: domain.OTCOffer{ID: 7}}
	repo.On("GetOfferListItem", ctx, int64(7), int64(3)).Return(expected, nil)

	got, err := svc.GetOffer(ctx, 7, 3)
	require.NoError(t, err)
	assert.Equal(t, int64(7), got.ID)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestOTCService_ListMarketplace(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockOTCRepo{}
	svc := NewOTCService(db, repo, &mockOTCPaymentPort{})
	ctx := context.Background()

	repo.On("ListMarketplace", ctx, int64(5)).Return([]domain.OTCMarketplaceItem{{ListingID: 1}}, nil)

	items, err := svc.ListMarketplace(ctx, 5)
	require.NoError(t, err)
	assert.Len(t, items, 1)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

// ─── CreateOffer ──────────────────────────────────────────────────────────────

func TestOTCService_CreateOffer_OK(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockOTCRepo{}
	svc := NewOTCService(db, repo, &mockOTCPaymentPort{})
	ctx := context.Background()

	dbMock.ExpectBegin()
	dbMock.ExpectCommit()

	repo.On("AvailablePublicShares", ctx, int64(2), int64(10), int64(0)).Return(int32(100), nil)
	repo.On("CreateOffer", ctx, mock.AnythingOfType("domain.OTCOffer")).Return(&domain.OTCOffer{ID: 1}, nil)

	in := domain.CreateOTCOfferInput{
		ListingID:      10,
		BuyerID:        1,
		SellerID:       2,
		BuyerAccountID: 99,
		Amount:         5,
		PricePerStock:  50.0,
		Premium:        2.0,
		SettlementDate: time.Now().Add(24 * time.Hour),
	}
	offer, err := svc.CreateOffer(ctx, in)
	require.NoError(t, err)
	assert.Equal(t, int64(1), offer.ID)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestOTCService_CreateOffer_SelfTrade(t *testing.T) {
	db, _ := newGormDB(t)
	repo := &mockOTCRepo{}
	svc := NewOTCService(db, repo, &mockOTCPaymentPort{})
	ctx := context.Background()

	in := domain.CreateOTCOfferInput{
		ListingID: 10, BuyerID: 5, SellerID: 5,
		Amount: 1, PricePerStock: 10, Premium: 1,
		SettlementDate: time.Now().Add(24 * time.Hour),
	}
	_, err := svc.CreateOffer(ctx, in)
	require.ErrorIs(t, err, domain.ErrOTCSelfTrade)
}

func TestOTCService_CreateOffer_InvalidFields(t *testing.T) {
	db, _ := newGormDB(t)
	svc := NewOTCService(db, &mockOTCRepo{}, &mockOTCPaymentPort{})
	ctx := context.Background()

	in := domain.CreateOTCOfferInput{
		ListingID: 10, BuyerID: 1, SellerID: 2,
		Amount: 0, PricePerStock: 50, Premium: 1,
		SettlementDate: time.Now().Add(24 * time.Hour),
	}
	_, err := svc.CreateOffer(ctx, in)
	require.ErrorIs(t, err, domain.ErrOTCInvalidInput)
}

func TestOTCService_CreateOffer_InsufficientCapacity(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockOTCRepo{}
	svc := NewOTCService(db, repo, &mockOTCPaymentPort{})
	ctx := context.Background()

	dbMock.ExpectBegin()
	dbMock.ExpectRollback()

	repo.On("AvailablePublicShares", ctx, int64(2), int64(10), int64(0)).Return(int32(2), nil)

	in := domain.CreateOTCOfferInput{
		ListingID: 10, BuyerID: 1, SellerID: 2,
		Amount: 10, PricePerStock: 50, Premium: 1,
		SettlementDate: time.Now().Add(24 * time.Hour),
	}
	_, err := svc.CreateOffer(ctx, in)
	require.ErrorIs(t, err, domain.ErrOTCInsufficientCapacity)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

// ─── DeclineOffer ─────────────────────────────────────────────────────────────

func TestOTCService_DeclineOffer_Rejected(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockOTCRepo{}
	svc := NewOTCService(db, repo, &mockOTCPaymentPort{})
	ctx := context.Background()

	dbMock.ExpectBegin()
	dbMock.ExpectCommit()

	offer := &domain.OTCOffer{
		ID: 1, BuyerID: 1, SellerID: 2,
		ModifiedBy: 1, // buyer modified last
		Status:     domain.OTCOfferPending,
	}
	repo.On("GetOfferByIDForUpdate", ctx, int64(1)).Return(offer, nil)
	// callerID=2 (seller) declines — different from modified_by=1 → REJECTED
	repo.On("UpdateOfferStatus", ctx, int64(1), domain.OTCOfferRejected, int64(2)).Return(nil)

	got, err := svc.DeclineOffer(ctx, 1, 2)
	require.NoError(t, err)
	assert.Equal(t, domain.OTCOfferRejected, got.Status)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestOTCService_DeclineOffer_Deactivated(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockOTCRepo{}
	svc := NewOTCService(db, repo, &mockOTCPaymentPort{})
	ctx := context.Background()

	dbMock.ExpectBegin()
	dbMock.ExpectCommit()

	offer := &domain.OTCOffer{
		ID: 2, BuyerID: 1, SellerID: 2,
		ModifiedBy: 1, // buyer modified last
		Status:     domain.OTCOfferPending,
	}
	repo.On("GetOfferByIDForUpdate", ctx, int64(2)).Return(offer, nil)
	// callerID=1 (buyer who sent offer) pulls it back → DEACTIVATED
	repo.On("UpdateOfferStatus", ctx, int64(2), domain.OTCOfferDeactivated, int64(1)).Return(nil)

	got, err := svc.DeclineOffer(ctx, 2, 1)
	require.NoError(t, err)
	assert.Equal(t, domain.OTCOfferDeactivated, got.Status)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestOTCService_DeclineOffer_NotParticipant(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockOTCRepo{}
	svc := NewOTCService(db, repo, &mockOTCPaymentPort{})
	ctx := context.Background()

	dbMock.ExpectBegin()
	dbMock.ExpectRollback()

	offer := &domain.OTCOffer{
		ID: 1, BuyerID: 1, SellerID: 2,
		ModifiedBy: 1, Status: domain.OTCOfferPending,
	}
	repo.On("GetOfferByIDForUpdate", ctx, int64(1)).Return(offer, nil)

	_, err := svc.DeclineOffer(ctx, 1, 99) // stranger
	require.ErrorIs(t, err, domain.ErrOTCNotParticipant)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestOTCService_DeclineOffer_InvalidStatus(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockOTCRepo{}
	svc := NewOTCService(db, repo, &mockOTCPaymentPort{})
	ctx := context.Background()

	dbMock.ExpectBegin()
	dbMock.ExpectRollback()

	offer := &domain.OTCOffer{
		ID: 1, BuyerID: 1, SellerID: 2,
		ModifiedBy: 1, Status: domain.OTCOfferAccepted,
	}
	repo.On("GetOfferByIDForUpdate", ctx, int64(1)).Return(offer, nil)

	_, err := svc.DeclineOffer(ctx, 1, 2)
	require.ErrorIs(t, err, domain.ErrOTCInvalidStatus)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

// ─── CounterOffer ─────────────────────────────────────────────────────────────

func TestOTCService_CounterOffer_OK(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockOTCRepo{}
	svc := NewOTCService(db, repo, &mockOTCPaymentPort{})
	ctx := context.Background()

	dbMock.ExpectBegin()
	dbMock.ExpectCommit()

	sellerAcc := int64(55)
	offer := &domain.OTCOffer{
		ID: 10, BuyerID: 1, SellerID: 2,
		ModifiedBy: 1, // buyer sent last — seller's turn
		ListingID:  20,
		Status:     domain.OTCOfferPending,
	}
	repo.On("GetOfferByIDForUpdate", ctx, int64(10)).Return(offer, nil)
	repo.On("AvailablePublicShares", ctx, int64(2), int64(20), int64(10)).Return(int32(50), nil)
	repo.On("UpdateOfferOnCounter", ctx, mock.AnythingOfType("domain.OTCOffer")).Return(nil)
	repo.On("GetOfferByID", ctx, int64(10)).Return(&domain.OTCOffer{ID: 10, SellerAccountID: &sellerAcc}, nil)

	in := domain.CounterOTCOfferInput{
		OfferID: 10, CallerID: 2, // seller counters
		Amount: 5, PricePerStock: 60, Premium: 3,
		SettlementDate:  time.Now().Add(24 * time.Hour),
		SellerAccountID: &sellerAcc,
	}
	updated, err := svc.CounterOffer(ctx, in)
	require.NoError(t, err)
	assert.Equal(t, int64(10), updated.ID)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestOTCService_CounterOffer_NotCounterparty(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockOTCRepo{}
	svc := NewOTCService(db, repo, &mockOTCPaymentPort{})
	ctx := context.Background()

	dbMock.ExpectBegin()
	dbMock.ExpectRollback()

	offer := &domain.OTCOffer{
		ID: 10, BuyerID: 1, SellerID: 2,
		ModifiedBy: 2, Status: domain.OTCOfferPending,
	}
	repo.On("GetOfferByIDForUpdate", ctx, int64(10)).Return(offer, nil)

	in := domain.CounterOTCOfferInput{
		OfferID: 10, CallerID: 2, // seller tries to counter their own change
		Amount: 5, PricePerStock: 60, Premium: 3,
		SettlementDate: time.Now().Add(24 * time.Hour),
	}
	_, err := svc.CounterOffer(ctx, in)
	require.ErrorIs(t, err, domain.ErrOTCNotCounterparty)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

// ─── AcceptOffer ──────────────────────────────────────────────────────────────

func TestOTCService_AcceptOffer_OK(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockOTCRepo{}
	payment := &mockOTCPaymentPort{}
	svc := NewOTCService(db, repo, payment)
	ctx := context.Background()

	dbMock.ExpectBegin()
	dbMock.ExpectCommit()

	sellerAcc := int64(55)
	offer := &domain.OTCOffer{
		ID: 20, BuyerID: 1, SellerID: 2, ListingID: 30,
		BuyerAccountID: 44, SellerAccountID: &sellerAcc,
		ModifiedBy: 1, Amount: 5, PricePerStock: 100, Premium: 10,
		SettlementDate: time.Now().Add(48 * time.Hour),
		Status:         domain.OTCOfferPending,
	}
	created := &domain.OTCContract{ID: 1, OfferID: 20}

	repo.On("GetOfferByIDForUpdate", ctx, int64(20)).Return(offer, nil)
	repo.On("AvailablePublicShares", ctx, int64(2), int64(30), int64(20)).Return(int32(10), nil)
	repo.On("GetListingCurrency", ctx, int64(30)).Return("USD", nil)
	repo.On("UpdateOfferStatus", ctx, int64(20), domain.OTCOfferAccepted, int64(2)).Return(nil)
	repo.On("CreateContract", ctx, mock.AnythingOfType("domain.OTCContract")).Return(created, nil)
	payment.On("ExecuteOTCPremiumTransfer", ctx, mock.Anything, mock.AnythingOfType("domain.OTCPremiumTransferInput")).Return(nil)

	in := domain.AcceptOTCOfferInput{OfferID: 20, CallerID: 2} // seller accepts buyer's last offer
	contract, err := svc.AcceptOffer(ctx, in)
	require.NoError(t, err)
	assert.Equal(t, int64(1), contract.ID)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestOTCService_AcceptOffer_SelfAccept(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockOTCRepo{}
	svc := NewOTCService(db, repo, &mockOTCPaymentPort{})
	ctx := context.Background()

	dbMock.ExpectBegin()
	dbMock.ExpectRollback()

	sellerAcc := int64(55)
	offer := &domain.OTCOffer{
		ID: 20, BuyerID: 1, SellerID: 2,
		BuyerAccountID: 44, SellerAccountID: &sellerAcc,
		ModifiedBy: 2, Status: domain.OTCOfferPending,
	}
	repo.On("GetOfferByIDForUpdate", ctx, int64(20)).Return(offer, nil)

	in := domain.AcceptOTCOfferInput{OfferID: 20, CallerID: 2}
	_, err := svc.AcceptOffer(ctx, in)
	require.ErrorIs(t, err, domain.ErrOTCSelfAccept)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestOTCService_AcceptOffer_SellerAccountMissing(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockOTCRepo{}
	svc := NewOTCService(db, repo, &mockOTCPaymentPort{})
	ctx := context.Background()

	dbMock.ExpectBegin()
	dbMock.ExpectRollback()

	offer := &domain.OTCOffer{
		ID: 20, BuyerID: 1, SellerID: 2, ListingID: 30,
		BuyerAccountID: 44, SellerAccountID: nil,
		ModifiedBy: 1, Amount: 5, PricePerStock: 100, Premium: 10,
		Status: domain.OTCOfferPending,
	}
	repo.On("GetOfferByIDForUpdate", ctx, int64(20)).Return(offer, nil)
	repo.On("AvailablePublicShares", ctx, int64(2), int64(30), int64(20)).Return(int32(10), nil)

	in := domain.AcceptOTCOfferInput{OfferID: 20, CallerID: 2}
	_, err := svc.AcceptOffer(ctx, in)
	require.ErrorIs(t, err, domain.ErrOTCSellerAccountMissing)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestOTCService_AcceptOffer_NotParticipant(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockOTCRepo{}
	svc := NewOTCService(db, repo, &mockOTCPaymentPort{})
	ctx := context.Background()

	dbMock.ExpectBegin()
	dbMock.ExpectRollback()

	offer := &domain.OTCOffer{
		ID: 30, BuyerID: 1, SellerID: 2,
		ModifiedBy: 1, Status: domain.OTCOfferPending,
	}
	repo.On("GetOfferByIDForUpdate", ctx, int64(30)).Return(offer, nil)

	in := domain.AcceptOTCOfferInput{OfferID: 30, CallerID: 99} // stranger
	_, err := svc.AcceptOffer(ctx, in)
	require.ErrorIs(t, err, domain.ErrOTCNotParticipant)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestOTCService_AcceptOffer_InvalidStatus(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockOTCRepo{}
	svc := NewOTCService(db, repo, &mockOTCPaymentPort{})
	ctx := context.Background()

	dbMock.ExpectBegin()
	dbMock.ExpectRollback()

	offer := &domain.OTCOffer{
		ID: 31, BuyerID: 1, SellerID: 2,
		ModifiedBy: 1, Status: domain.OTCOfferAccepted,
	}
	repo.On("GetOfferByIDForUpdate", ctx, int64(31)).Return(offer, nil)

	in := domain.AcceptOTCOfferInput{OfferID: 31, CallerID: 2}
	_, err := svc.AcceptOffer(ctx, in)
	require.ErrorIs(t, err, domain.ErrOTCInvalidStatus)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestOTCService_AcceptOffer_SellerAccountPassedByBuyer(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockOTCRepo{}
	svc := NewOTCService(db, repo, &mockOTCPaymentPort{})
	ctx := context.Background()

	dbMock.ExpectBegin()
	dbMock.ExpectRollback()

	sellerAcc := int64(55)
	offer := &domain.OTCOffer{
		ID: 32, BuyerID: 1, SellerID: 2, ListingID: 30,
		BuyerAccountID: 44, SellerAccountID: nil,
		ModifiedBy: 2, // seller modified last — buyer's turn to accept
		Amount:     5, Status: domain.OTCOfferPending,
	}
	repo.On("GetOfferByIDForUpdate", ctx, int64(32)).Return(offer, nil)

	// Buyer tries to set SellerAccountID — only seller may do that
	in := domain.AcceptOTCOfferInput{OfferID: 32, CallerID: 1, SellerAccountID: &sellerAcc}
	_, err := svc.AcceptOffer(ctx, in)
	require.ErrorIs(t, err, domain.ErrOTCNotCounterparty)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestOTCService_AcceptOffer_GetListingCurrencyError(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockOTCRepo{}
	svc := NewOTCService(db, repo, &mockOTCPaymentPort{})
	ctx := context.Background()

	dbMock.ExpectBegin()
	dbMock.ExpectRollback()

	sellerAcc := int64(55)
	offer := &domain.OTCOffer{
		ID: 33, BuyerID: 1, SellerID: 2, ListingID: 40,
		BuyerAccountID: 44, SellerAccountID: &sellerAcc,
		ModifiedBy: 1, Amount: 5, PricePerStock: 100, Premium: 10,
		Status: domain.OTCOfferPending,
	}
	repo.On("GetOfferByIDForUpdate", ctx, int64(33)).Return(offer, nil)
	repo.On("AvailablePublicShares", ctx, int64(2), int64(40), int64(33)).Return(int32(20), nil)
	repo.On("GetListingCurrency", ctx, int64(40)).Return("", assert.AnError)

	in := domain.AcceptOTCOfferInput{OfferID: 33, CallerID: 2}
	_, err := svc.AcceptOffer(ctx, in)
	require.Error(t, err)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestOTCService_AcceptOffer_PremiumTransferError(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockOTCRepo{}
	payment := &mockOTCPaymentPort{}
	svc := NewOTCService(db, repo, payment)
	ctx := context.Background()

	dbMock.ExpectBegin()
	dbMock.ExpectRollback()

	sellerAcc := int64(55)
	offer := &domain.OTCOffer{
		ID: 34, BuyerID: 1, SellerID: 2, ListingID: 50,
		BuyerAccountID: 44, SellerAccountID: &sellerAcc,
		ModifiedBy: 1, Amount: 5, PricePerStock: 100, Premium: 10,
		SettlementDate: time.Now().Add(48 * time.Hour),
		Status:         domain.OTCOfferPending,
	}
	created := &domain.OTCContract{ID: 9, OfferID: 34}

	repo.On("GetOfferByIDForUpdate", ctx, int64(34)).Return(offer, nil)
	repo.On("AvailablePublicShares", ctx, int64(2), int64(50), int64(34)).Return(int32(10), nil)
	repo.On("GetListingCurrency", ctx, int64(50)).Return("USD", nil)
	repo.On("UpdateOfferStatus", ctx, int64(34), domain.OTCOfferAccepted, int64(2)).Return(nil)
	repo.On("CreateContract", ctx, mock.AnythingOfType("domain.OTCContract")).Return(created, nil)
	payment.On("ExecuteOTCPremiumTransfer", ctx, mock.Anything, mock.AnythingOfType("domain.OTCPremiumTransferInput")).
		Return(assert.AnError)

	in := domain.AcceptOTCOfferInput{OfferID: 34, CallerID: 2}
	_, err := svc.AcceptOffer(ctx, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "premije")
	require.NoError(t, dbMock.ExpectationsWereMet())
}

// ─── validateAccountOwnership (white-box) ────────────────────────────────────

func TestValidateAccountOwnership_OK(t *testing.T) {
	db, _ := newGormDB(t)
	repo := &mockOTCRepo{}
	svc := NewOTCService(db, repo, &mockOTCPaymentPort{}).(*otcService)
	ctx := context.Background()

	repo.On("GetAccountInfo", ctx, int64(10)).Return(int64(5), "USD", nil)

	currency, err := svc.validateAccountOwnership(ctx, repo, 10, 5)
	require.NoError(t, err)
	assert.Equal(t, "USD", currency)
}

func TestValidateAccountOwnership_NotOwned(t *testing.T) {
	db, _ := newGormDB(t)
	repo := &mockOTCRepo{}
	svc := NewOTCService(db, repo, &mockOTCPaymentPort{}).(*otcService)
	ctx := context.Background()

	repo.On("GetAccountInfo", ctx, int64(10)).Return(int64(99), "USD", nil)

	_, err := svc.validateAccountOwnership(ctx, repo, 10, 5) // owner 99 ≠ expected 5
	require.ErrorIs(t, err, domain.ErrOTCAccountNotOwned)
}

func TestValidateAccountOwnership_RepoError(t *testing.T) {
	db, _ := newGormDB(t)
	repo := &mockOTCRepo{}
	svc := NewOTCService(db, repo, &mockOTCPaymentPort{}).(*otcService)
	ctx := context.Background()

	repo.On("GetAccountInfo", ctx, int64(10)).Return(int64(0), "", assert.AnError)

	_, err := svc.validateAccountOwnership(ctx, repo, 10, 5)
	require.Error(t, err)
}

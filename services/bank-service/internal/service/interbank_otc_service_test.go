package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"banka-backend/services/bank-service/internal/domain"
	"banka-backend/services/bank-service/internal/service"
)

// ─── Mock InterbankRepository ─────────────────────────────────────────────────

type mockInterbankRepo struct{ mock.Mock }

func (m *mockInterbankRepo) GetIncomingByIdempotence(ctx context.Context, rn int64, lk string) (*domain.InterbankMessageLog, error) {
	args := m.Called(ctx, rn, lk)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.InterbankMessageLog), args.Error(1)
}
func (m *mockInterbankRepo) GetOutgoingByIdempotence(ctx context.Context, rn int64, lk string) (*domain.InterbankMessageLog, error) {
	args := m.Called(ctx, rn, lk)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.InterbankMessageLog), args.Error(1)
}
func (m *mockInterbankRepo) CreateMessage(ctx context.Context, msg *domain.InterbankMessageLog) error {
	return m.Called(ctx, msg).Error(0)
}
func (m *mockInterbankRepo) UpdateMessage(ctx context.Context, msg *domain.InterbankMessageLog) error {
	return m.Called(ctx, msg).Error(0)
}
func (m *mockInterbankRepo) ListPendingOutgoing(ctx context.Context, limit int) ([]domain.InterbankMessageLog, error) {
	args := m.Called(ctx, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.InterbankMessageLog), args.Error(1)
}
func (m *mockInterbankRepo) CreateTransaction(ctx context.Context, t *domain.InterbankTransaction) error {
	return m.Called(ctx, t).Error(0)
}
func (m *mockInterbankRepo) GetTransactionByForeignID(ctx context.Context, rn int64, fid string) (*domain.InterbankTransaction, error) {
	args := m.Called(ctx, rn, fid)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.InterbankTransaction), args.Error(1)
}
func (m *mockInterbankRepo) UpdateTransactionStatus(ctx context.Context, id int64, status domain.InterbankTxStatus, step, reason string) error {
	return m.Called(ctx, id, status, step, reason).Error(0)
}
func (m *mockInterbankRepo) CreateReservation(ctx context.Context, r *domain.InterbankReservation) error {
	return m.Called(ctx, r).Error(0)
}
func (m *mockInterbankRepo) ListReservationsByTx(ctx context.Context, txID int64) ([]domain.InterbankReservation, error) {
	args := m.Called(ctx, txID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.InterbankReservation), args.Error(1)
}
func (m *mockInterbankRepo) CreateNegotiation(ctx context.Context, n *domain.InterbankNegotiation) error {
	return m.Called(ctx, n).Error(0)
}
func (m *mockInterbankRepo) GetNegotiationByID(ctx context.Context, rn int64, fid string) (*domain.InterbankNegotiation, error) {
	args := m.Called(ctx, rn, fid)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.InterbankNegotiation), args.Error(1)
}
func (m *mockInterbankRepo) UpdateNegotiation(ctx context.Context, n *domain.InterbankNegotiation) error {
	return m.Called(ctx, n).Error(0)
}
func (m *mockInterbankRepo) ListPublicStocks(ctx context.Context) ([]domain.PublicStock, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.PublicStock), args.Error(1)
}
func (m *mockInterbankRepo) CreateOptionContract(ctx context.Context, c *domain.InterbankOptionContract) error {
	return m.Called(ctx, c).Error(0)
}
func (m *mockInterbankRepo) GetOptionContract(ctx context.Context, rn int64, fid string) (*domain.InterbankOptionContract, error) {
	args := m.Called(ctx, rn, fid)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.InterbankOptionContract), args.Error(1)
}
func (m *mockInterbankRepo) UpdateOptionContractStatus(ctx context.Context, rn int64, fid, status string, usedAt *time.Time) error {
	return m.Called(ctx, rn, fid, status, usedAt).Error(0)
}
func (m *mockInterbankRepo) ListContractsForUser(ctx context.Context, rn int64, userID string) ([]domain.InterbankOptionContract, error) {
	args := m.Called(ctx, rn, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.InterbankOptionContract), args.Error(1)
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

const ourRouting int64 = 111

func validOffer() domain.OtcOffer {
	return domain.OtcOffer{
		Stock:          domain.StockDescription{Ticker: "AAPL"},
		SettlementDate: time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
		PricePerUnit:   domain.MonetaryValue{Currency: "USD", Amount: decimal.NewFromFloat(150)},
		Premium:        domain.MonetaryValue{Currency: "USD", Amount: decimal.NewFromFloat(5)},
		BuyerID:        domain.ForeignBankId{RoutingNumber: 222, ID: "buyer1"},
		SellerID:       domain.ForeignBankId{RoutingNumber: ourRouting, ID: "seller1"},
		Amount:         10,
		LastModifiedBy: domain.ForeignBankId{RoutingNumber: 222, ID: "buyer1"},
	}
}

// ─── CreateNegotiation ────────────────────────────────────────────────────────

func TestInterbankOTC_CreateNegotiation_OK(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := service.NewInterbankOTCService(repo, ourRouting)

	repo.On("CreateNegotiation", ctx, mock.AnythingOfType("*domain.InterbankNegotiation")).Return(nil)

	id, err := svc.CreateNegotiation(ctx, validOffer())
	require.NoError(t, err)
	assert.Equal(t, ourRouting, id.RoutingNumber)
	assert.NotEmpty(t, id.ID)
	repo.AssertExpectations(t)
}

func TestInterbankOTC_CreateNegotiation_SellerNotOnOurBank(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := service.NewInterbankOTCService(repo, ourRouting)

	offer := validOffer()
	offer.SellerID.RoutingNumber = 999

	_, err := svc.CreateNegotiation(ctx, offer)
	require.Error(t, err)
}

func TestInterbankOTC_CreateNegotiation_ZeroAmount(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := service.NewInterbankOTCService(repo, ourRouting)

	offer := validOffer()
	offer.Amount = 0

	_, err := svc.CreateNegotiation(ctx, offer)
	require.Error(t, err)
}

func TestInterbankOTC_CreateNegotiation_EmptyTicker(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := service.NewInterbankOTCService(repo, ourRouting)

	offer := validOffer()
	offer.Stock.Ticker = ""

	_, err := svc.CreateNegotiation(ctx, offer)
	require.Error(t, err)
}

func TestInterbankOTC_CreateNegotiation_EmptySettlementDate(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := service.NewInterbankOTCService(repo, ourRouting)

	offer := validOffer()
	offer.SettlementDate = ""

	_, err := svc.CreateNegotiation(ctx, offer)
	require.Error(t, err)
}

func TestInterbankOTC_CreateNegotiation_InvalidDate(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := service.NewInterbankOTCService(repo, ourRouting)

	offer := validOffer()
	offer.SettlementDate = "not-a-date"

	_, err := svc.CreateNegotiation(ctx, offer)
	require.Error(t, err)
}

func TestInterbankOTC_CreateNegotiation_RepoError(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := service.NewInterbankOTCService(repo, ourRouting)

	repo.On("CreateNegotiation", ctx, mock.Anything).Return(errors.New("db"))

	_, err := svc.CreateNegotiation(ctx, validOffer())
	require.Error(t, err)
}

// ─── CounterNegotiation ───────────────────────────────────────────────────────

func existingNegotiation() *domain.InterbankNegotiation {
	return &domain.InterbankNegotiation{
		NegotiationRoutingNumber:  ourRouting,
		NegotiationForeignID:      "neg1",
		StockTicker:               "AAPL",
		SettlementDate:            time.Now().Add(24 * time.Hour),
		PriceCurrency:             "USD",
		PriceAmount:               decimal.NewFromFloat(150),
		PremiumCurrency:           "USD",
		PremiumAmount:             decimal.NewFromFloat(5),
		Amount:                    10,
		BuyerRoutingNumber:        222,
		BuyerID:                   "buyer1",
		SellerRoutingNumber:       ourRouting,
		SellerID:                  "seller1",
		LastModifiedRoutingNumber: 222,
		LastModifiedID:            "buyer1",
		IsOngoing:                 true,
		Status:                    "OPEN",
	}
}

func TestInterbankOTC_CounterNegotiation_OK(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := service.NewInterbankOTCService(repo, ourRouting)

	n := existingNegotiation()
	repo.On("GetNegotiationByID", ctx, ourRouting, "neg1").Return(n, nil)
	repo.On("UpdateNegotiation", ctx, mock.Anything).Return(nil)

	offer := validOffer()
	offer.LastModifiedBy = domain.ForeignBankId{RoutingNumber: ourRouting, ID: "seller1"} // seller counters

	err := svc.CounterNegotiation(ctx, ourRouting, "neg1", offer)
	require.NoError(t, err)
}

func TestInterbankOTC_CounterNegotiation_NotFound(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := service.NewInterbankOTCService(repo, ourRouting)

	repo.On("GetNegotiationByID", ctx, ourRouting, "nope").Return(nil, nil)

	err := svc.CounterNegotiation(ctx, ourRouting, "nope", validOffer())
	require.ErrorIs(t, err, domain.ErrInterbankNotFound)
}

func TestInterbankOTC_CounterNegotiation_Closed(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := service.NewInterbankOTCService(repo, ourRouting)

	n := existingNegotiation()
	n.IsOngoing = false

	repo.On("GetNegotiationByID", ctx, ourRouting, "neg1").Return(n, nil)

	err := svc.CounterNegotiation(ctx, ourRouting, "neg1", validOffer())
	require.ErrorIs(t, err, domain.ErrInterbankConflict)
}

func TestInterbankOTC_CounterNegotiation_SamePartyConflict(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := service.NewInterbankOTCService(repo, ourRouting)

	n := existingNegotiation() // lastModified = buyer1
	repo.On("GetNegotiationByID", ctx, ourRouting, "neg1").Return(n, nil)

	offer := validOffer()
	offer.LastModifiedBy = domain.ForeignBankId{RoutingNumber: 222, ID: "buyer1"} // same as n.LastModified

	err := svc.CounterNegotiation(ctx, ourRouting, "neg1", offer)
	require.ErrorIs(t, err, domain.ErrInterbankConflict)
}

func TestInterbankOTC_CounterNegotiation_InvalidDate(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := service.NewInterbankOTCService(repo, ourRouting)

	n := existingNegotiation()
	repo.On("GetNegotiationByID", ctx, ourRouting, "neg1").Return(n, nil)

	offer := validOffer()
	offer.LastModifiedBy = domain.ForeignBankId{RoutingNumber: ourRouting, ID: "seller1"}
	offer.SettlementDate = "bad-date"

	err := svc.CounterNegotiation(ctx, ourRouting, "neg1", offer)
	require.Error(t, err)
}

// ─── GetNegotiation ───────────────────────────────────────────────────────────

func TestInterbankOTC_GetNegotiation_OK(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := service.NewInterbankOTCService(repo, ourRouting)

	n := existingNegotiation()
	repo.On("GetNegotiationByID", ctx, ourRouting, "neg1").Return(n, nil)

	got, err := svc.GetNegotiation(ctx, ourRouting, "neg1")
	require.NoError(t, err)
	assert.Equal(t, "AAPL", got.Stock.Ticker)
	assert.True(t, got.IsOngoing)
}

func TestInterbankOTC_GetNegotiation_NotFound(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := service.NewInterbankOTCService(repo, ourRouting)

	repo.On("GetNegotiationByID", ctx, ourRouting, "x").Return(nil, nil)

	_, err := svc.GetNegotiation(ctx, ourRouting, "x")
	require.ErrorIs(t, err, domain.ErrInterbankNotFound)
}

func TestInterbankOTC_GetNegotiation_RepoError(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := service.NewInterbankOTCService(repo, ourRouting)

	repo.On("GetNegotiationByID", ctx, ourRouting, "x").Return(nil, errors.New("db"))

	_, err := svc.GetNegotiation(ctx, ourRouting, "x")
	require.Error(t, err)
}

// ─── CancelNegotiation ────────────────────────────────────────────────────────

func TestInterbankOTC_CancelNegotiation_OK(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := service.NewInterbankOTCService(repo, ourRouting)

	n := existingNegotiation()
	repo.On("GetNegotiationByID", ctx, ourRouting, "neg1").Return(n, nil)
	repo.On("UpdateNegotiation", ctx, mock.MatchedBy(func(n *domain.InterbankNegotiation) bool {
		return !n.IsOngoing && n.Status == "CANCELLED"
	})).Return(nil)

	err := svc.CancelNegotiation(ctx, ourRouting, "neg1")
	require.NoError(t, err)
}

func TestInterbankOTC_CancelNegotiation_NotFound(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := service.NewInterbankOTCService(repo, ourRouting)

	repo.On("GetNegotiationByID", ctx, ourRouting, "x").Return(nil, nil)

	err := svc.CancelNegotiation(ctx, ourRouting, "x")
	require.ErrorIs(t, err, domain.ErrInterbankNotFound)
}

func TestInterbankOTC_CancelNegotiation_RepoError(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := service.NewInterbankOTCService(repo, ourRouting)

	repo.On("GetNegotiationByID", ctx, ourRouting, "x").Return(nil, errors.New("db"))

	err := svc.CancelNegotiation(ctx, ourRouting, "x")
	require.Error(t, err)
}

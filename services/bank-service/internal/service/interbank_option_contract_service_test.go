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

// ─── Helpers ──────────────────────────────────────────────────────────────────

func newOptionContractService(repo domain.InterbankRepository) *service.InterbankOptionContractService {
	return service.NewInterbankOptionContractService(repo, nil, ourRouting)
}

func activeNegotiation() *domain.InterbankNegotiation {
	return &domain.InterbankNegotiation{
		NegotiationRoutingNumber:  ourRouting,
		NegotiationForeignID:      "neg1",
		StockTicker:               "AAPL",
		SettlementDate:            time.Now().Add(24 * time.Hour),
		PriceCurrency:             "USD",
		PriceAmount:               decimal.NewFromFloat(100),
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

// ─── AcceptNegotiation ────────────────────────────────────────────────────────

func TestAcceptNegotiation_NotFound(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	repo.On("GetNegotiationByID", ctx, ourRouting, "x").Return(nil, nil)

	err := svc.AcceptNegotiation(ctx, ourRouting, "x")
	require.ErrorIs(t, err, domain.ErrInterbankNotFound)
}

func TestAcceptNegotiation_RepoError(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	repo.On("GetNegotiationByID", ctx, ourRouting, "x").Return(nil, errors.New("db"))

	err := svc.AcceptNegotiation(ctx, ourRouting, "x")
	require.Error(t, err)
}

func TestAcceptNegotiation_Conflict_NotOngoing(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	n := activeNegotiation()
	n.IsOngoing = false
	n.Status = "CANCELLED"
	repo.On("GetNegotiationByID", ctx, ourRouting, "neg1").Return(n, nil)

	err := svc.AcceptNegotiation(ctx, ourRouting, "neg1")
	require.ErrorIs(t, err, domain.ErrInterbankConflict)
}

func TestAcceptNegotiation_AlreadyAccepted_OK(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	n := activeNegotiation()
	n.IsOngoing = false
	n.Status = "ACCEPTED"
	repo.On("GetNegotiationByID", ctx, ourRouting, "neg1").Return(n, nil)

	err := svc.AcceptNegotiation(ctx, ourRouting, "neg1")
	require.NoError(t, err)
}

func TestAcceptNegotiation_IntraBankOK(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	// Use a service where buyer is on the same bank as ours — no inter-bank premium needed
	svc := service.NewInterbankOptionContractService(repo, nil, ourRouting)

	n := activeNegotiation()
	n.BuyerRoutingNumber = ourRouting // intra-bank

	repo.On("GetNegotiationByID", ctx, ourRouting, "neg1").Return(n, nil)
	repo.On("CreateOptionContract", ctx, mock.AnythingOfType("*domain.InterbankOptionContract")).Return(nil)
	repo.On("UpdateNegotiation", ctx, mock.MatchedBy(func(nn *domain.InterbankNegotiation) bool {
		return !nn.IsOngoing && nn.Status == "ACCEPTED"
	})).Return(nil)

	err := svc.AcceptNegotiation(ctx, ourRouting, "neg1")
	require.NoError(t, err)
}

func TestAcceptNegotiation_InterBankOK(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	n := activeNegotiation() // buyer is 222, we are ourRouting → inter-bank
	repo.On("GetNegotiationByID", ctx, ourRouting, "neg1").Return(n, nil)
	repo.On("CreateOptionContract", ctx, mock.Anything).Return(nil)
	repo.On("UpdateNegotiation", ctx, mock.Anything).Return(nil)

	err := svc.AcceptNegotiation(ctx, ourRouting, "neg1")
	require.NoError(t, err)
}

func TestAcceptNegotiation_CreateContractError(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	n := activeNegotiation()
	repo.On("GetNegotiationByID", ctx, ourRouting, "neg1").Return(n, nil)
	repo.On("CreateOptionContract", ctx, mock.Anything).Return(errors.New("db"))

	err := svc.AcceptNegotiation(ctx, ourRouting, "neg1")
	require.Error(t, err)
}

// ─── ListContracts ────────────────────────────────────────────────────────────

func TestListContracts_OK(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	contracts := []domain.InterbankOptionContract{
		{ID: 1, StockTicker: "AAPL"},
		{ID: 2, StockTicker: "GOOG"},
	}
	repo.On("ListContractsForUser", ctx, ourRouting, "42").Return(contracts, nil)

	list, err := svc.ListContracts(ctx, 42)
	require.NoError(t, err)
	assert.Len(t, list, 2)
}

func TestListContracts_Error(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	repo.On("ListContractsForUser", ctx, ourRouting, "42").Return(nil, errors.New("db"))

	_, err := svc.ListContracts(ctx, 42)
	require.Error(t, err)
}

// ─── GetContract ──────────────────────────────────────────────────────────────

func TestGetContract_OK(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	c := &domain.InterbankOptionContract{ID: 5, StockTicker: "TSLA", Status: "ACTIVE"}
	repo.On("GetOptionContract", ctx, ourRouting, "contract1").Return(c, nil)

	got, err := svc.GetContract(ctx, ourRouting, "contract1")
	require.NoError(t, err)
	assert.Equal(t, "TSLA", got.StockTicker)
}

func TestGetContract_NotFound(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	repo.On("GetOptionContract", ctx, ourRouting, "x").Return(nil, nil)

	_, err := svc.GetContract(ctx, ourRouting, "x")
	require.ErrorIs(t, err, domain.ErrInterbankNotFound)
}

func TestGetContract_RepoError(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	repo.On("GetOptionContract", ctx, ourRouting, "x").Return(nil, errors.New("db"))

	_, err := svc.GetContract(ctx, ourRouting, "x")
	require.Error(t, err)
}

// ─── ExerciseContract — validation paths (coordinator is nil, never reached) ──

func activeContract() *domain.InterbankOptionContract {
	return &domain.InterbankOptionContract{
		ID:                       1,
		BuyerRoutingNumber:       ourRouting,
		BuyerID:                  "42",
		Status:                   "ACTIVE",
		SettlementDate:           time.Now().Add(48 * time.Hour),
		NegotiationRoutingNumber: ourRouting,
		NegotiationForeignID:     "neg1",
		StockTicker:              "AAPL",
		Amount:                   10,
		PriceCurrency:            "USD",
		PriceAmount:              decimal.NewFromInt(100),
	}
}

func TestExerciseContract_RepoError(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	repo.On("GetOptionContract", ctx, ourRouting, "c1").Return(nil, errors.New("db"))

	_, err := svc.ExerciseContract(ctx, 42, ourRouting, "c1")
	require.Error(t, err)
}

func TestExerciseContract_NotFound(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	repo.On("GetOptionContract", ctx, ourRouting, "c2").Return(nil, nil)

	_, err := svc.ExerciseContract(ctx, 42, ourRouting, "c2")
	require.ErrorIs(t, err, domain.ErrInterbankNotFound)
}

func TestExerciseContract_BuyerOnOtherBank(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	c := activeContract()
	c.BuyerRoutingNumber = 999 // not our bank
	repo.On("GetOptionContract", ctx, ourRouting, "c3").Return(c, nil)

	_, err := svc.ExerciseContract(ctx, 42, ourRouting, "c3")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kupac")
}

func TestExerciseContract_NotBuyer(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	c := activeContract() // BuyerID = "42"
	repo.On("GetOptionContract", ctx, ourRouting, "c4").Return(c, nil)

	_, err := svc.ExerciseContract(ctx, 99, ourRouting, "c4") // wrong caller
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kupac")
}

func TestExerciseContract_NotActive(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	c := activeContract()
	c.Status = "EXERCISED"
	repo.On("GetOptionContract", ctx, ourRouting, "c5").Return(c, nil)

	_, err := svc.ExerciseContract(ctx, 42, ourRouting, "c5")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ACTIVE")
}

func TestExerciseContract_SettlementDatePassed(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	c := activeContract()
	c.SettlementDate = time.Now().Add(-24 * time.Hour) // past
	repo.On("GetOptionContract", ctx, ourRouting, "c6").Return(c, nil)

	_, err := svc.ExerciseContract(ctx, 42, ourRouting, "c6")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "settlementDate")
}

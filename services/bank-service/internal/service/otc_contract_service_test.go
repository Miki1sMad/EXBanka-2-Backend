package service

// White-box tests for otc_contract_service.go.
// Covers: ListContracts, GetContract, enrichContract, usdToRSDRate/usdToTargetRate,
// ExerciseContract (validation paths before SAGA launch).

import (
	"context"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"banka-backend/services/bank-service/internal/domain"
)

// ─── Mocks for ExchangeService and ListingService ────────────────────────────

type mockExchangeServiceIF struct{ mock.Mock }

func (m *mockExchangeServiceIF) GetRates(ctx context.Context) ([]domain.ExchangeRate, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.ExchangeRate), args.Error(1)
}
func (m *mockExchangeServiceIF) CalculateExchange(ctx context.Context, from, to string, amount float64) (*domain.ExchangeConversionResult, error) {
	args := m.Called(ctx, from, to, amount)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.ExchangeConversionResult), args.Error(1)
}
func (m *mockExchangeServiceIF) ExecuteExchangeTransfer(ctx context.Context, input domain.ExchangeTransferInput) (*domain.ExchangeTransferResult, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.ExchangeTransferResult), args.Error(1)
}

type mockListingServiceIF struct{ mock.Mock }

func (m *mockListingServiceIF) ListListings(ctx context.Context, filter domain.ListingFilter) ([]domain.ListingCalculated, int64, error) {
	args := m.Called(ctx, filter)
	return args.Get(0).([]domain.ListingCalculated), args.Get(1).(int64), args.Error(2)
}
func (m *mockListingServiceIF) GetListingByID(ctx context.Context, id int64) (*domain.ListingCalculated, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.ListingCalculated), args.Error(1)
}
func (m *mockListingServiceIF) GetListingHistory(ctx context.Context, id int64, from, to time.Time) ([]domain.ListingDailyPriceInfo, error) {
	args := m.Called(ctx, id, from, to)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.ListingDailyPriceInfo), args.Error(1)
}

// ─── Mock OTCSagaRepository ───────────────────────────────────────────────────

type mockSagaRepo struct{ mock.Mock }

func (m *mockSagaRepo) CreateExecution(ctx context.Context, contractID, initiatedBy int64, buyerReservedAmount float64) (*domain.OTCSagaExecution, error) {
	args := m.Called(ctx, contractID, initiatedBy, buyerReservedAmount)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.OTCSagaExecution), args.Error(1)
}
func (m *mockSagaRepo) GetExecution(ctx context.Context, id int64) (*domain.OTCSagaExecution, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.OTCSagaExecution), args.Error(1)
}
func (m *mockSagaRepo) GetExecutionByContractID(ctx context.Context, contractID int64) (*domain.OTCSagaExecution, error) {
	args := m.Called(ctx, contractID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.OTCSagaExecution), args.Error(1)
}
func (m *mockSagaRepo) UpdateStep(ctx context.Context, id int64, step domain.OTCSagaStep, status domain.OTCSagaStatus, errMsg string) error {
	return m.Called(ctx, id, step, status, errMsg).Error(0)
}
func (m *mockSagaRepo) IncrementRetry(ctx context.Context, id int64) (int, error) {
	args := m.Called(ctx, id)
	return args.Int(0), args.Error(1)
}
func (m *mockSagaRepo) LogStep(ctx context.Context, executionID int64, step domain.OTCSagaStep, stepStatus domain.OTCSagaStepStatus, errMsg string, attempt int) error {
	return m.Called(ctx, executionID, step, stepStatus, errMsg, attempt).Error(0)
}
func (m *mockSagaRepo) DeleteExecution(ctx context.Context, id int64) error {
	return m.Called(ctx, id).Error(0)
}
func (m *mockSagaRepo) WithTx(tx interface{}) domain.OTCSagaRepository { return m }

// ─── Helper ───────────────────────────────────────────────────────────────────

func newOTCContractSvc(t *testing.T, repo domain.OTCRepository, saga domain.OTCSagaRepository, ls domain.ListingService, es domain.ExchangeService) domain.OTCContractService {
	t.Helper()
	db, _ := newGormDB(t)
	return NewOTCContractService(db, repo, saga, ls, es)
}

func validContract() domain.OTCContract {
	return domain.OTCContract{
		ID:              1,
		OfferID:         10,
		ListingID:       5,
		BuyerID:         1,
		SellerID:        2,
		BuyerAccountID:  44,
		SellerAccountID: 55,
		Amount:          10,
		StrikePrice:     100.0,
		Premium:         20.0,
		SettlementDate:  time.Now().Add(48 * time.Hour),
		Status:          domain.OTCContractValid,
	}
}

// ─── usdToRSDRate / usdToTargetRate ───────────────────────────────────────────

func TestUsdToRSDRate_OK(t *testing.T) {
	es := &mockExchangeServiceIF{}
	ctx := context.Background()
	svc := newOTCContractSvc(t, &mockOTCRepo{}, &mockSagaRepo{}, &mockListingServiceIF{}, es)
	s := svc.(*otcContractService)

	es.On("GetRates", ctx).Return([]domain.ExchangeRate{
		{Oznaka: "USD", Srednji: 110.0},
	}, nil)

	rate := s.usdToRSDRate(ctx)
	assert.InDelta(t, 110.0, rate, 0.01)
}

func TestUsdToRSDRate_ExchangeError_Fallback(t *testing.T) {
	es := &mockExchangeServiceIF{}
	ctx := context.Background()
	svc := newOTCContractSvc(t, &mockOTCRepo{}, &mockSagaRepo{}, &mockListingServiceIF{}, es)
	s := svc.(*otcContractService)

	es.On("GetRates", ctx).Return(nil, errors.New("service unavailable"))

	rate := s.usdToRSDRate(ctx)
	assert.InDelta(t, 1.0, rate, 0.01)
}

func TestUsdToTargetRate_USD_Returns1(t *testing.T) {
	es := &mockExchangeServiceIF{}
	ctx := context.Background()
	svc := newOTCContractSvc(t, &mockOTCRepo{}, &mockSagaRepo{}, &mockListingServiceIF{}, es)
	s := svc.(*otcContractService)

	rate := s.usdToTargetRate(ctx, "USD")
	assert.InDelta(t, 1.0, rate, 0.01)
}

func TestUsdToTargetRate_NoUSDRate_Fallback(t *testing.T) {
	es := &mockExchangeServiceIF{}
	ctx := context.Background()
	svc := newOTCContractSvc(t, &mockOTCRepo{}, &mockSagaRepo{}, &mockListingServiceIF{}, es)
	s := svc.(*otcContractService)

	// Rates without USD entry
	es.On("GetRates", ctx).Return([]domain.ExchangeRate{
		{Oznaka: "EUR", Srednji: 117.0},
	}, nil)

	rate := s.usdToTargetRate(ctx, "RSD")
	assert.InDelta(t, 1.0, rate, 0.01)
}

func TestUsdToTargetRate_ZeroSrednji_Fallback(t *testing.T) {
	es := &mockExchangeServiceIF{}
	ctx := context.Background()
	svc := newOTCContractSvc(t, &mockOTCRepo{}, &mockSagaRepo{}, &mockListingServiceIF{}, es)
	s := svc.(*otcContractService)

	es.On("GetRates", ctx).Return([]domain.ExchangeRate{
		{Oznaka: "USD", Srednji: 0}, // zero srednji is skipped
	}, nil)

	rate := s.usdToTargetRate(ctx, "RSD")
	assert.InDelta(t, 1.0, rate, 0.01)
}

// ─── enrichContract ───────────────────────────────────────────────────────────

func TestEnrichContract_WithListing(t *testing.T) {
	ls := &mockListingServiceIF{}
	ctx := context.Background()
	svc := newOTCContractSvc(t, &mockOTCRepo{}, &mockSagaRepo{}, ls, &mockExchangeServiceIF{})
	s := svc.(*otcContractService)

	c := validContract()
	listing := &domain.ListingCalculated{
		Listing: domain.Listing{ID: 5, Ticker: "AAPL", Name: "Apple Inc.", Price: 150.0},
	}
	ls.On("GetListingByID", ctx, int64(5)).Return(listing, nil)

	item := s.enrichContract(ctx, c, 110.0)
	assert.Equal(t, "AAPL", item.Ticker)
	assert.Equal(t, "Apple Inc.", item.StockName)
	assert.InDelta(t, 150.0, item.CurrentPrice, 0.01)
	// profit = (150 - 100) * 10 - 20 = 480
	assert.InDelta(t, 480.0, item.Profit, 0.01)
}

func TestEnrichContract_ListingError(t *testing.T) {
	ls := &mockListingServiceIF{}
	ctx := context.Background()
	svc := newOTCContractSvc(t, &mockOTCRepo{}, &mockSagaRepo{}, ls, &mockExchangeServiceIF{})
	s := svc.(*otcContractService)

	c := validContract()
	ls.On("GetListingByID", ctx, int64(5)).Return(nil, errors.New("not found"))

	item := s.enrichContract(ctx, c, 110.0)
	// listing error: ticker/price are empty/zero but no panic
	assert.Empty(t, item.Ticker)
	assert.Equal(t, "EXBanka", item.SellerBankName)
}

// ─── ListContracts ────────────────────────────────────────────────────────────

func TestOTCContractSvc_ListContracts_Empty(t *testing.T) {
	repo := &mockOTCRepo{}
	es := &mockExchangeServiceIF{}
	ctx := context.Background()
	svc := newOTCContractSvc(t, repo, &mockSagaRepo{}, &mockListingServiceIF{}, es)

	repo.On("ListContracts", ctx, int64(1)).Return([]domain.OTCContract{}, nil)
	es.On("GetRates", ctx).Return([]domain.ExchangeRate{}, nil)

	items, err := svc.ListContracts(ctx, 1)
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestOTCContractSvc_ListContracts_Error(t *testing.T) {
	repo := &mockOTCRepo{}
	ctx := context.Background()
	svc := newOTCContractSvc(t, repo, &mockSagaRepo{}, &mockListingServiceIF{}, &mockExchangeServiceIF{})

	repo.On("ListContracts", ctx, int64(1)).Return(nil, errors.New("db"))

	_, err := svc.ListContracts(ctx, 1)
	require.Error(t, err)
}

func TestOTCContractSvc_ListContracts_WithItems(t *testing.T) {
	repo := &mockOTCRepo{}
	ls := &mockListingServiceIF{}
	es := &mockExchangeServiceIF{}
	ctx := context.Background()
	svc := newOTCContractSvc(t, repo, &mockSagaRepo{}, ls, es)

	c1 := validContract()
	c1.ID = 1
	c2 := validContract()
	c2.ID = 2

	repo.On("ListContracts", ctx, int64(5)).Return([]domain.OTCContract{c1, c2}, nil)
	es.On("GetRates", ctx).Return([]domain.ExchangeRate{{Oznaka: "USD", Srednji: 110.0}}, nil)
	ls.On("GetListingByID", ctx, int64(5)).Return(&domain.ListingCalculated{
		Listing: domain.Listing{ID: 5, Ticker: "AAPL", Price: 120.0},
	}, nil)

	items, err := svc.ListContracts(ctx, 5)
	require.NoError(t, err)
	assert.Len(t, items, 2)
	assert.Equal(t, "AAPL", items[0].Ticker)
}

// ─── GetContract ──────────────────────────────────────────────────────────────

func TestOTCContractSvc_GetContract_AsBuyer(t *testing.T) {
	repo := &mockOTCRepo{}
	ls := &mockListingServiceIF{}
	es := &mockExchangeServiceIF{}
	ctx := context.Background()
	svc := newOTCContractSvc(t, repo, &mockSagaRepo{}, ls, es)

	c := validContract() // BuyerID=1
	repo.On("GetContractByID", ctx, int64(1)).Return(&c, nil)
	es.On("GetRates", ctx).Return([]domain.ExchangeRate{}, nil)
	ls.On("GetListingByID", ctx, int64(5)).Return(nil, errors.New("not found"))

	item, err := svc.GetContract(ctx, 1, 1) // callerID = buyer
	require.NoError(t, err)
	assert.Equal(t, int64(1), item.ID)
}

func TestOTCContractSvc_GetContract_AsSeller(t *testing.T) {
	repo := &mockOTCRepo{}
	ls := &mockListingServiceIF{}
	es := &mockExchangeServiceIF{}
	ctx := context.Background()
	svc := newOTCContractSvc(t, repo, &mockSagaRepo{}, ls, es)

	c := validContract() // SellerID=2
	repo.On("GetContractByID", ctx, int64(1)).Return(&c, nil)
	es.On("GetRates", ctx).Return([]domain.ExchangeRate{}, nil)
	ls.On("GetListingByID", ctx, int64(5)).Return(nil, errors.New("not found"))

	item, err := svc.GetContract(ctx, 1, 2) // callerID = seller
	require.NoError(t, err)
	assert.Equal(t, int64(1), item.ID)
}

func TestOTCContractSvc_GetContract_NotParticipant(t *testing.T) {
	repo := &mockOTCRepo{}
	ctx := context.Background()
	svc := newOTCContractSvc(t, repo, &mockSagaRepo{}, &mockListingServiceIF{}, &mockExchangeServiceIF{})

	c := validContract()
	repo.On("GetContractByID", ctx, int64(1)).Return(&c, nil)

	_, err := svc.GetContract(ctx, 1, 99) // stranger
	require.ErrorIs(t, err, domain.ErrOTCNotParticipant)
}

func TestOTCContractSvc_GetContract_RepoError(t *testing.T) {
	repo := &mockOTCRepo{}
	ctx := context.Background()
	svc := newOTCContractSvc(t, repo, &mockSagaRepo{}, &mockListingServiceIF{}, &mockExchangeServiceIF{})

	repo.On("GetContractByID", ctx, int64(1)).Return(nil, errors.New("db"))

	_, err := svc.GetContract(ctx, 1, 1)
	require.Error(t, err)
}

// ─── ExerciseContract (validation paths before SAGA) ─────────────────────────

func TestExerciseContract_ContractNotFound(t *testing.T) {
	repo := &mockOTCRepo{}
	ctx := context.Background()
	svc := newOTCContractSvc(t, repo, &mockSagaRepo{}, &mockListingServiceIF{}, &mockExchangeServiceIF{})

	repo.On("GetContractByID", ctx, int64(99)).Return(nil, domain.ErrOTCContractNotFound)

	_, err := svc.ExerciseContract(ctx, domain.ExerciseOTCContractInput{ContractID: 99, CallerID: 1})
	require.Error(t, err)
}

func TestExerciseContract_NotBuyer(t *testing.T) {
	repo := &mockOTCRepo{}
	ctx := context.Background()
	svc := newOTCContractSvc(t, repo, &mockSagaRepo{}, &mockListingServiceIF{}, &mockExchangeServiceIF{})

	c := validContract()
	repo.On("GetContractByID", ctx, int64(1)).Return(&c, nil)

	_, err := svc.ExerciseContract(ctx, domain.ExerciseOTCContractInput{ContractID: 1, CallerID: 99})
	require.ErrorIs(t, err, domain.ErrOTCContractNotBuyer)
}

func TestExerciseContract_AlreadyExercised(t *testing.T) {
	repo := &mockOTCRepo{}
	ctx := context.Background()
	svc := newOTCContractSvc(t, repo, &mockSagaRepo{}, &mockListingServiceIF{}, &mockExchangeServiceIF{})

	c := validContract()
	c.Status = domain.OTCContractExercised
	repo.On("GetContractByID", ctx, int64(1)).Return(&c, nil)

	_, err := svc.ExerciseContract(ctx, domain.ExerciseOTCContractInput{ContractID: 1, CallerID: 1})
	require.ErrorIs(t, err, domain.ErrOTCContractAlreadyExecuted)
}

func TestExerciseContract_Expired(t *testing.T) {
	repo := &mockOTCRepo{}
	ctx := context.Background()
	svc := newOTCContractSvc(t, repo, &mockSagaRepo{}, &mockListingServiceIF{}, &mockExchangeServiceIF{})

	c := validContract()
	c.Status = domain.OTCContractExpired
	repo.On("GetContractByID", ctx, int64(1)).Return(&c, nil)

	_, err := svc.ExerciseContract(ctx, domain.ExerciseOTCContractInput{ContractID: 1, CallerID: 1})
	require.ErrorIs(t, err, domain.ErrOTCContractExpired)
}

func TestExerciseContract_SettlementDatePassed(t *testing.T) {
	repo := &mockOTCRepo{}
	ctx := context.Background()
	svc := newOTCContractSvc(t, repo, &mockSagaRepo{}, &mockListingServiceIF{}, &mockExchangeServiceIF{})

	c := validContract()
	c.Status = domain.OTCContractValid
	c.SettlementDate = time.Now().Add(-24 * time.Hour) // past
	repo.On("GetContractByID", ctx, int64(1)).Return(&c, nil)

	_, err := svc.ExerciseContract(ctx, domain.ExerciseOTCContractInput{ContractID: 1, CallerID: 1})
	require.ErrorIs(t, err, domain.ErrOTCContractExpired)
}

func TestExerciseContract_SagaAlreadyRunning(t *testing.T) {
	repo := &mockOTCRepo{}
	saga := &mockSagaRepo{}
	ctx := context.Background()
	svc := newOTCContractSvc(t, repo, saga, &mockListingServiceIF{}, &mockExchangeServiceIF{})

	c := validContract()
	repo.On("GetContractByID", ctx, int64(1)).Return(&c, nil)
	saga.On("GetExecutionByContractID", ctx, int64(1)).Return(&domain.OTCSagaExecution{
		ID:     7,
		Status: domain.OTCSagaStatusInProgress,
	}, nil)

	_, err := svc.ExerciseContract(ctx, domain.ExerciseOTCContractInput{ContractID: 1, CallerID: 1})
	require.ErrorIs(t, err, domain.ErrOTCSagaAlreadyRunning)
}

func TestExerciseContract_SagaCheckError(t *testing.T) {
	repo := &mockOTCRepo{}
	saga := &mockSagaRepo{}
	ctx := context.Background()
	svc := newOTCContractSvc(t, repo, saga, &mockListingServiceIF{}, &mockExchangeServiceIF{})

	c := validContract()
	repo.On("GetContractByID", ctx, int64(1)).Return(&c, nil)
	saga.On("GetExecutionByContractID", ctx, int64(1)).Return(nil, errors.New("db error"))

	_, err := svc.ExerciseContract(ctx, domain.ExerciseOTCContractInput{ContractID: 1, CallerID: 1})
	require.Error(t, err)
}

func TestExerciseContract_DeletesFailedSaga(t *testing.T) {
	repo := &mockOTCRepo{}
	saga := &mockSagaRepo{}
	es := &mockExchangeServiceIF{}
	ctx := context.Background()

	db, dbMock := newGormDB(t)
	svc := NewOTCContractService(db, repo, saga, &mockListingServiceIF{}, es)

	c := validContract()
	repo.On("GetContractByID", ctx, int64(1)).Return(&c, nil)
	saga.On("GetExecutionByContractID", ctx, int64(1)).Return(&domain.OTCSagaExecution{
		ID:     5,
		Status: domain.OTCSagaStatusFailed,
	}, nil)
	saga.On("DeleteExecution", ctx, int64(5)).Return(nil)
	es.On("GetRates", ctx).Return([]domain.ExchangeRate{{Oznaka: "USD", Srednji: 110.0}}, nil)
	// goroutine may call these; .Maybe() makes them optional
	saga.On("LogStep", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	saga.On("UpdateStep", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	// DB call: SELECT valuta for buyer account (returns empty → RSD fallback)
	dbMock.ExpectQuery("SELECT").WillReturnRows(dbMock.NewRows([]string{"oznaka"}))

	exec := &domain.OTCSagaExecution{ID: 99, Status: domain.OTCSagaStatusInProgress}
	saga.On("CreateExecution", ctx, int64(1), int64(1), mock.AnythingOfType("float64")).Return(exec, nil)

	// runSaga runs in goroutine - we don't wait for it, just verify the main path
	got, err := svc.ExerciseContract(ctx, domain.ExerciseOTCContractInput{ContractID: 1, CallerID: 1})
	require.NoError(t, err)
	assert.Equal(t, int64(99), got.ID)
	// Give goroutine a moment to start (it will fail internally but test doesn't care)
	time.Sleep(10 * time.Millisecond)
}

// ─── recordStep ───────────────────────────────────────────────────────────────

func TestRecordStep_Success(t *testing.T) {
	saga := &mockSagaRepo{}
	ctx := context.Background()
	db, _ := newGormDB(t)
	svc := NewOTCContractService(db, &mockOTCRepo{}, saga, &mockListingServiceIF{}, &mockExchangeServiceIF{}).(*otcContractService)
	exec := &domain.OTCSagaExecution{ID: 1}

	saga.On("LogStep", ctx, int64(1), domain.OTCSagaStepReserveFunds, domain.OTCSagaStepStatusCompleted, "", 1).Return(nil)
	saga.On("UpdateStep", ctx, int64(1), domain.OTCSagaStepReserveFunds, domain.OTCSagaStatusInProgress, "").Return(nil)

	err := svc.recordStep(ctx, exec, domain.OTCSagaStepReserveFunds, nil)
	require.NoError(t, err)
	assert.Equal(t, domain.OTCSagaStepReserveFunds, exec.CurrentStep)
	saga.AssertExpectations(t)
}

func TestRecordStep_Error(t *testing.T) {
	saga := &mockSagaRepo{}
	ctx := context.Background()
	db, _ := newGormDB(t)
	svc := NewOTCContractService(db, &mockOTCRepo{}, saga, &mockListingServiceIF{}, &mockExchangeServiceIF{}).(*otcContractService)
	exec := &domain.OTCSagaExecution{ID: 1}
	theErr := errors.New("step failed")

	saga.On("LogStep", ctx, int64(1), domain.OTCSagaStepReserveFunds, domain.OTCSagaStepStatusFailed, theErr.Error(), 1).Return(nil)
	saga.On("UpdateStep", ctx, int64(1), domain.OTCSagaStepReserveFunds, domain.OTCSagaStatusInProgress, theErr.Error()).Return(nil)

	err := svc.recordStep(ctx, exec, domain.OTCSagaStepReserveFunds, theErr)
	require.ErrorIs(t, err, theErr)
	saga.AssertExpectations(t)
}

// ─── compensateFrom ───────────────────────────────────────────────────────────

func TestCompensateFrom_PENDING_MarksAsFailed(t *testing.T) {
	saga := &mockSagaRepo{}
	ctx := context.Background()
	db, _ := newGormDB(t)
	svc := NewOTCContractService(db, &mockOTCRepo{}, saga, &mockListingServiceIF{}, &mockExchangeServiceIF{}).(*otcContractService)
	exec := &domain.OTCSagaExecution{ID: 5, ContractID: 1}
	originalErr := errors.New("step 1 failed")

	// compensateFrom with PENDING: startIdx=-1 → mark FAILED
	saga.On("UpdateStep", ctx, int64(5), domain.OTCSagaStepPending, domain.OTCSagaStatusCompensating, originalErr.Error()).Return(nil)
	saga.On("UpdateStep", ctx, int64(5), domain.OTCSagaStepPending, domain.OTCSagaStatusFailed, originalErr.Error()).Return(nil)

	svc.compensateFrom(ctx, exec, domain.OTCSagaStepPending, originalErr)
	saga.AssertExpectations(t)
}

// ─── stepReserveFunds ─────────────────────────────────────────────────────────

func TestStepReserveFunds_OK(t *testing.T) {
	saga := &mockSagaRepo{}
	ctx := context.Background()
	db, dbMock := newGormDB(t)
	svc := NewOTCContractService(db, &mockOTCRepo{}, saga, &mockListingServiceIF{}, &mockExchangeServiceIF{}).(*otcContractService)
	exec := &domain.OTCSagaExecution{ID: 7, ContractID: 1, BuyerReservedAmount: 100.0}
	c := &domain.OTCContract{ID: 1, BuyerAccountID: 44}

	dbMock.ExpectBegin()
	dbMock.ExpectQuery("stanje_racuna AS stanje").
		WillReturnRows(sqlmock.NewRows([]string{"stanje", "rezerv"}).AddRow(1000.0, 0.0))
	dbMock.ExpectExec("UPDATE core_banking.racun SET rezervisana_sredstva").
		WillReturnResult(sqlmock.NewResult(1, 1))
	dbMock.ExpectCommit()

	saga.On("LogStep", ctx, int64(7), domain.OTCSagaStepReserveFunds, domain.OTCSagaStepStatusCompleted, "", 1).Return(nil)
	saga.On("UpdateStep", ctx, int64(7), domain.OTCSagaStepReserveFunds, domain.OTCSagaStatusInProgress, "").Return(nil)

	err := svc.stepReserveFunds(ctx, exec, c)
	require.NoError(t, err)
	saga.AssertExpectations(t)
}

func TestStepReserveFunds_InsufficientFunds(t *testing.T) {
	saga := &mockSagaRepo{}
	ctx := context.Background()
	db, dbMock := newGormDB(t)
	svc := NewOTCContractService(db, &mockOTCRepo{}, saga, &mockListingServiceIF{}, &mockExchangeServiceIF{}).(*otcContractService)
	exec := &domain.OTCSagaExecution{ID: 7, ContractID: 1, BuyerReservedAmount: 2000.0}
	c := &domain.OTCContract{ID: 1, BuyerAccountID: 44}

	dbMock.ExpectBegin()
	dbMock.ExpectQuery("stanje_racuna AS stanje").
		WillReturnRows(sqlmock.NewRows([]string{"stanje", "rezerv"}).AddRow(1000.0, 0.0))
	dbMock.ExpectRollback()

	saga.On("LogStep", mock.Anything, int64(7), domain.OTCSagaStepReserveFunds, domain.OTCSagaStepStatusFailed, mock.AnythingOfType("string"), 1).Return(nil)
	saga.On("UpdateStep", mock.Anything, int64(7), domain.OTCSagaStepReserveFunds, domain.OTCSagaStatusInProgress, mock.AnythingOfType("string")).Return(nil)

	err := svc.stepReserveFunds(ctx, exec, c)
	require.ErrorIs(t, err, domain.ErrOTCInsufficientFunds)
}

func TestStepReserveFunds_AccountNotFound(t *testing.T) {
	saga := &mockSagaRepo{}
	ctx := context.Background()
	db, dbMock := newGormDB(t)
	svc := NewOTCContractService(db, &mockOTCRepo{}, saga, &mockListingServiceIF{}, &mockExchangeServiceIF{}).(*otcContractService)
	exec := &domain.OTCSagaExecution{ID: 7, ContractID: 1, BuyerReservedAmount: 100.0}
	c := &domain.OTCContract{ID: 1, BuyerAccountID: 44}

	dbMock.ExpectBegin()
	// stanje=0, rezerv=0 → ErrOTCAccountNotOwned
	dbMock.ExpectQuery("stanje_racuna AS stanje").
		WillReturnRows(sqlmock.NewRows([]string{"stanje", "rezerv"}).AddRow(0.0, 0.0))
	dbMock.ExpectRollback()

	saga.On("LogStep", mock.Anything, int64(7), domain.OTCSagaStepReserveFunds, domain.OTCSagaStepStatusFailed, mock.AnythingOfType("string"), 1).Return(nil)
	saga.On("UpdateStep", mock.Anything, int64(7), domain.OTCSagaStepReserveFunds, domain.OTCSagaStatusInProgress, mock.AnythingOfType("string")).Return(nil)

	err := svc.stepReserveFunds(ctx, exec, c)
	require.ErrorIs(t, err, domain.ErrOTCAccountNotOwned)
}

// ─── stepReserveSecurities ────────────────────────────────────────────────────

func TestStepReserveSecurities_ExactDelete(t *testing.T) {
	saga := &mockSagaRepo{}
	ctx := context.Background()
	db, dbMock := newGormDB(t)
	svc := NewOTCContractService(db, &mockOTCRepo{}, saga, &mockListingServiceIF{}, &mockExchangeServiceIF{}).(*otcContractService)
	exec := &domain.OTCSagaExecution{ID: 7}
	c := &domain.OTCContract{ID: 1, SellerID: 2, ListingID: 5, Amount: 10}

	dbMock.ExpectBegin()
	dbMock.ExpectQuery("SELECT id FROM core_banking.public_shares").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)).AddRow(int64(2)))
	dbMock.ExpectQuery("SELECT COALESCE").
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(int64(10)))
	dbMock.ExpectExec("DELETE FROM core_banking.public_shares").
		WillReturnResult(sqlmock.NewResult(1, 1))
	dbMock.ExpectCommit()

	saga.On("LogStep", ctx, int64(7), domain.OTCSagaStepReserveSecurities, domain.OTCSagaStepStatusCompleted, "", 1).Return(nil)
	saga.On("UpdateStep", ctx, int64(7), domain.OTCSagaStepReserveSecurities, domain.OTCSagaStatusInProgress, "").Return(nil)

	err := svc.stepReserveSecurities(ctx, exec, c)
	require.NoError(t, err)
}

func TestStepReserveSecurities_PartialDelete_Update(t *testing.T) {
	saga := &mockSagaRepo{}
	ctx := context.Background()
	db, dbMock := newGormDB(t)
	svc := NewOTCContractService(db, &mockOTCRepo{}, saga, &mockListingServiceIF{}, &mockExchangeServiceIF{}).(*otcContractService)
	exec := &domain.OTCSagaExecution{ID: 7}
	c := &domain.OTCContract{ID: 1, SellerID: 2, ListingID: 5, Amount: 10}

	dbMock.ExpectBegin()
	dbMock.ExpectQuery("SELECT id FROM core_banking.public_shares").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))
	dbMock.ExpectQuery("SELECT COALESCE").
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(int64(15))) // 15 > 10, no exact delete
	dbMock.ExpectExec("DELETE FROM core_banking.public_shares").
		WillReturnResult(sqlmock.NewResult(0, 0)) // 0 rows affected → fall through to UPDATE
	dbMock.ExpectExec("UPDATE core_banking.public_shares SET quantity").
		WillReturnResult(sqlmock.NewResult(1, 1))
	dbMock.ExpectCommit()

	saga.On("LogStep", ctx, int64(7), domain.OTCSagaStepReserveSecurities, domain.OTCSagaStepStatusCompleted, "", 1).Return(nil)
	saga.On("UpdateStep", ctx, int64(7), domain.OTCSagaStepReserveSecurities, domain.OTCSagaStatusInProgress, "").Return(nil)

	err := svc.stepReserveSecurities(ctx, exec, c)
	require.NoError(t, err)
}

// ─── stepTransferFunds ────────────────────────────────────────────────────────

func TestStepTransferFunds_OK(t *testing.T) {
	saga := &mockSagaRepo{}
	ctx := context.Background()
	db, dbMock := newGormDB(t)
	svc := NewOTCContractService(db, &mockOTCRepo{}, saga, &mockListingServiceIF{}, &mockExchangeServiceIF{}).(*otcContractService)
	exec := &domain.OTCSagaExecution{ID: 7, ContractID: 1, BuyerReservedAmount: 100.0}
	c := &domain.OTCContract{ID: 1, BuyerAccountID: 44, SellerAccountID: 55}

	dbMock.ExpectBegin()
	dbMock.ExpectExec("UPDATE core_banking.racun").WillReturnResult(sqlmock.NewResult(1, 1)) // debit buyer
	dbMock.ExpectExec("INSERT INTO core_banking.transakcija").WillReturnResult(sqlmock.NewResult(1, 1))
	dbMock.ExpectExec("UPDATE core_banking.racun").WillReturnResult(sqlmock.NewResult(1, 1)) // credit seller
	dbMock.ExpectExec("INSERT INTO core_banking.transakcija").WillReturnResult(sqlmock.NewResult(1, 1))
	dbMock.ExpectCommit()

	saga.On("LogStep", ctx, int64(7), domain.OTCSagaStepTransferFunds, domain.OTCSagaStepStatusCompleted, "", 1).Return(nil)
	saga.On("UpdateStep", ctx, int64(7), domain.OTCSagaStepTransferFunds, domain.OTCSagaStatusInProgress, "").Return(nil)

	err := svc.stepTransferFunds(ctx, exec, c)
	require.NoError(t, err)
}

// ─── stepTransferOwnership ────────────────────────────────────────────────────

func TestStepTransferOwnership_OK(t *testing.T) {
	saga := &mockSagaRepo{}
	ctx := context.Background()
	db, dbMock := newGormDB(t)
	svc := NewOTCContractService(db, &mockOTCRepo{}, saga, &mockListingServiceIF{}, &mockExchangeServiceIF{}).(*otcContractService)
	exec := &domain.OTCSagaExecution{ID: 7}
	c := &domain.OTCContract{ID: 1, BuyerID: 1, SellerID: 2, BuyerAccountID: 44, SellerAccountID: 55, ListingID: 5, Amount: 10}

	dbMock.ExpectBegin()
	dbMock.ExpectExec("INSERT INTO core_banking.orders").WillReturnResult(sqlmock.NewResult(1, 1)) // BUY
	dbMock.ExpectExec("INSERT INTO core_banking.orders").WillReturnResult(sqlmock.NewResult(1, 1)) // SELL
	dbMock.ExpectCommit()

	saga.On("LogStep", ctx, int64(7), domain.OTCSagaStepTransferOwnership, domain.OTCSagaStepStatusCompleted, "", 1).Return(nil)
	saga.On("UpdateStep", ctx, int64(7), domain.OTCSagaStepTransferOwnership, domain.OTCSagaStatusInProgress, "").Return(nil)

	err := svc.stepTransferOwnership(ctx, exec, c)
	require.NoError(t, err)
}

// ─── compensateReserveFunds ───────────────────────────────────────────────────

func TestCompensateReserveFunds_OK(t *testing.T) {
	ctx := context.Background()
	db, dbMock := newGormDB(t)
	svc := NewOTCContractService(db, &mockOTCRepo{}, &mockSagaRepo{}, &mockListingServiceIF{}, &mockExchangeServiceIF{}).(*otcContractService)
	exec := &domain.OTCSagaExecution{ID: 7, ContractID: 1, BuyerReservedAmount: 100.0}

	dbMock.ExpectExec("UPDATE core_banking.racun SET rezervisana_sredstva").
		WillReturnResult(sqlmock.NewResult(1, 1))

	err := svc.compensateReserveFunds(ctx, exec)
	require.NoError(t, err)
}

// ─── markCompleted ────────────────────────────────────────────────────────────

func TestMarkCompleted_OK(t *testing.T) {
	repo := &mockOTCRepo{}
	saga := &mockSagaRepo{}
	ctx := context.Background()
	db, dbMock := newGormDB(t)
	svc := NewOTCContractService(db, repo, saga, &mockListingServiceIF{}, &mockExchangeServiceIF{}).(*otcContractService)
	exec := &domain.OTCSagaExecution{ID: 7, ContractID: 1}
	c := &domain.OTCContract{ID: 1, BuyerID: 1, ListingID: 5}

	// double-check query → count=1 (synthetic BUY found)
	dbMock.ExpectQuery("otc_saga_execution_id").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(1)))
	dbMock.ExpectBegin()
	repo.On("UpdateContractStatus", ctx, int64(1), domain.OTCContractExercised).Return(nil)
	saga.On("UpdateStep", ctx, int64(7), domain.OTCSagaStepCompleted, domain.OTCSagaStatusCompleted, "").Return(nil)
	dbMock.ExpectCommit()
	saga.On("LogStep", ctx, int64(7), domain.OTCSagaStepCompleted, domain.OTCSagaStepStatusCompleted, "", 1).Return(nil)

	err := svc.markCompleted(ctx, exec, c)
	require.NoError(t, err)
}

func TestMarkCompleted_SyntheticBuyNotFound(t *testing.T) {
	ctx := context.Background()
	db, dbMock := newGormDB(t)
	svc := NewOTCContractService(db, &mockOTCRepo{}, &mockSagaRepo{}, &mockListingServiceIF{}, &mockExchangeServiceIF{}).(*otcContractService)
	exec := &domain.OTCSagaExecution{ID: 7, ContractID: 1}
	c := &domain.OTCContract{ID: 1, BuyerID: 1, ListingID: 5}

	dbMock.ExpectQuery("otc_saga_execution_id").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0))) // not found

	err := svc.markCompleted(ctx, exec, c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "double-check")
}

// ─── compensateTransferOwnership ─────────────────────────────────────────────

func TestCompensateTransferOwnership_OK(t *testing.T) {
	ctx := context.Background()
	db, dbMock := newGormDB(t)
	svc := NewOTCContractService(db, &mockOTCRepo{}, &mockSagaRepo{}, &mockListingServiceIF{}, &mockExchangeServiceIF{}).(*otcContractService)
	exec := &domain.OTCSagaExecution{ID: 7}

	dbMock.ExpectExec("DELETE FROM core_banking.orders").
		WillReturnResult(sqlmock.NewResult(1, 2))

	err := svc.compensateTransferOwnership(ctx, exec, 1)
	require.NoError(t, err)
}

func TestCompensateTransferOwnership_Error(t *testing.T) {
	ctx := context.Background()
	db, dbMock := newGormDB(t)
	svc := NewOTCContractService(db, &mockOTCRepo{}, &mockSagaRepo{}, &mockListingServiceIF{}, &mockExchangeServiceIF{}).(*otcContractService)
	exec := &domain.OTCSagaExecution{ID: 7}

	dbMock.ExpectExec("DELETE FROM core_banking.orders").
		WillReturnError(errors.New("db error"))

	err := svc.compensateTransferOwnership(ctx, exec, 1)
	require.Error(t, err)
}

// ─── compensateReserveSecurities ─────────────────────────────────────────────

func TestCompensateReserveSecurities_UpdatePath(t *testing.T) {
	ctx := context.Background()
	db, dbMock := newGormDB(t)
	svc := NewOTCContractService(db, &mockOTCRepo{}, &mockSagaRepo{}, &mockListingServiceIF{}, &mockExchangeServiceIF{}).(*otcContractService)
	exec := &domain.OTCSagaExecution{ID: 7, ContractID: 1}

	dbMock.ExpectBegin()
	dbMock.ExpectQuery("SELECT seller_id").
		WillReturnRows(sqlmock.NewRows([]string{"seller_id", "listing_id", "amount"}).AddRow(int64(2), int64(5), int32(10)))
	dbMock.ExpectExec("UPDATE core_banking.public_shares").
		WillReturnResult(sqlmock.NewResult(1, 1))
	dbMock.ExpectCommit()

	err := svc.compensateReserveSecurities(ctx, exec, 1)
	require.NoError(t, err)
}

func TestCompensateReserveSecurities_InsertPath(t *testing.T) {
	ctx := context.Background()
	db, dbMock := newGormDB(t)
	svc := NewOTCContractService(db, &mockOTCRepo{}, &mockSagaRepo{}, &mockListingServiceIF{}, &mockExchangeServiceIF{}).(*otcContractService)
	exec := &domain.OTCSagaExecution{ID: 7, ContractID: 1}

	dbMock.ExpectBegin()
	dbMock.ExpectQuery("SELECT seller_id").
		WillReturnRows(sqlmock.NewRows([]string{"seller_id", "listing_id", "amount"}).AddRow(int64(2), int64(5), int32(10)))
	dbMock.ExpectExec("UPDATE core_banking.public_shares").
		WillReturnResult(sqlmock.NewResult(0, 0)) // 0 rows → INSERT path
	dbMock.ExpectExec("INSERT INTO core_banking.public_shares").
		WillReturnResult(sqlmock.NewResult(1, 1))
	dbMock.ExpectCommit()

	err := svc.compensateReserveSecurities(ctx, exec, 1)
	require.NoError(t, err)
}

func TestCompensateReserveSecurities_SelectError(t *testing.T) {
	ctx := context.Background()
	db, dbMock := newGormDB(t)
	svc := NewOTCContractService(db, &mockOTCRepo{}, &mockSagaRepo{}, &mockListingServiceIF{}, &mockExchangeServiceIF{}).(*otcContractService)
	exec := &domain.OTCSagaExecution{ID: 7, ContractID: 1}

	dbMock.ExpectBegin()
	dbMock.ExpectQuery("SELECT seller_id").
		WillReturnError(errors.New("db error"))
	dbMock.ExpectRollback()

	err := svc.compensateReserveSecurities(ctx, exec, 1)
	require.Error(t, err)
}

// ─── compensateTransferFunds ─────────────────────────────────────────────────

func TestCompensateTransferFunds_OK(t *testing.T) {
	ctx := context.Background()
	db, dbMock := newGormDB(t)
	svc := NewOTCContractService(db, &mockOTCRepo{}, &mockSagaRepo{}, &mockListingServiceIF{}, &mockExchangeServiceIF{}).(*otcContractService)
	exec := &domain.OTCSagaExecution{ID: 7, ContractID: 1, BuyerReservedAmount: 100.0}

	dbMock.ExpectBegin()
	dbMock.ExpectQuery("SELECT buyer_account_id").
		WillReturnRows(sqlmock.NewRows([]string{"buyer_account_id", "seller_account_id"}).AddRow(int64(44), int64(55)))
	dbMock.ExpectExec("UPDATE core_banking.racun SET stanje_racuna = stanje_racuna - ").
		WillReturnResult(sqlmock.NewResult(1, 1))
	dbMock.ExpectExec("INSERT INTO core_banking.transakcija").
		WillReturnResult(sqlmock.NewResult(1, 1))
	dbMock.ExpectExec("UPDATE core_banking.racun").
		WillReturnResult(sqlmock.NewResult(1, 1))
	dbMock.ExpectExec("INSERT INTO core_banking.transakcija").
		WillReturnResult(sqlmock.NewResult(1, 1))
	dbMock.ExpectCommit()

	err := svc.compensateTransferFunds(ctx, exec)
	require.NoError(t, err)
}

func TestCompensateTransferFunds_SelectError(t *testing.T) {
	ctx := context.Background()
	db, dbMock := newGormDB(t)
	svc := NewOTCContractService(db, &mockOTCRepo{}, &mockSagaRepo{}, &mockListingServiceIF{}, &mockExchangeServiceIF{}).(*otcContractService)
	exec := &domain.OTCSagaExecution{ID: 7, ContractID: 1, BuyerReservedAmount: 100.0}

	dbMock.ExpectBegin()
	dbMock.ExpectQuery("SELECT buyer_account_id").
		WillReturnError(errors.New("db error"))
	dbMock.ExpectRollback()

	err := svc.compensateTransferFunds(ctx, exec)
	require.Error(t, err)
}

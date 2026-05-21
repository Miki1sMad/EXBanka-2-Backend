package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"banka-backend/services/bank-service/internal/domain"
	"banka-backend/services/bank-service/internal/service"
)

// ─── Mocks ────────────────────────────────────────────────────────────────────

type mockInvestmentFundRepo struct{ mock.Mock }

func (m *mockInvestmentFundRepo) Create(ctx context.Context, fund domain.InvestmentFund) (*domain.InvestmentFund, error) {
	args := m.Called(ctx, fund)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.InvestmentFund), args.Error(1)
}
func (m *mockInvestmentFundRepo) GetByID(ctx context.Context, id int64) (*domain.InvestmentFund, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.InvestmentFund), args.Error(1)
}
func (m *mockInvestmentFundRepo) GetAccountNumber(ctx context.Context, accountID int64) (string, error) {
	args := m.Called(ctx, accountID)
	return args.String(0), args.Error(1)
}
func (m *mockInvestmentFundRepo) ListBankRSDAccounts(ctx context.Context) ([]domain.BankAccountItem, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.BankAccountItem), args.Error(1)
}
func (m *mockInvestmentFundRepo) ListBankAllAccounts(ctx context.Context) ([]domain.BankAccountItem, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.BankAccountItem), args.Error(1)
}
func (m *mockInvestmentFundRepo) List(ctx context.Context) ([]domain.InvestmentFund, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.InvestmentFund), args.Error(1)
}
func (m *mockInvestmentFundRepo) TransferManagerFunds(ctx context.Context, oldManagerID, newManagerID int64) error {
	return m.Called(ctx, oldManagerID, newManagerID).Error(0)
}
func (m *mockInvestmentFundRepo) GetSecurities(ctx context.Context, fundID int64) ([]domain.FundSecurity, error) {
	args := m.Called(ctx, fundID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.FundSecurity), args.Error(1)
}
func (m *mockInvestmentFundRepo) UpsertSecurity(ctx context.Context, sec domain.FundSecurity) error {
	return m.Called(ctx, sec).Error(0)
}
func (m *mockInvestmentFundRepo) AddSecurityQuantity(ctx context.Context, fundID, listingID int64, deltaQty float64, acquisitionDate time.Time, deltaCostRSD float64) error {
	return m.Called(ctx, fundID, listingID, deltaQty, acquisitionDate, deltaCostRSD).Error(0)
}
func (m *mockInvestmentFundRepo) DeductLiquidAssets(ctx context.Context, fundID int64, amountRSD float64) error {
	return m.Called(ctx, fundID, amountRSD).Error(0)
}
func (m *mockInvestmentFundRepo) GetPositions(ctx context.Context, fundID int64) ([]domain.ClientFundPosition, error) {
	args := m.Called(ctx, fundID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.ClientFundPosition), args.Error(1)
}
func (m *mockInvestmentFundRepo) GetTotalInvested(ctx context.Context, fundID int64) (float64, error) {
	args := m.Called(ctx, fundID)
	return args.Get(0).(float64), args.Error(1)
}
func (m *mockInvestmentFundRepo) WithDB(db interface{}) domain.InvestmentFundRepository {
	return m.Called(db).Get(0).(domain.InvestmentFundRepository)
}

type mockListingServiceIF struct{ mock.Mock }

func (m *mockListingServiceIF) ListListings(ctx context.Context, filter domain.ListingFilter) ([]domain.ListingCalculated, int64, error) {
	args := m.Called(ctx, filter)
	if args.Get(0) == nil {
		return nil, args.Get(1).(int64), args.Error(2)
	}
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

type mockExchangeServiceIF struct{ mock.Mock }

func (m *mockExchangeServiceIF) GetRates(ctx context.Context) ([]domain.ExchangeRate, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.ExchangeRate), args.Error(1)
}
func (m *mockExchangeServiceIF) CalculateExchange(ctx context.Context, fromOznaka, toOznaka string, amount float64) (*domain.ExchangeConversionResult, error) {
	args := m.Called(ctx, fromOznaka, toOznaka, amount)
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

type mockAccountServiceIF struct{ mock.Mock }

func (m *mockAccountServiceIF) CreateAccount(ctx context.Context, input domain.CreateAccountInput) (int64, error) {
	args := m.Called(ctx, input)
	return args.Get(0).(int64), args.Error(1)
}
func (m *mockAccountServiceIF) GetAllAccounts(ctx context.Context, f string) ([]domain.EmployeeAccountListItem, error) {
	args := m.Called(ctx, f)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.EmployeeAccountListItem), args.Error(1)
}
func (m *mockAccountServiceIF) GetClientAccounts(ctx context.Context, vlasnikID int64) ([]domain.AccountListItem, error) {
	args := m.Called(ctx, vlasnikID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.AccountListItem), args.Error(1)
}
func (m *mockAccountServiceIF) GetAccountDetail(ctx context.Context, accountID, vlasnikID int64) (*domain.AccountDetail, error) {
	args := m.Called(ctx, accountID, vlasnikID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.AccountDetail), args.Error(1)
}
func (m *mockAccountServiceIF) GetAccountTransactions(ctx context.Context, input domain.GetAccountTransactionsInput, vlasnikID int64) ([]domain.Transakcija, error) {
	args := m.Called(ctx, input, vlasnikID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Transakcija), args.Error(1)
}
func (m *mockAccountServiceIF) RenameAccount(ctx context.Context, input domain.RenameAccountInput) error {
	return m.Called(ctx, input).Error(0)
}
func (m *mockAccountServiceIF) UpdateAccountLimit(ctx context.Context, input domain.UpdateLimitInput) (int64, error) {
	args := m.Called(ctx, input)
	return args.Get(0).(int64), args.Error(1)
}
func (m *mockAccountServiceIF) GetPendingActions(ctx context.Context, vlasnikID int64) ([]domain.PendingAction, error) {
	args := m.Called(ctx, vlasnikID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.PendingAction), args.Error(1)
}
func (m *mockAccountServiceIF) GetPendingAction(ctx context.Context, actionID, vlasnikID int64) (*domain.PendingAction, error) {
	args := m.Called(ctx, actionID, vlasnikID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.PendingAction), args.Error(1)
}
func (m *mockAccountServiceIF) ApprovePendingAction(ctx context.Context, actionID, vlasnikID int64) (string, time.Time, error) {
	args := m.Called(ctx, actionID, vlasnikID)
	return args.String(0), args.Get(1).(time.Time), args.Error(2)
}
func (m *mockAccountServiceIF) VerifyAndApplyLimit(ctx context.Context, input domain.VerifyLimitInput) error {
	return m.Called(ctx, input).Error(0)
}
func (m *mockAccountServiceIF) FindAccountIDByNumber(ctx context.Context, brojRacuna string) (int64, error) {
	args := m.Called(ctx, brojRacuna)
	return args.Get(0).(int64), args.Error(1)
}

type mockCurrencyRepo struct{ mock.Mock }

func (m *mockCurrencyRepo) GetAll(ctx context.Context) ([]domain.Currency, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Currency), args.Error(1)
}
func (m *mockCurrencyRepo) GetByID(ctx context.Context, id int64) (*domain.Currency, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Currency), args.Error(1)
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func newFundService(repo domain.InvestmentFundRepository, ls domain.ListingService, es domain.ExchangeService, as domain.AccountService, cr domain.CurrencyRepository) domain.InvestmentFundService {
	return service.NewInvestmentFundService(repo, ls, es, as, cr)
}

// ─── CreateFund ───────────────────────────────────────────────────────────────

func TestCreateFund_OK(t *testing.T) {
	repo := &mockInvestmentFundRepo{}
	ls := &mockListingServiceIF{}
	es := &mockExchangeServiceIF{}
	as := &mockAccountServiceIF{}
	cr := &mockCurrencyRepo{}

	ctx := context.Background()

	cr.On("GetAll", ctx).Return([]domain.Currency{{ID: 3, Oznaka: "RSD"}}, nil)
	as.On("CreateAccount", ctx, mock.AnythingOfType("domain.CreateAccountInput")).Return(int64(42), nil)

	expected := &domain.InvestmentFund{ID: 1, Name: "Test Fund", ManagerID: 7, AccountID: 42}
	repo.On("Create", ctx, mock.AnythingOfType("domain.InvestmentFund")).Return(expected, nil)

	svc := newFundService(repo, ls, es, as, cr)
	fund, err := svc.CreateFund(ctx, domain.CreateFundInput{
		Name:                "Test Fund",
		Description:         "Opis",
		MinimumContribution: 1000,
		ManagerID:           7,
	})

	require.NoError(t, err)
	assert.Equal(t, int64(1), fund.ID)
	assert.Equal(t, int64(42), fund.AccountID)
	repo.AssertExpectations(t)
	cr.AssertExpectations(t)
	as.AssertExpectations(t)
}

func TestCreateFund_CurrencyRepoError(t *testing.T) {
	repo := &mockInvestmentFundRepo{}
	ls := &mockListingServiceIF{}
	es := &mockExchangeServiceIF{}
	as := &mockAccountServiceIF{}
	cr := &mockCurrencyRepo{}

	ctx := context.Background()
	cr.On("GetAll", ctx).Return(nil, errors.New("db error"))

	svc := newFundService(repo, ls, es, as, cr)
	_, err := svc.CreateFund(ctx, domain.CreateFundInput{Name: "F", ManagerID: 1})
	require.Error(t, err)
}

func TestCreateFund_RSDNotFound(t *testing.T) {
	repo := &mockInvestmentFundRepo{}
	ls := &mockListingServiceIF{}
	es := &mockExchangeServiceIF{}
	as := &mockAccountServiceIF{}
	cr := &mockCurrencyRepo{}

	ctx := context.Background()
	cr.On("GetAll", ctx).Return([]domain.Currency{{ID: 1, Oznaka: "EUR"}}, nil)

	svc := newFundService(repo, ls, es, as, cr)
	_, err := svc.CreateFund(ctx, domain.CreateFundInput{Name: "F", ManagerID: 1})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RSD")
}

func TestCreateFund_AccountCreateError(t *testing.T) {
	repo := &mockInvestmentFundRepo{}
	ls := &mockListingServiceIF{}
	es := &mockExchangeServiceIF{}
	as := &mockAccountServiceIF{}
	cr := &mockCurrencyRepo{}

	ctx := context.Background()
	cr.On("GetAll", ctx).Return([]domain.Currency{{ID: 3, Oznaka: "RSD"}}, nil)
	as.On("CreateAccount", ctx, mock.AnythingOfType("domain.CreateAccountInput")).Return(int64(0), errors.New("account error"))

	svc := newFundService(repo, ls, es, as, cr)
	_, err := svc.CreateFund(ctx, domain.CreateFundInput{Name: "F", ManagerID: 1})
	require.Error(t, err)
}

// ─── GetFundByID ──────────────────────────────────────────────────────────────

func TestGetFundByID_OK(t *testing.T) {
	repo := &mockInvestmentFundRepo{}
	ctx := context.Background()
	expected := &domain.InvestmentFund{ID: 5, Name: "My Fund"}
	repo.On("GetByID", ctx, int64(5)).Return(expected, nil)

	svc := newFundService(repo, &mockListingServiceIF{}, &mockExchangeServiceIF{}, &mockAccountServiceIF{}, &mockCurrencyRepo{})
	fund, err := svc.GetFundByID(ctx, 5)
	require.NoError(t, err)
	assert.Equal(t, int64(5), fund.ID)
}

func TestGetFundByID_Error(t *testing.T) {
	repo := &mockInvestmentFundRepo{}
	ctx := context.Background()
	repo.On("GetByID", ctx, int64(99)).Return(nil, domain.ErrFundNotFound)

	svc := newFundService(repo, &mockListingServiceIF{}, &mockExchangeServiceIF{}, &mockAccountServiceIF{}, &mockCurrencyRepo{})
	_, err := svc.GetFundByID(ctx, 99)
	require.ErrorIs(t, err, domain.ErrFundNotFound)
}

// ─── GetFundDetails ───────────────────────────────────────────────────────────

func TestGetFundDetails_OK(t *testing.T) {
	repo := &mockInvestmentFundRepo{}
	ls := &mockListingServiceIF{}
	es := &mockExchangeServiceIF{}

	ctx := context.Background()
	fund := &domain.InvestmentFund{ID: 1, Name: "F", LiquidAssets: 1000}
	repo.On("GetByID", ctx, int64(1)).Return(fund, nil)
	repo.On("GetSecurities", ctx, int64(1)).Return([]domain.FundSecurity{
		{ID: 1, FundID: 1, ListingID: 10, Quantity: 5},
	}, nil)
	repo.On("GetTotalInvested", ctx, int64(1)).Return(float64(500), nil)

	es.On("GetRates", ctx).Return([]domain.ExchangeRate{{Oznaka: "USD", Srednji: 110.0}}, nil)
	ls.On("GetListingByID", ctx, int64(10)).Return(&domain.ListingCalculated{
		Listing:       domain.Listing{ID: 10, Ticker: "AAPL", Price: 150, Volume: 1000},
		ChangePercent: 2.5,
	}, nil)

	svc := newFundService(repo, ls, es, &mockAccountServiceIF{}, &mockCurrencyRepo{})
	details, err := svc.GetFundDetails(ctx, 1)

	require.NoError(t, err)
	assert.Equal(t, int64(1), details.ID)
	assert.Equal(t, 1, len(details.Securities))
	assert.Equal(t, "AAPL", details.Securities[0].Ticker)
	// fundValue = 1000 (liquid) + 5 * 150 * 110 = 1000 + 82500 = 83500
	assert.InDelta(t, 83500.0, details.FundValueRSD, 1.0)
	assert.InDelta(t, 83000.0, details.Profit, 1.0)
}

func TestGetFundDetails_FundNotFound(t *testing.T) {
	repo := &mockInvestmentFundRepo{}
	ctx := context.Background()
	repo.On("GetByID", ctx, int64(99)).Return(nil, domain.ErrFundNotFound)

	svc := newFundService(repo, &mockListingServiceIF{}, &mockExchangeServiceIF{}, &mockAccountServiceIF{}, &mockCurrencyRepo{})
	_, err := svc.GetFundDetails(ctx, 99)
	require.ErrorIs(t, err, domain.ErrFundNotFound)
}

func TestGetFundDetails_SecuritiesError(t *testing.T) {
	repo := &mockInvestmentFundRepo{}
	es := &mockExchangeServiceIF{}
	ctx := context.Background()

	repo.On("GetByID", ctx, int64(1)).Return(&domain.InvestmentFund{ID: 1}, nil)
	repo.On("GetSecurities", ctx, int64(1)).Return(nil, errors.New("db error"))

	svc := newFundService(repo, &mockListingServiceIF{}, es, &mockAccountServiceIF{}, &mockCurrencyRepo{})
	_, err := svc.GetFundDetails(ctx, 1)
	require.Error(t, err)
}

func TestGetFundDetails_ListingNotFound_Skipped(t *testing.T) {
	repo := &mockInvestmentFundRepo{}
	ls := &mockListingServiceIF{}
	es := &mockExchangeServiceIF{}
	ctx := context.Background()

	fund := &domain.InvestmentFund{ID: 1, LiquidAssets: 500}
	repo.On("GetByID", ctx, int64(1)).Return(fund, nil)
	repo.On("GetSecurities", ctx, int64(1)).Return([]domain.FundSecurity{
		{ID: 1, FundID: 1, ListingID: 10, Quantity: 3},
	}, nil)
	repo.On("GetTotalInvested", ctx, int64(1)).Return(float64(300), nil)
	es.On("GetRates", ctx).Return([]domain.ExchangeRate{{Oznaka: "USD", Srednji: 100.0}}, nil)
	ls.On("GetListingByID", ctx, int64(10)).Return(nil, errors.New("not found"))

	svc := newFundService(repo, ls, es, &mockAccountServiceIF{}, &mockCurrencyRepo{})
	details, err := svc.GetFundDetails(ctx, 1)
	require.NoError(t, err)
	assert.Empty(t, details.Securities)
	// fundValue = liquid only since listing not found
	assert.InDelta(t, 500.0, details.FundValueRSD, 0.1)
}

func TestGetFundDetails_ExchangeRateError_Fallback(t *testing.T) {
	repo := &mockInvestmentFundRepo{}
	ls := &mockListingServiceIF{}
	es := &mockExchangeServiceIF{}
	ctx := context.Background()

	fund := &domain.InvestmentFund{ID: 1, LiquidAssets: 200}
	repo.On("GetByID", ctx, int64(1)).Return(fund, nil)
	repo.On("GetSecurities", ctx, int64(1)).Return([]domain.FundSecurity{
		{ID: 1, FundID: 1, ListingID: 5, Quantity: 2},
	}, nil)
	repo.On("GetTotalInvested", ctx, int64(1)).Return(float64(100), nil)
	es.On("GetRates", ctx).Return(nil, errors.New("rate error"))
	ls.On("GetListingByID", ctx, int64(5)).Return(&domain.ListingCalculated{
		Listing: domain.Listing{ID: 5, Ticker: "IBM", Price: 50},
	}, nil)

	svc := newFundService(repo, ls, es, &mockAccountServiceIF{}, &mockCurrencyRepo{})
	details, err := svc.GetFundDetails(ctx, 1)
	require.NoError(t, err)
	// usdRate falls back to 1.0; fundValue = 200 + 2*50*1 = 300
	assert.InDelta(t, 300.0, details.FundValueRSD, 0.1)
}

// ─── ListFunds ────────────────────────────────────────────────────────────────

func TestListFunds_Empty(t *testing.T) {
	repo := &mockInvestmentFundRepo{}
	es := &mockExchangeServiceIF{}
	ctx := context.Background()

	repo.On("List", ctx).Return([]domain.InvestmentFund{}, nil)
	es.On("GetRates", ctx).Return([]domain.ExchangeRate{}, nil)

	svc := newFundService(repo, &mockListingServiceIF{}, es, &mockAccountServiceIF{}, &mockCurrencyRepo{})
	items, err := svc.ListFunds(ctx, domain.FundFilter{})
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestListFunds_RepoError(t *testing.T) {
	repo := &mockInvestmentFundRepo{}
	es := &mockExchangeServiceIF{}
	ctx := context.Background()

	repo.On("List", ctx).Return(nil, errors.New("db"))
	es.On("GetRates", ctx).Return([]domain.ExchangeRate{}, nil)

	svc := newFundService(repo, &mockListingServiceIF{}, es, &mockAccountServiceIF{}, &mockCurrencyRepo{})
	_, err := svc.ListFunds(ctx, domain.FundFilter{})
	require.Error(t, err)
}

func TestListFunds_WithSearch(t *testing.T) {
	repo := &mockInvestmentFundRepo{}
	ls := &mockListingServiceIF{}
	es := &mockExchangeServiceIF{}
	ctx := context.Background()

	funds := []domain.InvestmentFund{
		{ID: 1, Name: "Alpha Fund"},
		{ID: 2, Name: "Beta Capital"},
		{ID: 3, Name: "alpha growth", Description: "alpha in description"},
	}
	repo.On("List", ctx).Return(funds, nil)
	es.On("GetRates", ctx).Return([]domain.ExchangeRate{{Oznaka: "USD", Srednji: 100}}, nil)
	// For each fund GetSecurities and GetTotalInvested will be called
	repo.On("GetSecurities", ctx, int64(1)).Return([]domain.FundSecurity{}, nil)
	repo.On("GetSecurities", ctx, int64(3)).Return([]domain.FundSecurity{}, nil)
	repo.On("GetTotalInvested", ctx, int64(1)).Return(float64(0), nil)
	repo.On("GetTotalInvested", ctx, int64(3)).Return(float64(0), nil)

	svc := newFundService(repo, ls, es, &mockAccountServiceIF{}, &mockCurrencyRepo{})
	items, err := svc.ListFunds(ctx, domain.FundFilter{Search: "alpha"})
	require.NoError(t, err)
	assert.Len(t, items, 2)
}

func TestListFunds_SortByName_ASC(t *testing.T) {
	repo := &mockInvestmentFundRepo{}
	ls := &mockListingServiceIF{}
	es := &mockExchangeServiceIF{}
	ctx := context.Background()

	funds := []domain.InvestmentFund{
		{ID: 2, Name: "Zebra Fund"},
		{ID: 1, Name: "Apple Fund"},
	}
	repo.On("List", ctx).Return(funds, nil)
	es.On("GetRates", ctx).Return([]domain.ExchangeRate{}, nil)
	repo.On("GetSecurities", ctx, int64(2)).Return([]domain.FundSecurity{}, nil)
	repo.On("GetSecurities", ctx, int64(1)).Return([]domain.FundSecurity{}, nil)
	repo.On("GetTotalInvested", ctx, int64(2)).Return(float64(0), nil)
	repo.On("GetTotalInvested", ctx, int64(1)).Return(float64(0), nil)

	svc := newFundService(repo, ls, es, &mockAccountServiceIF{}, &mockCurrencyRepo{})
	items, err := svc.ListFunds(ctx, domain.FundFilter{SortBy: "name", SortOrder: "ASC"})
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "Apple Fund", items[0].Name)
	assert.Equal(t, "Zebra Fund", items[1].Name)
}

func TestListFunds_SortByName_DESC(t *testing.T) {
	repo := &mockInvestmentFundRepo{}
	ls := &mockListingServiceIF{}
	es := &mockExchangeServiceIF{}
	ctx := context.Background()

	funds := []domain.InvestmentFund{
		{ID: 1, Name: "Apple Fund"},
		{ID: 2, Name: "Zebra Fund"},
	}
	repo.On("List", ctx).Return(funds, nil)
	es.On("GetRates", ctx).Return([]domain.ExchangeRate{}, nil)
	repo.On("GetSecurities", ctx, int64(1)).Return([]domain.FundSecurity{}, nil)
	repo.On("GetSecurities", ctx, int64(2)).Return([]domain.FundSecurity{}, nil)
	repo.On("GetTotalInvested", ctx, int64(1)).Return(float64(0), nil)
	repo.On("GetTotalInvested", ctx, int64(2)).Return(float64(0), nil)

	svc := newFundService(repo, ls, es, &mockAccountServiceIF{}, &mockCurrencyRepo{})
	items, err := svc.ListFunds(ctx, domain.FundFilter{SortBy: "name", SortOrder: "DESC"})
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "Zebra Fund", items[0].Name)
}

func TestListFunds_SortByFundValue(t *testing.T) {
	repo := &mockInvestmentFundRepo{}
	es := &mockExchangeServiceIF{}
	ctx := context.Background()

	funds := []domain.InvestmentFund{
		{ID: 1, LiquidAssets: 500},
		{ID: 2, LiquidAssets: 1500},
	}
	repo.On("List", ctx).Return(funds, nil)
	es.On("GetRates", ctx).Return([]domain.ExchangeRate{}, nil)
	repo.On("GetSecurities", ctx, int64(1)).Return([]domain.FundSecurity{}, nil)
	repo.On("GetSecurities", ctx, int64(2)).Return([]domain.FundSecurity{}, nil)
	repo.On("GetTotalInvested", ctx, int64(1)).Return(float64(0), nil)
	repo.On("GetTotalInvested", ctx, int64(2)).Return(float64(0), nil)

	svc := newFundService(repo, &mockListingServiceIF{}, es, &mockAccountServiceIF{}, &mockCurrencyRepo{})
	items, err := svc.ListFunds(ctx, domain.FundFilter{SortBy: "fundvalue", SortOrder: "ASC"})
	require.NoError(t, err)
	assert.InDelta(t, 500.0, items[0].FundValueRSD, 0.1)
	assert.InDelta(t, 1500.0, items[1].FundValueRSD, 0.1)
}

func TestListFunds_SortByProfit(t *testing.T) {
	repo := &mockInvestmentFundRepo{}
	es := &mockExchangeServiceIF{}
	ctx := context.Background()

	funds := []domain.InvestmentFund{
		{ID: 1, LiquidAssets: 1000},
		{ID: 2, LiquidAssets: 2000},
	}
	repo.On("List", ctx).Return(funds, nil)
	es.On("GetRates", ctx).Return([]domain.ExchangeRate{}, nil)
	repo.On("GetSecurities", ctx, int64(1)).Return([]domain.FundSecurity{}, nil)
	repo.On("GetSecurities", ctx, int64(2)).Return([]domain.FundSecurity{}, nil)
	repo.On("GetTotalInvested", ctx, int64(1)).Return(float64(800), nil)
	repo.On("GetTotalInvested", ctx, int64(2)).Return(float64(100), nil)

	svc := newFundService(repo, &mockListingServiceIF{}, es, &mockAccountServiceIF{}, &mockCurrencyRepo{})
	items, err := svc.ListFunds(ctx, domain.FundFilter{SortBy: "profit", SortOrder: "DESC"})
	require.NoError(t, err)
	// Fund 2 profit = 2000 - 100 = 1900, Fund 1 profit = 1000 - 800 = 200
	assert.InDelta(t, 1900.0, items[0].Profit, 0.1)
}

func TestListFunds_SortByMinimumContribution(t *testing.T) {
	repo := &mockInvestmentFundRepo{}
	es := &mockExchangeServiceIF{}
	ctx := context.Background()

	funds := []domain.InvestmentFund{
		{ID: 1, MinimumContribution: 5000},
		{ID: 2, MinimumContribution: 1000},
	}
	repo.On("List", ctx).Return(funds, nil)
	es.On("GetRates", ctx).Return([]domain.ExchangeRate{}, nil)
	repo.On("GetSecurities", ctx, int64(1)).Return([]domain.FundSecurity{}, nil)
	repo.On("GetSecurities", ctx, int64(2)).Return([]domain.FundSecurity{}, nil)
	repo.On("GetTotalInvested", ctx, int64(1)).Return(float64(0), nil)
	repo.On("GetTotalInvested", ctx, int64(2)).Return(float64(0), nil)

	svc := newFundService(repo, &mockListingServiceIF{}, es, &mockAccountServiceIF{}, &mockCurrencyRepo{})
	items, err := svc.ListFunds(ctx, domain.FundFilter{SortBy: "minimumcontribution", SortOrder: "ASC"})
	require.NoError(t, err)
	assert.InDelta(t, 1000.0, items[0].MinimumContribution, 0.1)
}

func TestListFunds_SortByDescription(t *testing.T) {
	repo := &mockInvestmentFundRepo{}
	es := &mockExchangeServiceIF{}
	ctx := context.Background()

	funds := []domain.InvestmentFund{
		{ID: 1, Description: "zeta"},
		{ID: 2, Description: "alpha"},
	}
	repo.On("List", ctx).Return(funds, nil)
	es.On("GetRates", ctx).Return([]domain.ExchangeRate{}, nil)
	repo.On("GetSecurities", ctx, int64(1)).Return([]domain.FundSecurity{}, nil)
	repo.On("GetSecurities", ctx, int64(2)).Return([]domain.FundSecurity{}, nil)
	repo.On("GetTotalInvested", ctx, int64(1)).Return(float64(0), nil)
	repo.On("GetTotalInvested", ctx, int64(2)).Return(float64(0), nil)

	svc := newFundService(repo, &mockListingServiceIF{}, es, &mockAccountServiceIF{}, &mockCurrencyRepo{})
	items, err := svc.ListFunds(ctx, domain.FundFilter{SortBy: "description", SortOrder: "ASC"})
	require.NoError(t, err)
	assert.Equal(t, "alpha", items[0].Description)
}

func TestListFunds_SortByDefault(t *testing.T) {
	repo := &mockInvestmentFundRepo{}
	es := &mockExchangeServiceIF{}
	ctx := context.Background()

	funds := []domain.InvestmentFund{
		{ID: 3, Name: "C"},
		{ID: 1, Name: "A"},
	}
	repo.On("List", ctx).Return(funds, nil)
	es.On("GetRates", ctx).Return([]domain.ExchangeRate{}, nil)
	repo.On("GetSecurities", ctx, int64(3)).Return([]domain.FundSecurity{}, nil)
	repo.On("GetSecurities", ctx, int64(1)).Return([]domain.FundSecurity{}, nil)
	repo.On("GetTotalInvested", ctx, int64(3)).Return(float64(0), nil)
	repo.On("GetTotalInvested", ctx, int64(1)).Return(float64(0), nil)

	svc := newFundService(repo, &mockListingServiceIF{}, es, &mockAccountServiceIF{}, &mockCurrencyRepo{})
	// sort by unknown field falls through to ID comparison
	items, err := svc.ListFunds(ctx, domain.FundFilter{SortBy: "unknown", SortOrder: "ASC"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), items[0].ID)
}

func TestListFunds_NoSort(t *testing.T) {
	repo := &mockInvestmentFundRepo{}
	es := &mockExchangeServiceIF{}
	ctx := context.Background()

	funds := []domain.InvestmentFund{{ID: 1}, {ID: 2}}
	repo.On("List", ctx).Return(funds, nil)
	es.On("GetRates", ctx).Return([]domain.ExchangeRate{}, nil)
	repo.On("GetSecurities", ctx, int64(1)).Return([]domain.FundSecurity{}, nil)
	repo.On("GetSecurities", ctx, int64(2)).Return([]domain.FundSecurity{}, nil)
	repo.On("GetTotalInvested", ctx, int64(1)).Return(float64(0), nil)
	repo.On("GetTotalInvested", ctx, int64(2)).Return(float64(0), nil)

	svc := newFundService(repo, &mockListingServiceIF{}, es, &mockAccountServiceIF{}, &mockCurrencyRepo{})
	items, err := svc.ListFunds(ctx, domain.FundFilter{})
	require.NoError(t, err)
	assert.Len(t, items, 2)
}

// ─── TransferManagerFunds ─────────────────────────────────────────────────────

func TestTransferManagerFunds_OK(t *testing.T) {
	repo := &mockInvestmentFundRepo{}
	ctx := context.Background()
	repo.On("TransferManagerFunds", ctx, int64(5), int64(1)).Return(nil)

	svc := newFundService(repo, &mockListingServiceIF{}, &mockExchangeServiceIF{}, &mockAccountServiceIF{}, &mockCurrencyRepo{})
	err := svc.TransferManagerFunds(ctx, 5, 1)
	require.NoError(t, err)
}

func TestTransferManagerFunds_Error(t *testing.T) {
	repo := &mockInvestmentFundRepo{}
	ctx := context.Background()
	repo.On("TransferManagerFunds", ctx, int64(5), int64(1)).Return(errors.New("db error"))

	svc := newFundService(repo, &mockListingServiceIF{}, &mockExchangeServiceIF{}, &mockAccountServiceIF{}, &mockCurrencyRepo{})
	err := svc.TransferManagerFunds(ctx, 5, 1)
	require.Error(t, err)
}

package service

// White-box tests for pure helpers in local_transaction_executor.go
// and basic Commit/Rollback with empty reservation sets.

import (
	"context"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"banka-backend/services/bank-service/internal/domain"
)

// ─── isBalanced ───────────────────────────────────────────────────────────────

func TestIsBalanced_Empty(t *testing.T) {
	assert.True(t, isBalanced(nil))
}

func TestIsBalanced_SingleCurrencyBalanced(t *testing.T) {
	usd := domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}}
	buyer := &domain.ForeignBankId{RoutingNumber: 1, ID: "user1"}
	seller := &domain.ForeignBankId{RoutingNumber: 1, ID: "user2"}
	postings := []domain.Posting{
		{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: buyer}, Amount: decimal.NewFromFloat(-100), Asset: usd},
		{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: seller}, Amount: decimal.NewFromFloat(100), Asset: usd},
	}
	assert.True(t, isBalanced(postings))
}

func TestIsBalanced_Unbalanced(t *testing.T) {
	usd := domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}}
	buyer := &domain.ForeignBankId{RoutingNumber: 1, ID: "user1"}
	postings := []domain.Posting{
		{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: buyer}, Amount: decimal.NewFromFloat(-50), Asset: usd},
		{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: buyer}, Amount: decimal.NewFromFloat(100), Asset: usd},
	}
	assert.False(t, isBalanced(postings))
}

func TestIsBalanced_MultiAssetEachBalanced(t *testing.T) {
	usd := domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}}
	stock := domain.Asset{Type: domain.AssetTypeStock, Stock: &domain.StockDescription{Ticker: "AAPL"}}
	a := &domain.ForeignBankId{RoutingNumber: 1, ID: "a"}
	b := &domain.ForeignBankId{RoutingNumber: 1, ID: "b"}
	postings := []domain.Posting{
		{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: a}, Amount: decimal.NewFromFloat(-100), Asset: usd},
		{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: b}, Amount: decimal.NewFromFloat(100), Asset: usd},
		{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: a}, Amount: decimal.NewFromFloat(5), Asset: stock},
		{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: b}, Amount: decimal.NewFromFloat(-5), Asset: stock},
	}
	assert.True(t, isBalanced(postings))
}

func TestIsBalanced_MultiAssetOnlyOneUnbalanced(t *testing.T) {
	usd := domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}}
	rsd := domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "RSD"}}
	a := &domain.ForeignBankId{RoutingNumber: 1, ID: "a"}
	postings := []domain.Posting{
		{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: a}, Amount: decimal.NewFromFloat(-100), Asset: usd},
		{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: a}, Amount: decimal.NewFromFloat(100), Asset: usd},
		{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: a}, Amount: decimal.NewFromFloat(50), Asset: rsd},
		// RSD not balanced
	}
	assert.False(t, isBalanced(postings))
}

// ─── assetKey ─────────────────────────────────────────────────────────────────

func TestAssetKey_Monas(t *testing.T) {
	a := domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "EUR"}}
	assert.Equal(t, "MONAS:EUR", assetKey(a))
}

func TestAssetKey_MonasNilMonAs(t *testing.T) {
	a := domain.Asset{Type: domain.AssetTypeMonas}
	assert.Equal(t, "MONAS:?", assetKey(a))
}

func TestAssetKey_Stock(t *testing.T) {
	a := domain.Asset{Type: domain.AssetTypeStock, Stock: &domain.StockDescription{Ticker: "TSLA"}}
	assert.Equal(t, "STOCK:TSLA", assetKey(a))
}

func TestAssetKey_StockNilStock(t *testing.T) {
	a := domain.Asset{Type: domain.AssetTypeStock}
	assert.Equal(t, "STOCK:?", assetKey(a))
}

func TestAssetKey_Option(t *testing.T) {
	a := domain.Asset{Type: domain.AssetTypeOption, Option: &domain.OptionDescription{
		NegotiationID: domain.ForeignBankId{RoutingNumber: 111, ID: "opt1"},
	}}
	key := assetKey(a)
	assert.Contains(t, key, "OPTION:")
	assert.Contains(t, key, "111")
	assert.Contains(t, key, "opt1")
}

func TestAssetKey_OptionNilOption(t *testing.T) {
	a := domain.Asset{Type: domain.AssetTypeOption}
	assert.Equal(t, "OPTION:?", assetKey(a))
}

// ─── MarshalTransaction / UnmarshalTransaction ────────────────────────────────

func TestMarshalUnmarshal_RoundTrip(t *testing.T) {
	buyer := &domain.ForeignBankId{RoutingNumber: 111, ID: "buyer1"}
	seller := &domain.ForeignBankId{RoutingNumber: 222, ID: "seller1"}
	tx := domain.Transaction{
		TransactionID:  domain.ForeignBankId{RoutingNumber: 111, ID: "tx1"},
		Message:        "test transfer",
		PaymentCode:    "289",
		PaymentPurpose: "TEST",
		Postings: []domain.Posting{
			{
				Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: buyer},
				Amount:  decimal.NewFromFloat(-100),
				Asset:   domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}},
			},
			{
				Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: seller},
				Amount:  decimal.NewFromFloat(100),
				Asset:   domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}},
			},
		},
	}

	payload, err := MarshalTransaction(tx)
	require.NoError(t, err)
	assert.NotEmpty(t, payload)

	restored, err := UnmarshalTransaction(payload)
	require.NoError(t, err)
	assert.Equal(t, tx.TransactionID, restored.TransactionID)
	assert.Equal(t, tx.Message, restored.Message)
	assert.Len(t, restored.Postings, 2)
}

func TestUnmarshalTransaction_InvalidJSON(t *testing.T) {
	_, err := UnmarshalTransaction("{not-json}")
	require.Error(t, err)
}

// ─── Commit with empty reservations ──────────────────────────────────────────

type mockInterbankRepoForExecutor struct{ mock.Mock }

func (m *mockInterbankRepoForExecutor) GetIncomingByIdempotence(ctx context.Context, rn int64, lk string) (*domain.InterbankMessageLog, error) {
	args := m.Called(ctx, rn, lk)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.InterbankMessageLog), args.Error(1)
}
func (m *mockInterbankRepoForExecutor) GetOutgoingByIdempotence(ctx context.Context, rn int64, lk string) (*domain.InterbankMessageLog, error) {
	args := m.Called(ctx, rn, lk)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.InterbankMessageLog), args.Error(1)
}
func (m *mockInterbankRepoForExecutor) CreateMessage(ctx context.Context, msg *domain.InterbankMessageLog) error {
	return m.Called(ctx, msg).Error(0)
}
func (m *mockInterbankRepoForExecutor) UpdateMessage(ctx context.Context, msg *domain.InterbankMessageLog) error {
	return m.Called(ctx, msg).Error(0)
}
func (m *mockInterbankRepoForExecutor) ListPendingOutgoing(ctx context.Context, limit int) ([]domain.InterbankMessageLog, error) {
	args := m.Called(ctx, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.InterbankMessageLog), args.Error(1)
}
func (m *mockInterbankRepoForExecutor) CreateTransaction(ctx context.Context, t *domain.InterbankTransaction) error {
	return m.Called(ctx, t).Error(0)
}
func (m *mockInterbankRepoForExecutor) GetTransactionByForeignID(ctx context.Context, rn int64, fid string) (*domain.InterbankTransaction, error) {
	args := m.Called(ctx, rn, fid)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.InterbankTransaction), args.Error(1)
}
func (m *mockInterbankRepoForExecutor) UpdateTransactionStatus(ctx context.Context, id int64, status domain.InterbankTxStatus, step, reason string) error {
	return m.Called(ctx, id, status, step, reason).Error(0)
}
func (m *mockInterbankRepoForExecutor) CreateReservation(ctx context.Context, r *domain.InterbankReservation) error {
	return m.Called(ctx, r).Error(0)
}
func (m *mockInterbankRepoForExecutor) ListReservationsByTx(ctx context.Context, txID int64) ([]domain.InterbankReservation, error) {
	args := m.Called(ctx, txID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.InterbankReservation), args.Error(1)
}
func (m *mockInterbankRepoForExecutor) CreateNegotiation(ctx context.Context, n *domain.InterbankNegotiation) error {
	return m.Called(ctx, n).Error(0)
}
func (m *mockInterbankRepoForExecutor) GetNegotiationByID(ctx context.Context, rn int64, fid string) (*domain.InterbankNegotiation, error) {
	args := m.Called(ctx, rn, fid)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.InterbankNegotiation), args.Error(1)
}
func (m *mockInterbankRepoForExecutor) UpdateNegotiation(ctx context.Context, n *domain.InterbankNegotiation) error {
	return m.Called(ctx, n).Error(0)
}
func (m *mockInterbankRepoForExecutor) ListPublicStocks(ctx context.Context) ([]domain.PublicStock, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.PublicStock), args.Error(1)
}
func (m *mockInterbankRepoForExecutor) CreateOptionContract(ctx context.Context, c *domain.InterbankOptionContract) error {
	return m.Called(ctx, c).Error(0)
}
func (m *mockInterbankRepoForExecutor) GetOptionContract(ctx context.Context, rn int64, fid string) (*domain.InterbankOptionContract, error) {
	args := m.Called(ctx, rn, fid)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.InterbankOptionContract), args.Error(1)
}
func (m *mockInterbankRepoForExecutor) UpdateOptionContractStatus(ctx context.Context, rn int64, fid, status string, usedAt *time.Time) error {
	return m.Called(ctx, rn, fid, status, usedAt).Error(0)
}
func (m *mockInterbankRepoForExecutor) ListContractsForUser(ctx context.Context, rn int64, userID string) ([]domain.InterbankOptionContract, error) {
	args := m.Called(ctx, rn, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.InterbankOptionContract), args.Error(1)
}

func TestCommit_EmptyReservations(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")

	ctx := context.Background()
	repo.On("ListReservationsByTx", ctx, int64(42)).Return([]domain.InterbankReservation{}, nil)

	dbMock.ExpectBegin()
	repo.On("UpdateTransactionStatus", ctx, int64(42), domain.TxStatusCommitted, "COMMITTED", "").Return(nil)
	dbMock.ExpectCommit()

	err := e.Commit(ctx, 42)
	require.NoError(t, err)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestCommit_ReservationsListError(t *testing.T) {
	db, _ := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")

	ctx := context.Background()
	repo.On("ListReservationsByTx", ctx, int64(42)).Return(nil, assert.AnError)

	err := e.Commit(ctx, 42)
	require.Error(t, err)
}

func TestRollback_EmptyReservations(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")

	ctx := context.Background()
	repo.On("ListReservationsByTx", ctx, int64(77)).Return([]domain.InterbankReservation{}, nil)

	dbMock.ExpectBegin()
	repo.On("UpdateTransactionStatus", ctx, int64(77), domain.TxStatusRolledBack, "ROLLED_BACK", "").Return(nil)
	dbMock.ExpectCommit()

	err := e.Rollback(ctx, 77)
	require.NoError(t, err)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestRollback_ReservationsListError(t *testing.T) {
	db, _ := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")

	ctx := context.Background()
	repo.On("ListReservationsByTx", ctx, int64(77)).Return(nil, assert.AnError)

	err := e.Rollback(ctx, 77)
	require.Error(t, err)
}

// ─── isLocalPosting ───────────────────────────────────────────────────────────

func TestIsLocalPosting_PersonLocal(t *testing.T) {
	db, _ := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")

	p := domain.Posting{
		Account: domain.TxAccount{
			Type: domain.AccountKindPerson,
			ID:   &domain.ForeignBankId{RoutingNumber: 111, ID: "user1"},
		},
	}
	local, err := e.isLocalPosting(p)
	require.NoError(t, err)
	assert.True(t, local)
}

func TestIsLocalPosting_PersonRemote(t *testing.T) {
	db, _ := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")

	p := domain.Posting{
		Account: domain.TxAccount{
			Type: domain.AccountKindPerson,
			ID:   &domain.ForeignBankId{RoutingNumber: 999, ID: "user1"},
		},
	}
	local, err := e.isLocalPosting(p)
	require.NoError(t, err)
	assert.False(t, local)
}

func TestIsLocalPosting_AccountLocal(t *testing.T) {
	db, _ := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")

	num := "265-001-01234"
	p := domain.Posting{
		Account: domain.TxAccount{Type: domain.AccountKindAccount, Num: &num},
	}
	local, err := e.isLocalPosting(p)
	require.NoError(t, err)
	assert.True(t, local)
}

func TestIsLocalPosting_AccountForeign(t *testing.T) {
	db, _ := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")

	num := "111-001-01234"
	p := domain.Posting{
		Account: domain.TxAccount{Type: domain.AccountKindAccount, Num: &num},
	}
	local, err := e.isLocalPosting(p)
	require.NoError(t, err)
	assert.False(t, local)
}

func TestIsLocalPosting_AccountNoNum_Error(t *testing.T) {
	db, _ := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")

	p := domain.Posting{Account: domain.TxAccount{Type: domain.AccountKindAccount}}
	_, err := e.isLocalPosting(p)
	require.Error(t, err)
}

func TestIsLocalPosting_PersonNoID_Error(t *testing.T) {
	db, _ := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")

	p := domain.Posting{Account: domain.TxAccount{Type: domain.AccountKindPerson}}
	_, err := e.isLocalPosting(p)
	require.Error(t, err)
}

func TestIsLocalPosting_UnknownKind_Error(t *testing.T) {
	db, _ := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")

	p := domain.Posting{Account: domain.TxAccount{Type: "UNKNOWN"}}
	_, err := e.isLocalPosting(p)
	require.Error(t, err)
}

// ─── pCopy / ptrTime ──────────────────────────────────────────────────────────

func TestPCopy(t *testing.T) {
	buyer := &domain.ForeignBankId{RoutingNumber: 111, ID: "u1"}
	original := domain.Posting{
		Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: buyer},
		Amount:  decimal.NewFromFloat(50),
		Asset:   domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}},
	}
	cp := pCopy(original)
	require.NotNil(t, cp)
	assert.Equal(t, original.Amount, cp.Amount)
	assert.Equal(t, original.Account.Type, cp.Account.Type)
}

func TestPtrTime(t *testing.T) {
	now := time.Now().UTC()
	p := ptrTime(now)
	require.NotNil(t, p)
	assert.Equal(t, now, *p)
}

// ─── validatePosting — PERSON + MONAS (no DB) ─────────────────────────────────

func TestValidatePosting_PersonMonas_Valid(t *testing.T) {
	db, _ := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	p := domain.Posting{
		Account: domain.TxAccount{
			Type: domain.AccountKindPerson,
			ID:   &domain.ForeignBankId{RoutingNumber: 111, ID: "u1"},
		},
		Amount: decimal.NewFromFloat(100),
		Asset:  domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}},
	}
	reason := e.validatePosting(ctx, p)
	assert.Nil(t, reason)
}

func TestValidatePosting_PersonMonas_NilMonAs(t *testing.T) {
	db, _ := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	p := domain.Posting{
		Account: domain.TxAccount{
			Type: domain.AccountKindPerson,
			ID:   &domain.ForeignBankId{RoutingNumber: 111, ID: "u1"},
		},
		Amount: decimal.NewFromFloat(100),
		Asset:  domain.Asset{Type: domain.AssetTypeMonas}, // nil MonAs
	}
	reason := e.validatePosting(ctx, p)
	require.NotNil(t, reason)
	assert.Equal(t, domain.NoReasonNoSuchAsset, reason.Reason)
}

func TestValidatePosting_PersonStock_Valid(t *testing.T) {
	db, _ := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	// Positive amount = no DB check needed
	p := domain.Posting{
		Account: domain.TxAccount{
			Type: domain.AccountKindPerson,
			ID:   &domain.ForeignBankId{RoutingNumber: 111, ID: "u1"},
		},
		Amount: decimal.NewFromFloat(5),
		Asset:  domain.Asset{Type: domain.AssetTypeStock, Stock: &domain.StockDescription{Ticker: "AAPL"}},
	}
	reason := e.validatePosting(ctx, p)
	assert.Nil(t, reason)
}

func TestValidatePosting_PersonStock_EmptyTicker(t *testing.T) {
	db, _ := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	p := domain.Posting{
		Account: domain.TxAccount{
			Type: domain.AccountKindPerson,
			ID:   &domain.ForeignBankId{RoutingNumber: 111, ID: "u1"},
		},
		Amount: decimal.NewFromFloat(5),
		Asset:  domain.Asset{Type: domain.AssetTypeStock, Stock: &domain.StockDescription{Ticker: ""}},
	}
	reason := e.validatePosting(ctx, p)
	require.NotNil(t, reason)
	assert.Equal(t, domain.NoReasonNoSuchAsset, reason.Reason)
}

func TestValidatePosting_PersonOption_Valid(t *testing.T) {
	db, _ := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	p := domain.Posting{
		Account: domain.TxAccount{
			Type: domain.AccountKindPerson,
			ID:   &domain.ForeignBankId{RoutingNumber: 111, ID: "u1"},
		},
		Amount: decimal.NewFromFloat(5),
		Asset:  domain.Asset{Type: domain.AssetTypeOption},
	}
	reason := e.validatePosting(ctx, p)
	assert.Nil(t, reason)
}

func TestValidatePosting_OptionAccount_NilID(t *testing.T) {
	db, _ := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	p := domain.Posting{
		Account: domain.TxAccount{Type: domain.AccountKindOption},
		Amount:  decimal.NewFromFloat(5),
		Asset:   domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}},
	}
	reason := e.validatePosting(ctx, p)
	require.NotNil(t, reason)
	assert.Equal(t, domain.NoReasonOptionNegotiationNotFound, reason.Reason)
}

func TestValidatePosting_OptionAccount_ContractNotFound(t *testing.T) {
	db, _ := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")
	ctx := context.Background()

	repo.On("GetOptionContract", ctx, int64(111), "opt1").Return(nil, nil)

	p := domain.Posting{
		Account: domain.TxAccount{
			Type: domain.AccountKindOption,
			ID:   &domain.ForeignBankId{RoutingNumber: 111, ID: "opt1"},
		},
		Amount: decimal.NewFromFloat(5),
		Asset:  domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}},
	}
	reason := e.validatePosting(ctx, p)
	require.NotNil(t, reason)
	assert.Equal(t, domain.NoReasonOptionNegotiationNotFound, reason.Reason)
}

func TestValidatePosting_OptionAccount_NotActive(t *testing.T) {
	db, _ := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")
	ctx := context.Background()

	contract := &domain.InterbankOptionContract{
		ID:             1,
		Status:         "EXERCISED",
		SettlementDate: time.Now().Add(24 * time.Hour),
	}
	repo.On("GetOptionContract", ctx, int64(111), "opt1").Return(contract, nil)

	p := domain.Posting{
		Account: domain.TxAccount{
			Type: domain.AccountKindOption,
			ID:   &domain.ForeignBankId{RoutingNumber: 111, ID: "opt1"},
		},
		Amount: decimal.NewFromFloat(5),
		Asset:  domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}},
	}
	reason := e.validatePosting(ctx, p)
	require.NotNil(t, reason)
	assert.Equal(t, domain.NoReasonOptionUsedOrExpired, reason.Reason)
}

func TestValidatePosting_OptionAccount_Expired(t *testing.T) {
	db, _ := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")
	ctx := context.Background()

	contract := &domain.InterbankOptionContract{
		ID:             1,
		Status:         "ACTIVE",
		SettlementDate: time.Now().Add(-24 * time.Hour), // past
	}
	repo.On("GetOptionContract", ctx, int64(111), "opt1").Return(contract, nil)

	p := domain.Posting{
		Account: domain.TxAccount{
			Type: domain.AccountKindOption,
			ID:   &domain.ForeignBankId{RoutingNumber: 111, ID: "opt1"},
		},
		Amount: decimal.NewFromFloat(5),
		Asset:  domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}},
	}
	reason := e.validatePosting(ctx, p)
	require.NotNil(t, reason)
	assert.Equal(t, domain.NoReasonOptionUsedOrExpired, reason.Reason)
}

func TestValidatePosting_OptionAccount_ZeroAmount(t *testing.T) {
	db, _ := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")
	ctx := context.Background()

	contract := &domain.InterbankOptionContract{
		ID:             1,
		Status:         "ACTIVE",
		SettlementDate: time.Now().Add(24 * time.Hour),
	}
	repo.On("GetOptionContract", ctx, int64(111), "opt1").Return(contract, nil)

	p := domain.Posting{
		Account: domain.TxAccount{
			Type: domain.AccountKindOption,
			ID:   &domain.ForeignBankId{RoutingNumber: 111, ID: "opt1"},
		},
		Amount: decimal.NewFromFloat(0), // zero
		Asset:  domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}},
	}
	reason := e.validatePosting(ctx, p)
	require.NotNil(t, reason)
	assert.Equal(t, domain.NoReasonOptionAmountIncorrect, reason.Reason)
}

func TestValidatePosting_OptionAccount_Valid(t *testing.T) {
	db, _ := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")
	ctx := context.Background()

	contract := &domain.InterbankOptionContract{
		ID:             1,
		Status:         "ACTIVE",
		SettlementDate: time.Now().Add(48 * time.Hour),
	}
	repo.On("GetOptionContract", ctx, int64(111), "opt1").Return(contract, nil)

	p := domain.Posting{
		Account: domain.TxAccount{
			Type: domain.AccountKindOption,
			ID:   &domain.ForeignBankId{RoutingNumber: 111, ID: "opt1"},
		},
		Amount: decimal.NewFromFloat(5),
		Asset:  domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}},
	}
	reason := e.validatePosting(ctx, p)
	assert.Nil(t, reason)
}

// ─── Prepare ──────────────────────────────────────────────────────────────────

func TestPrepare_Unbalanced(t *testing.T) {
	db, _ := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	usd := domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}}
	a := &domain.ForeignBankId{RoutingNumber: 222, ID: "u1"}
	tx := domain.Transaction{
		Postings: []domain.Posting{
			{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: a}, Amount: decimal.NewFromInt(100), Asset: usd},
		},
	}
	vote, err := e.Prepare(ctx, 1, tx)
	require.NoError(t, err)
	assert.Equal(t, domain.VoteNo, vote.Vote)
	require.Len(t, vote.Reasons, 1)
	assert.Equal(t, domain.NoReasonUnbalancedTx, vote.Reasons[0].Reason)
}

func TestPrepare_AllRemote_VoteYes(t *testing.T) {
	db, dbMock := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	usd := domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}}
	a := &domain.ForeignBankId{RoutingNumber: 222, ID: "u1"}
	b := &domain.ForeignBankId{RoutingNumber: 222, ID: "u2"}
	tx := domain.Transaction{
		Postings: []domain.Posting{
			{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: a}, Amount: decimal.NewFromInt(-100), Asset: usd},
			{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: b}, Amount: decimal.NewFromInt(100), Asset: usd},
		},
	}
	dbMock.ExpectBegin()
	dbMock.ExpectCommit()

	vote, err := e.Prepare(ctx, 1, tx)
	require.NoError(t, err)
	assert.Equal(t, domain.VoteYes, vote.Vote)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestPrepare_LocalAccountNotFound_VoteNo(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")
	ctx := context.Background()

	num1 := "265-001-00001"
	num2 := "265-001-00002"
	usd := domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}}
	tx := domain.Transaction{
		Postings: []domain.Posting{
			{Account: domain.TxAccount{Type: domain.AccountKindAccount, Num: &num1}, Amount: decimal.NewFromInt(-50), Asset: usd},
			{Account: domain.TxAccount{Type: domain.AccountKindAccount, Num: &num2}, Amount: decimal.NewFromInt(50), Asset: usd},
		},
	}
	// Both SELECTs return empty rows → account not found → VoteNo (no db.Transaction call)
	cols := []string{"id", "valuta_oznaka", "stanje_racuna", "rezervisana_sredstva", "status"}
	dbMock.ExpectQuery("SELECT").WillReturnRows(dbMock.NewRows(cols))
	dbMock.ExpectQuery("SELECT").WillReturnRows(dbMock.NewRows(cols))

	vote, err := e.Prepare(ctx, 1, tx)
	require.NoError(t, err)
	assert.Equal(t, domain.VoteNo, vote.Vote)
	assert.NotEmpty(t, vote.Reasons)
}

func TestPrepare_LocalAccountSufficientFunds_VoteYes(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")
	ctx := context.Background()

	num1 := "265001000001" // sender (negative)
	num2 := "265001000002" // receiver (positive)
	usd := domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}}
	tx := domain.Transaction{
		Postings: []domain.Posting{
			{Account: domain.TxAccount{Type: domain.AccountKindAccount, Num: &num1}, Amount: decimal.NewFromInt(-100), Asset: usd},
			{Account: domain.TxAccount{Type: domain.AccountKindAccount, Num: &num2}, Amount: decimal.NewFromInt(100), Asset: usd},
		},
	}

	// validatePosting: sender account (status=AKTIVAN, USD, stanje=500, rez=0)
	cols := []string{"id", "valuta_oznaka", "stanje_racuna", "rezervisana_sredstva", "status"}
	dbMock.ExpectQuery("SELECT r.id").WillReturnRows(
		dbMock.NewRows(cols).AddRow(int64(1), "USD", "500", "0", "AKTIVAN"))
	// validatePosting: receiver account (status=AKTIVAN, USD)
	dbMock.ExpectQuery("SELECT r.id").WillReturnRows(
		dbMock.NewRows(cols).AddRow(int64(2), "USD", "200", "0", "AKTIVAN"))

	// reservation loop
	dbMock.ExpectBegin()
	// sender: UPDATE rezervisana_sredstva (negative amount)
	dbMock.ExpectExec("UPDATE core_banking.racun SET rezervisana_sredstva").WillReturnResult(sqlmock.NewResult(1, 1))
	// CreateReservation for sender and receiver
	repo.On("CreateReservation", ctx, mock.Anything).Return(nil).Times(2)
	dbMock.ExpectCommit()

	vote, err := e.Prepare(ctx, 1, tx)
	require.NoError(t, err)
	assert.Equal(t, domain.VoteYes, vote.Vote)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestPrepare_LocalAccount_InsufficientFunds_VoteNo(t *testing.T) {
	db, dbMock := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	num1 := "265001000010"
	num2 := "265001000011"
	usd := domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}}
	tx := domain.Transaction{
		Postings: []domain.Posting{
			{Account: domain.TxAccount{Type: domain.AccountKindAccount, Num: &num1}, Amount: decimal.NewFromInt(-1000), Asset: usd},
			{Account: domain.TxAccount{Type: domain.AccountKindAccount, Num: &num2}, Amount: decimal.NewFromInt(1000), Asset: usd},
		},
	}

	// sender: only 50 available (stanje=50, rez=0)
	cols := []string{"id", "valuta_oznaka", "stanje_racuna", "rezervisana_sredstva", "status"}
	dbMock.ExpectQuery("SELECT r.id").WillReturnRows(
		dbMock.NewRows(cols).AddRow(int64(1), "USD", "50", "0", "AKTIVAN"))
	// receiver: ok
	dbMock.ExpectQuery("SELECT r.id").WillReturnRows(
		dbMock.NewRows(cols).AddRow(int64(2), "USD", "200", "0", "AKTIVAN"))

	vote, err := e.Prepare(ctx, 1, tx)
	require.NoError(t, err)
	assert.Equal(t, domain.VoteNo, vote.Vote)
	assert.NotEmpty(t, vote.Reasons)
}

func TestValidatePosting_Account_CurrencyMismatch(t *testing.T) {
	db, dbMock := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	num := "265001000020"
	cols := []string{"id", "valuta_oznaka", "stanje_racuna", "rezervisana_sredstva", "status"}
	dbMock.ExpectQuery("SELECT r.id").WillReturnRows(
		dbMock.NewRows(cols).AddRow(int64(1), "EUR", "500", "0", "AKTIVAN"))

	p := domain.Posting{
		Account: domain.TxAccount{Type: domain.AccountKindAccount, Num: &num},
		Amount:  decimal.NewFromFloat(-100),
		Asset:   domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}}, // mismatch
	}
	reason := e.validatePosting(ctx, p)
	require.NotNil(t, reason)
	assert.Equal(t, domain.NoReasonUnacceptableAsset, reason.Reason)
}

func TestValidatePosting_Account_InsufficientFunds(t *testing.T) {
	db, dbMock := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	num := "265001000021"
	cols := []string{"id", "valuta_oznaka", "stanje_racuna", "rezervisana_sredstva", "status"}
	dbMock.ExpectQuery("SELECT r.id").WillReturnRows(
		dbMock.NewRows(cols).AddRow(int64(1), "USD", "50", "0", "AKTIVAN"))

	p := domain.Posting{
		Account: domain.TxAccount{Type: domain.AccountKindAccount, Num: &num},
		Amount:  decimal.NewFromFloat(-200), // want 200, only have 50
		Asset:   domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}},
	}
	reason := e.validatePosting(ctx, p)
	require.NotNil(t, reason)
	assert.Equal(t, domain.NoReasonInsufficientAsset, reason.Reason)
}

func TestPrepare_EmptyTx_VoteYes(t *testing.T) {
	db, dbMock := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	dbMock.ExpectBegin()
	dbMock.ExpectCommit()

	vote, err := e.Prepare(ctx, 99, domain.Transaction{})
	require.NoError(t, err)
	assert.Equal(t, domain.VoteYes, vote.Vote)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

// ─── Commit with non-empty reservations ──────────────────────────────────────

func TestCommit_MonasNegativeReservation(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")
	ctx := context.Background()

	num := "265001000001"
	resv := domain.InterbankReservation{
		InterbankTransactionID: 10,
		AccountKind:            domain.AccountKindAccount,
		AssetType:              domain.AssetTypeMonas,
		Amount:                 decimal.NewFromInt(-100),
		AccountNum:             &num,
		Reserved:               true,
	}
	repo.On("ListReservationsByTx", ctx, int64(10)).Return([]domain.InterbankReservation{resv}, nil)

	dbMock.ExpectBegin()
	dbMock.ExpectExec("UPDATE core_banking.racun").WillReturnResult(sqlmock.NewResult(1, 1))
	dbMock.ExpectQuery("SELECT id FROM core_banking.racun WHERE broj_racuna").
		WillReturnRows(dbMock.NewRows([]string{"id"}).AddRow(int64(42)))
	dbMock.ExpectExec("INSERT INTO core_banking.transakcija").WillReturnResult(sqlmock.NewResult(1, 1))
	repo.On("UpdateTransactionStatus", ctx, int64(10), domain.TxStatusCommitted, "COMMITTED", "").Return(nil)
	dbMock.ExpectCommit()

	err := e.Commit(ctx, 10)
	require.NoError(t, err)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestCommit_MonasPositiveReservation(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")
	ctx := context.Background()

	num := "265001000002"
	resv := domain.InterbankReservation{
		InterbankTransactionID: 11,
		AccountKind:            domain.AccountKindAccount,
		AssetType:              domain.AssetTypeMonas,
		Amount:                 decimal.NewFromInt(100),
		AccountNum:             &num,
		Reserved:               false,
	}
	repo.On("ListReservationsByTx", ctx, int64(11)).Return([]domain.InterbankReservation{resv}, nil)

	dbMock.ExpectBegin()
	dbMock.ExpectExec("UPDATE core_banking.racun").WillReturnResult(sqlmock.NewResult(1, 1))
	dbMock.ExpectQuery("SELECT id FROM core_banking.racun WHERE broj_racuna").
		WillReturnRows(dbMock.NewRows([]string{"id"}).AddRow(int64(43)))
	dbMock.ExpectExec("INSERT INTO core_banking.transakcija").WillReturnResult(sqlmock.NewResult(1, 1))
	repo.On("UpdateTransactionStatus", ctx, int64(11), domain.TxStatusCommitted, "COMMITTED", "").Return(nil)
	dbMock.ExpectCommit()

	err := e.Commit(ctx, 11)
	require.NoError(t, err)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestCommit_MonasNegative_WriteTxRowRacunNotFound(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")
	ctx := context.Background()

	num := "265001000003"
	resv := domain.InterbankReservation{
		InterbankTransactionID: 12,
		AccountKind:            domain.AccountKindAccount,
		AssetType:              domain.AssetTypeMonas,
		Amount:                 decimal.NewFromInt(-50),
		AccountNum:             &num,
		Reserved:               true,
	}
	repo.On("ListReservationsByTx", ctx, int64(12)).Return([]domain.InterbankReservation{resv}, nil)

	dbMock.ExpectBegin()
	dbMock.ExpectExec("UPDATE core_banking.racun").WillReturnResult(sqlmock.NewResult(1, 1))
	// writeTxRow: racun not found (id=0)
	dbMock.ExpectQuery("SELECT id FROM core_banking.racun WHERE broj_racuna").
		WillReturnRows(dbMock.NewRows([]string{"id"}).AddRow(int64(0)))
	dbMock.ExpectRollback()

	err := e.Commit(ctx, 12)
	require.Error(t, err)
}

// ─── Rollback with non-empty reservations ────────────────────────────────────

func TestRollback_ReservedMonasNegative(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")
	ctx := context.Background()

	num := "265001000004"
	resv := domain.InterbankReservation{
		InterbankTransactionID: 20,
		AccountKind:            domain.AccountKindAccount,
		AssetType:              domain.AssetTypeMonas,
		Amount:                 decimal.NewFromInt(-75),
		AccountNum:             &num,
		Reserved:               true,
	}
	repo.On("ListReservationsByTx", ctx, int64(20)).Return([]domain.InterbankReservation{resv}, nil)

	dbMock.ExpectBegin()
	dbMock.ExpectExec("UPDATE core_banking.racun").WillReturnResult(sqlmock.NewResult(1, 1))
	repo.On("UpdateTransactionStatus", ctx, int64(20), domain.TxStatusRolledBack, "ROLLED_BACK", "").Return(nil)
	dbMock.ExpectCommit()

	err := e.Rollback(ctx, 20)
	require.NoError(t, err)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestRollback_NotReserved_Skipped(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")
	ctx := context.Background()

	num := "265001000005"
	resv := domain.InterbankReservation{
		InterbankTransactionID: 21,
		AccountKind:            domain.AccountKindAccount,
		AssetType:              domain.AssetTypeMonas,
		Amount:                 decimal.NewFromInt(-75),
		AccountNum:             &num,
		Reserved:               false, // not reserved — skip
	}
	repo.On("ListReservationsByTx", ctx, int64(21)).Return([]domain.InterbankReservation{resv}, nil)

	dbMock.ExpectBegin()
	repo.On("UpdateTransactionStatus", ctx, int64(21), domain.TxStatusRolledBack, "ROLLED_BACK", "").Return(nil)
	dbMock.ExpectCommit()

	err := e.Rollback(ctx, 21)
	require.NoError(t, err)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

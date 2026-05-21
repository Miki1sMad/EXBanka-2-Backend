package service

// White-box tests for transaction_coordinator.go.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"banka-backend/services/bank-service/internal/domain"
)

// ─── Mock InterbankClient ─────────────────────────────────────────────────────

type mockIBClientForCoord struct{ mock.Mock }

func (m *mockIBClientForCoord) SendMessage(ctx context.Context, msg domain.InterbankMessage) (int, []byte, error) {
	args := m.Called(ctx, msg)
	b, _ := args.Get(1).([]byte)
	return args.Int(0), b, args.Error(2)
}
func (m *mockIBClientForCoord) GetPublicStock(context.Context) ([]domain.PublicStock, error) {
	return nil, nil
}
func (m *mockIBClientForCoord) CreateNegotiation(context.Context, domain.OtcOffer) (*domain.ForeignBankId, error) {
	return nil, nil
}
func (m *mockIBClientForCoord) CounterNegotiation(context.Context, domain.ForeignBankId, domain.OtcOffer) error {
	return nil
}
func (m *mockIBClientForCoord) GetNegotiation(context.Context, domain.ForeignBankId) (*domain.OtcNegotiation, error) {
	return nil, nil
}
func (m *mockIBClientForCoord) CancelNegotiation(context.Context, domain.ForeignBankId) error {
	return nil
}
func (m *mockIBClientForCoord) AcceptNegotiation(context.Context, domain.ForeignBankId) error {
	return nil
}

// ─── Coordinator test helpers ─────────────────────────────────────────────────

func newCoordSut(t *testing.T) (*mockInterbankRepoForExecutor, *mockIBClientForCoord, *TransactionCoordinator) {
	t.Helper()
	db, _ := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	client := &mockIBClientForCoord{}
	executor := &LocalTransactionExecutor{db: db, repo: repo, ourRouting: 111, accountPrefix: "265"}
	msgSvc := &InterbankMessageService{repo: repo, client: client, ourRoutingNum: 111, maxRetry: 3, backoffDuration: 30 * time.Second}
	coord := &TransactionCoordinator{
		db: db, repo: repo, executor: executor, msgSvc: msgSvc,
		ourRoutingNumber: 111, peerRoutingNumber: 222, accountPrefix: "265",
	}
	return repo, client, coord
}

func remoteBalancedTx() domain.Transaction {
	usd := domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}}
	a := &domain.ForeignBankId{RoutingNumber: 222, ID: "u1"}
	b := &domain.ForeignBankId{RoutingNumber: 222, ID: "u2"}
	return domain.Transaction{
		TransactionID: domain.ForeignBankId{RoutingNumber: 111, ID: "tx-remote"},
		Postings: []domain.Posting{
			{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: a}, Amount: decimal.NewFromInt(-100), Asset: usd},
			{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: b}, Amount: decimal.NewFromInt(100), Asset: usd},
		},
	}
}

func newTestCoordinator() *TransactionCoordinator {
	return &TransactionCoordinator{
		ourRoutingNumber:  111,
		peerRoutingNumber: 222,
		accountPrefix:     "265",
	}
}

// ─── IsLocalAccount ───────────────────────────────────────────────────────────

func TestIsLocalAccount_LocalPrefix(t *testing.T) {
	c := newTestCoordinator()
	assert.True(t, c.IsLocalAccount("265-001-01234"))
}

func TestIsLocalAccount_ForeignPrefix(t *testing.T) {
	c := newTestCoordinator()
	assert.False(t, c.IsLocalAccount("111-001-01234"))
}

func TestIsLocalAccount_Empty(t *testing.T) {
	c := newTestCoordinator()
	assert.False(t, c.IsLocalAccount(""))
}

// ─── PeerRoutingNumber ────────────────────────────────────────────────────────

func TestPeerRoutingNumber(t *testing.T) {
	c := newTestCoordinator()
	assert.Equal(t, int64(222), c.PeerRoutingNumber())
}

// ─── ptrInt64 ─────────────────────────────────────────────────────────────────

func TestPtrInt64_Zero(t *testing.T) {
	p := ptrInt64(0)
	assert.Nil(t, p)
}

func TestPtrInt64_NonZero(t *testing.T) {
	p := ptrInt64(42)
	require.NotNil(t, p)
	assert.Equal(t, int64(42), *p)
}

func TestPtrInt64_Negative(t *testing.T) {
	p := ptrInt64(-1)
	require.NotNil(t, p)
	assert.Equal(t, int64(-1), *p)
}

// ─── joinReasons ──────────────────────────────────────────────────────────────

func TestJoinReasons_Empty(t *testing.T) {
	assert.Equal(t, "", joinReasons(nil))
	assert.Equal(t, "", joinReasons([]domain.NoVoteReason{}))
}

func TestJoinReasons_Single(t *testing.T) {
	rs := []domain.NoVoteReason{{Reason: domain.NoReasonUnbalancedTx}}
	assert.Equal(t, "UNBALANCED_TX", joinReasons(rs))
}

func TestJoinReasons_Multiple(t *testing.T) {
	rs := []domain.NoVoteReason{
		{Reason: domain.NoReasonNoSuchAccount},
		{Reason: domain.NoReasonInsufficientAsset},
	}
	result := joinReasons(rs)
	assert.Contains(t, result, "NO_SUCH_ACCOUNT")
	assert.Contains(t, result, "INSUFFICIENT_ASSET")
	assert.Contains(t, result, ",")
}

// ─── transactionHasRemote ─────────────────────────────────────────────────────

func TestTransactionHasRemote_AllLocal(t *testing.T) {
	num := "265-001-01234"
	tx := domain.Transaction{
		Postings: []domain.Posting{
			{Account: domain.TxAccount{Type: domain.AccountKindAccount, Num: &num}},
		},
	}
	assert.False(t, transactionHasRemote(tx, 111, "265"))
}

func TestTransactionHasRemote_ForeignAccount(t *testing.T) {
	foreignNum := "888-001-01234"
	tx := domain.Transaction{
		Postings: []domain.Posting{
			{Account: domain.TxAccount{Type: domain.AccountKindAccount, Num: &foreignNum}},
		},
	}
	assert.True(t, transactionHasRemote(tx, 111, "265"))
}

func TestTransactionHasRemote_ForeignPerson(t *testing.T) {
	tx := domain.Transaction{
		Postings: []domain.Posting{
			{Account: domain.TxAccount{
				Type: domain.AccountKindPerson,
				ID:   &domain.ForeignBankId{RoutingNumber: 999, ID: "user1"},
			}},
		},
	}
	assert.True(t, transactionHasRemote(tx, 111, "265"))
}

func TestTransactionHasRemote_LocalPerson(t *testing.T) {
	tx := domain.Transaction{
		Postings: []domain.Posting{
			{Account: domain.TxAccount{
				Type: domain.AccountKindPerson,
				ID:   &domain.ForeignBankId{RoutingNumber: 111, ID: "user1"},
			}},
		},
	}
	assert.False(t, transactionHasRemote(tx, 111, "265"))
}

func TestTransactionHasRemote_Empty(t *testing.T) {
	assert.False(t, transactionHasRemote(domain.Transaction{}, 111, "265"))
}

func TestTransactionHasRemote_AccountNilNum_Local(t *testing.T) {
	// nil num is treated as local (not remote)
	tx := domain.Transaction{
		Postings: []domain.Posting{
			{Account: domain.TxAccount{Type: domain.AccountKindAccount, Num: nil}},
		},
	}
	assert.False(t, transactionHasRemote(tx, 111, "265"))
}

// ─── NewTransactionCoordinator ────────────────────────────────────────────────

func TestNewTransactionCoordinator_WithPrefix(t *testing.T) {
	db, _ := newGormDB(t)
	c := NewTransactionCoordinator(db, &mockInterbankRepoForExecutor{}, nil, nil, nil, 111, 222, "265")
	assert.Equal(t, "265", c.accountPrefix)
	assert.Equal(t, int64(111), c.ourRoutingNumber)
}

func TestNewTransactionCoordinator_EmptyPrefix(t *testing.T) {
	db, _ := newGormDB(t)
	c := NewTransactionCoordinator(db, &mockInterbankRepoForExecutor{}, nil, nil, nil, 123, 456, "")
	assert.Equal(t, "123", c.accountPrefix)
}

// ─── HandleIncomingMessage ────────────────────────────────────────────────────

func TestHandleIncoming_BadRequest_EmptyKey(t *testing.T) {
	_, _, coord := newCoordSut(t)
	ctx := context.Background()
	status, body, err := coord.HandleIncomingMessage(ctx, domain.InterbankMessage{
		IdempotenceKey: domain.IdempotenceKey{LocallyGeneratedKey: ""},
	})
	assert.Equal(t, 400, status)
	assert.Nil(t, body)
	require.Error(t, err)
}

func TestHandleIncoming_BadRequest_LongKey(t *testing.T) {
	_, _, coord := newCoordSut(t)
	ctx := context.Background()
	status, _, err := coord.HandleIncomingMessage(ctx, domain.InterbankMessage{
		IdempotenceKey: domain.IdempotenceKey{LocallyGeneratedKey: strings.Repeat("x", 65)},
	})
	assert.Equal(t, 400, status)
	require.Error(t, err)
}

func TestHandleIncoming_LookupError(t *testing.T) {
	repo, _, coord := newCoordSut(t)
	ctx := context.Background()
	repo.On("GetIncomingByIdempotence", ctx, int64(222), "k1").Return(nil, errors.New("db"))
	status, _, err := coord.HandleIncomingMessage(ctx, domain.InterbankMessage{
		IdempotenceKey: domain.IdempotenceKey{RoutingNumber: 222, LocallyGeneratedKey: "k1"},
	})
	assert.Equal(t, 500, status)
	require.Error(t, err)
}

func TestHandleIncoming_Idempotent_ReturnsExisting(t *testing.T) {
	repo, _, coord := newCoordSut(t)
	ctx := context.Background()
	prevCode := 200
	prevPayload := `{"vote":"YES"}`
	prev := &domain.InterbankMessageLog{ResponseStatusCode: &prevCode, ResponsePayload: &prevPayload}
	repo.On("GetIncomingByIdempotence", ctx, int64(222), "k2").Return(prev, nil)
	status, body, err := coord.HandleIncomingMessage(ctx, domain.InterbankMessage{
		IdempotenceKey: domain.IdempotenceKey{RoutingNumber: 222, LocallyGeneratedKey: "k2"},
	})
	require.NoError(t, err)
	assert.Equal(t, 200, status)
	assert.NotNil(t, body)
}

func TestHandleIncoming_RecordIncomingError(t *testing.T) {
	repo, _, coord := newCoordSut(t)
	ctx := context.Background()
	repo.On("GetIncomingByIdempotence", ctx, int64(222), "k3").Return(nil, nil)
	repo.On("CreateMessage", ctx, mock.Anything).Return(errors.New("db"))
	status, _, err := coord.HandleIncomingMessage(ctx, domain.InterbankMessage{
		MessageType:    domain.MessageNewTx,
		IdempotenceKey: domain.IdempotenceKey{RoutingNumber: 222, LocallyGeneratedKey: "k3"},
	})
	assert.Equal(t, 500, status)
	require.Error(t, err)
}

func TestHandleIncoming_NewTx_InvalidJSON(t *testing.T) {
	repo, _, coord := newCoordSut(t)
	ctx := context.Background()
	repo.On("GetIncomingByIdempotence", ctx, int64(222), "k4").Return(nil, nil)
	repo.On("CreateMessage", ctx, mock.Anything).Return(nil)
	status, _, err := coord.HandleIncomingMessage(ctx, domain.InterbankMessage{
		MessageType:    domain.MessageNewTx,
		Message:        json.RawMessage(`not-json`),
		IdempotenceKey: domain.IdempotenceKey{RoutingNumber: 222, LocallyGeneratedKey: "k4"},
	})
	assert.Equal(t, 400, status)
	require.Error(t, err)
}

func TestHandleIncoming_NewTx_PrepareNo_Unbalanced(t *testing.T) {
	repo, _, coord := newCoordSut(t)
	ctx := context.Background()

	// Unbalanced tx: one posting, no counterpart → Prepare returns VoteNo without DB
	usd := domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}}
	a := &domain.ForeignBankId{RoutingNumber: 222, ID: "u1"}
	tx := domain.Transaction{
		TransactionID: domain.ForeignBankId{RoutingNumber: 222, ID: "tx-x"},
		Postings: []domain.Posting{
			{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: a}, Amount: decimal.NewFromInt(100), Asset: usd},
		},
	}
	txJSON, _ := json.Marshal(tx)

	repo.On("GetIncomingByIdempotence", ctx, int64(222), "k5").Return(nil, nil)
	repo.On("CreateMessage", ctx, mock.Anything).Return(nil)
	repo.On("GetTransactionByForeignID", ctx, int64(222), "tx-x").Return(nil, nil)
	repo.On("CreateTransaction", ctx, mock.Anything).Return(nil)
	repo.On("UpdateTransactionStatus", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	repo.On("UpdateMessage", ctx, mock.Anything).Return(nil)

	status, body, err := coord.HandleIncomingMessage(ctx, domain.InterbankMessage{
		MessageType:    domain.MessageNewTx,
		Message:        txJSON,
		IdempotenceKey: domain.IdempotenceKey{RoutingNumber: 222, LocallyGeneratedKey: "k5"},
	})
	require.NoError(t, err)
	assert.Equal(t, 200, status)
	vote, ok := body.(domain.TransactionVote)
	require.True(t, ok)
	assert.Equal(t, domain.VoteNo, vote.Vote)
}

func TestHandleIncoming_CommitTx_InvalidJSON(t *testing.T) {
	repo, _, coord := newCoordSut(t)
	ctx := context.Background()
	repo.On("GetIncomingByIdempotence", ctx, int64(222), "k6").Return(nil, nil)
	repo.On("CreateMessage", ctx, mock.Anything).Return(nil)
	status, _, err := coord.HandleIncomingMessage(ctx, domain.InterbankMessage{
		MessageType:    domain.MessageCommitTx,
		Message:        json.RawMessage(`bad`),
		IdempotenceKey: domain.IdempotenceKey{RoutingNumber: 222, LocallyGeneratedKey: "k6"},
	})
	assert.Equal(t, 400, status)
	require.Error(t, err)
}

func TestHandleIncoming_CommitTx_NotFound(t *testing.T) {
	repo, _, coord := newCoordSut(t)
	ctx := context.Background()
	ct := domain.CommitTransaction{TransactionID: domain.ForeignBankId{RoutingNumber: 111, ID: "tx-c"}}
	ctJSON, _ := json.Marshal(ct)
	repo.On("GetIncomingByIdempotence", ctx, int64(222), "k7").Return(nil, nil)
	repo.On("CreateMessage", ctx, mock.Anything).Return(nil)
	repo.On("GetTransactionByForeignID", ctx, int64(111), "tx-c").Return(nil, nil)
	status, _, err := coord.HandleIncomingMessage(ctx, domain.InterbankMessage{
		MessageType:    domain.MessageCommitTx,
		Message:        ctJSON,
		IdempotenceKey: domain.IdempotenceKey{RoutingNumber: 222, LocallyGeneratedKey: "k7"},
	})
	assert.Equal(t, 404, status)
	require.Error(t, err)
}

func TestHandleIncoming_CommitTx_AlreadyCommitted(t *testing.T) {
	repo, _, coord := newCoordSut(t)
	ctx := context.Background()
	ct := domain.CommitTransaction{TransactionID: domain.ForeignBankId{RoutingNumber: 111, ID: "tx-d"}}
	ctJSON, _ := json.Marshal(ct)
	ibTx := &domain.InterbankTransaction{ID: 5, Status: domain.TxStatusCommitted}
	repo.On("GetIncomingByIdempotence", ctx, int64(222), "k8").Return(nil, nil)
	repo.On("CreateMessage", ctx, mock.Anything).Return(nil)
	repo.On("GetTransactionByForeignID", ctx, int64(111), "tx-d").Return(ibTx, nil)
	repo.On("UpdateMessage", ctx, mock.Anything).Return(nil)
	status, _, err := coord.HandleIncomingMessage(ctx, domain.InterbankMessage{
		MessageType:    domain.MessageCommitTx,
		Message:        ctJSON,
		IdempotenceKey: domain.IdempotenceKey{RoutingNumber: 222, LocallyGeneratedKey: "k8"},
	})
	require.NoError(t, err)
	assert.Equal(t, 204, status)
}

func TestHandleIncoming_CommitTx_Success(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	executor := &LocalTransactionExecutor{db: db, repo: repo, ourRouting: 111, accountPrefix: "265"}
	msgSvc := &InterbankMessageService{repo: repo, client: &mockIBClientForCoord{}, ourRoutingNum: 111, maxRetry: 3, backoffDuration: 30 * time.Second}
	coord := &TransactionCoordinator{db: db, repo: repo, executor: executor, msgSvc: msgSvc, ourRoutingNumber: 111, peerRoutingNumber: 222, accountPrefix: "265"}
	ctx := context.Background()

	ct := domain.CommitTransaction{TransactionID: domain.ForeignBankId{RoutingNumber: 111, ID: "tx-e"}}
	ctJSON, _ := json.Marshal(ct)
	ibTx := &domain.InterbankTransaction{ID: 10, Status: domain.TxStatusPrepared}

	repo.On("GetIncomingByIdempotence", ctx, int64(222), "k9").Return(nil, nil)
	repo.On("CreateMessage", ctx, mock.Anything).Return(nil)
	repo.On("GetTransactionByForeignID", ctx, int64(111), "tx-e").Return(ibTx, nil)
	repo.On("ListReservationsByTx", ctx, int64(10)).Return([]domain.InterbankReservation{}, nil)
	dbMock.ExpectBegin()
	repo.On("UpdateTransactionStatus", ctx, int64(10), domain.TxStatusCommitted, "COMMITTED", "").Return(nil)
	dbMock.ExpectCommit()
	repo.On("UpdateMessage", ctx, mock.Anything).Return(nil)

	status, _, err := coord.HandleIncomingMessage(ctx, domain.InterbankMessage{
		MessageType:    domain.MessageCommitTx,
		Message:        ctJSON,
		IdempotenceKey: domain.IdempotenceKey{RoutingNumber: 222, LocallyGeneratedKey: "k9"},
	})
	require.NoError(t, err)
	assert.Equal(t, 204, status)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestHandleIncoming_RollbackTx_NotFound(t *testing.T) {
	repo, _, coord := newCoordSut(t)
	ctx := context.Background()
	rt := domain.RollbackTransaction{TransactionID: domain.ForeignBankId{RoutingNumber: 111, ID: "tx-rb1"}}
	rtJSON, _ := json.Marshal(rt)
	repo.On("GetIncomingByIdempotence", ctx, int64(222), "k10").Return(nil, nil)
	repo.On("CreateMessage", ctx, mock.Anything).Return(nil)
	repo.On("GetTransactionByForeignID", ctx, int64(111), "tx-rb1").Return(nil, nil)
	repo.On("UpdateMessage", ctx, mock.Anything).Return(nil)
	status, _, err := coord.HandleIncomingMessage(ctx, domain.InterbankMessage{
		MessageType:    domain.MessageRollbackTx,
		Message:        rtJSON,
		IdempotenceKey: domain.IdempotenceKey{RoutingNumber: 222, LocallyGeneratedKey: "k10"},
	})
	require.NoError(t, err)
	assert.Equal(t, 204, status)
}

func TestHandleIncoming_RollbackTx_AlreadyRolledBack(t *testing.T) {
	repo, _, coord := newCoordSut(t)
	ctx := context.Background()
	rt := domain.RollbackTransaction{TransactionID: domain.ForeignBankId{RoutingNumber: 111, ID: "tx-rb2"}}
	rtJSON, _ := json.Marshal(rt)
	ibTx := &domain.InterbankTransaction{ID: 6, Status: domain.TxStatusRolledBack}
	repo.On("GetIncomingByIdempotence", ctx, int64(222), "k11").Return(nil, nil)
	repo.On("CreateMessage", ctx, mock.Anything).Return(nil)
	repo.On("GetTransactionByForeignID", ctx, int64(111), "tx-rb2").Return(ibTx, nil)
	repo.On("UpdateMessage", ctx, mock.Anything).Return(nil)
	status, _, err := coord.HandleIncomingMessage(ctx, domain.InterbankMessage{
		MessageType:    domain.MessageRollbackTx,
		Message:        rtJSON,
		IdempotenceKey: domain.IdempotenceKey{RoutingNumber: 222, LocallyGeneratedKey: "k11"},
	})
	require.NoError(t, err)
	assert.Equal(t, 204, status)
}

func TestHandleIncoming_RollbackTx_Success(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	executor := &LocalTransactionExecutor{db: db, repo: repo, ourRouting: 111, accountPrefix: "265"}
	msgSvc := &InterbankMessageService{repo: repo, client: &mockIBClientForCoord{}, ourRoutingNum: 111, maxRetry: 3, backoffDuration: 30 * time.Second}
	coord := &TransactionCoordinator{db: db, repo: repo, executor: executor, msgSvc: msgSvc, ourRoutingNumber: 111, peerRoutingNumber: 222, accountPrefix: "265"}
	ctx := context.Background()

	rt := domain.RollbackTransaction{TransactionID: domain.ForeignBankId{RoutingNumber: 111, ID: "tx-rb3"}}
	rtJSON, _ := json.Marshal(rt)
	ibTx := &domain.InterbankTransaction{ID: 20, Status: domain.TxStatusPrepared}

	repo.On("GetIncomingByIdempotence", ctx, int64(222), "k12").Return(nil, nil)
	repo.On("CreateMessage", ctx, mock.Anything).Return(nil)
	repo.On("GetTransactionByForeignID", ctx, int64(111), "tx-rb3").Return(ibTx, nil)
	repo.On("ListReservationsByTx", ctx, int64(20)).Return([]domain.InterbankReservation{}, nil)
	dbMock.ExpectBegin()
	repo.On("UpdateTransactionStatus", ctx, int64(20), domain.TxStatusRolledBack, "ROLLED_BACK", "").Return(nil)
	dbMock.ExpectCommit()
	repo.On("UpdateMessage", ctx, mock.Anything).Return(nil)

	status, _, err := coord.HandleIncomingMessage(ctx, domain.InterbankMessage{
		MessageType:    domain.MessageRollbackTx,
		Message:        rtJSON,
		IdempotenceKey: domain.IdempotenceKey{RoutingNumber: 222, LocallyGeneratedKey: "k12"},
	})
	require.NoError(t, err)
	assert.Equal(t, 204, status)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestHandleIncoming_UnknownMessageType(t *testing.T) {
	repo, _, coord := newCoordSut(t)
	ctx := context.Background()
	repo.On("GetIncomingByIdempotence", ctx, int64(222), "k13").Return(nil, nil)
	repo.On("CreateMessage", ctx, mock.Anything).Return(nil)
	status, _, err := coord.HandleIncomingMessage(ctx, domain.InterbankMessage{
		MessageType:    "UNKNOWN_TYPE",
		Message:        json.RawMessage(`{}`),
		IdempotenceKey: domain.IdempotenceKey{RoutingNumber: 222, LocallyGeneratedKey: "k13"},
	})
	assert.Equal(t, 400, status)
	require.Error(t, err)
}

// ─── InitiateInterbankTransaction ─────────────────────────────────────────────

func TestInitiateInterbankTx_CreateTransactionError(t *testing.T) {
	repo, _, coord := newCoordSut(t)
	ctx := context.Background()
	repo.On("CreateTransaction", ctx, mock.Anything).Return(errors.New("db"))
	_, err := coord.InitiateInterbankTransaction(ctx, remoteBalancedTx(), nil)
	require.Error(t, err)
}

func TestInitiateInterbankTx_PrepareNo_Unbalanced(t *testing.T) {
	repo, _, coord := newCoordSut(t)
	ctx := context.Background()
	repo.On("CreateTransaction", ctx, mock.Anything).Return(nil)
	repo.On("UpdateTransactionStatus", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	usd := domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}}
	a := &domain.ForeignBankId{RoutingNumber: 222, ID: "u1"}
	unbalanced := domain.Transaction{
		TransactionID: domain.ForeignBankId{RoutingNumber: 111, ID: "tx-unbal"},
		Postings: []domain.Posting{
			{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: a}, Amount: decimal.NewFromInt(100), Asset: usd},
		},
	}
	_, err := coord.InitiateInterbankTransaction(ctx, unbalanced, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NO")
}

func TestInitiateInterbankTx_AllLocal_CommitSuccess(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	executor := &LocalTransactionExecutor{db: db, repo: repo, ourRouting: 111, accountPrefix: "265"}
	msgSvc := &InterbankMessageService{repo: repo, client: &mockIBClientForCoord{}, ourRoutingNum: 111, maxRetry: 3, backoffDuration: 30 * time.Second}
	coord := &TransactionCoordinator{db: db, repo: repo, executor: executor, msgSvc: msgSvc, ourRoutingNumber: 111, peerRoutingNumber: 222, accountPrefix: "265"}
	ctx := context.Background()

	// Empty tx: balanced (trivially), no postings → transactionHasRemote=false → local commit
	emptyTx := domain.Transaction{TransactionID: domain.ForeignBankId{RoutingNumber: 111, ID: "tx-local"}}
	repo.On("CreateTransaction", ctx, mock.Anything).Return(nil)
	repo.On("UpdateTransactionStatus", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	repo.On("ListReservationsByTx", ctx, mock.Anything).Return([]domain.InterbankReservation{}, nil)
	// Prepare calls db.Transaction for reservation loop
	dbMock.ExpectBegin()
	dbMock.ExpectCommit()
	// Commit calls db.Transaction
	dbMock.ExpectBegin()
	dbMock.ExpectCommit()

	ibTx, err := coord.InitiateInterbankTransaction(ctx, emptyTx, nil)
	require.NoError(t, err)
	assert.NotNil(t, ibTx)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestInitiateInterbankTx_HasRemote_EnqueueError(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	executor := &LocalTransactionExecutor{db: db, repo: repo, ourRouting: 111, accountPrefix: "265"}
	msgSvc := &InterbankMessageService{repo: repo, client: &mockIBClientForCoord{}, ourRoutingNum: 111, maxRetry: 3, backoffDuration: 30 * time.Second}
	coord := &TransactionCoordinator{db: db, repo: repo, executor: executor, msgSvc: msgSvc, ourRoutingNumber: 111, peerRoutingNumber: 222, accountPrefix: "265"}
	ctx := context.Background()

	tx := remoteBalancedTx()
	repo.On("CreateTransaction", ctx, mock.Anything).Return(nil)
	repo.On("UpdateTransactionStatus", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	// Prepare: no local postings, still needs db.Transaction for reservation loop
	dbMock.ExpectBegin()
	dbMock.ExpectCommit()
	// EnqueueOutgoing → GetOutgoingByIdempotence fails
	repo.On("GetOutgoingByIdempotence", ctx, mock.Anything, mock.Anything).Return(nil, errors.New("db"))
	// Rollback after enqueue fail (db.Transaction)
	repo.On("ListReservationsByTx", ctx, mock.Anything).Return([]domain.InterbankReservation{}, nil)
	dbMock.ExpectBegin()
	dbMock.ExpectCommit()
	_ = dbMock // used above

	_, err := coord.InitiateInterbankTransaction(ctx, tx, nil)
	require.Error(t, err)
}

func TestInitiateInterbankTx_HasRemote_SendError(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	client := &mockIBClientForCoord{}
	executor := &LocalTransactionExecutor{db: db, repo: repo, ourRouting: 111, accountPrefix: "265"}
	msgSvc := &InterbankMessageService{repo: repo, client: client, ourRoutingNum: 111, maxRetry: 3, backoffDuration: 30 * time.Second}
	coord := &TransactionCoordinator{db: db, repo: repo, executor: executor, msgSvc: msgSvc, ourRoutingNumber: 111, peerRoutingNumber: 222, accountPrefix: "265"}
	ctx := context.Background()

	tx := remoteBalancedTx()
	repo.On("CreateTransaction", ctx, mock.Anything).Return(nil)
	repo.On("UpdateTransactionStatus", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	dbMock.ExpectBegin()
	dbMock.ExpectCommit()
	// EnqueueOutgoing succeeds
	repo.On("GetOutgoingByIdempotence", ctx, mock.Anything, mock.Anything).Return(nil, nil)
	repo.On("CreateMessage", ctx, mock.Anything).Return(nil)
	// SendMessage → network error
	client.On("SendMessage", ctx, mock.Anything).Return(0, nil, errors.New("network"))
	repo.On("UpdateMessage", ctx, mock.Anything).Return(nil)
	// Rollback
	repo.On("ListReservationsByTx", ctx, mock.Anything).Return([]domain.InterbankReservation{}, nil)
	dbMock.ExpectBegin()
	dbMock.ExpectCommit()

	_, err := coord.InitiateInterbankTransaction(ctx, tx, nil)
	require.Error(t, err)
}

func TestInitiateInterbankTx_HasRemote_RemoteVoteNo(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	client := &mockIBClientForCoord{}
	executor := &LocalTransactionExecutor{db: db, repo: repo, ourRouting: 111, accountPrefix: "265"}
	msgSvc := &InterbankMessageService{repo: repo, client: client, ourRoutingNum: 111, maxRetry: 3, backoffDuration: 30 * time.Second}
	coord := &TransactionCoordinator{db: db, repo: repo, executor: executor, msgSvc: msgSvc, ourRoutingNumber: 111, peerRoutingNumber: 222, accountPrefix: "265"}
	ctx := context.Background()

	tx := remoteBalancedTx()
	repo.On("CreateTransaction", ctx, mock.Anything).Return(nil)
	repo.On("UpdateTransactionStatus", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	dbMock.ExpectBegin()
	dbMock.ExpectCommit()
	repo.On("GetOutgoingByIdempotence", ctx, mock.Anything, mock.Anything).Return(nil, nil)
	repo.On("CreateMessage", ctx, mock.Anything).Return(nil)

	// Remote responds 200 with VoteNo
	voteNoBody, _ := json.Marshal(domain.TransactionVote{Vote: domain.VoteNo, Reasons: []domain.NoVoteReason{{Reason: domain.NoReasonInsufficientAsset}}})
	client.On("SendMessage", ctx, mock.Anything).Return(200, voteNoBody, nil)
	repo.On("UpdateMessage", ctx, mock.Anything).Return(nil)

	// Rollback after VoteNo (db.Transaction)
	repo.On("ListReservationsByTx", ctx, mock.Anything).Return([]domain.InterbankReservation{}, nil)
	dbMock.ExpectBegin()
	dbMock.ExpectCommit()
	// ROLLBACK_TX enqueue attempt (ignored if fails or succeeds)
	repo.On("GetOutgoingByIdempotence", ctx, mock.Anything, mock.Anything).Return(nil, errors.New("skip")).Maybe()
	repo.On("CreateMessage", ctx, mock.Anything).Return(nil).Maybe()

	_, err := coord.InitiateInterbankTransaction(ctx, tx, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "odbila")
}

func TestInitiateInterbankTx_HasRemote_FullSuccess(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	client := &mockIBClientForCoord{}
	executor := &LocalTransactionExecutor{db: db, repo: repo, ourRouting: 111, accountPrefix: "265"}
	msgSvc := &InterbankMessageService{repo: repo, client: client, ourRoutingNum: 111, maxRetry: 3, backoffDuration: 30 * time.Second}
	coord := &TransactionCoordinator{db: db, repo: repo, executor: executor, msgSvc: msgSvc, ourRoutingNumber: 111, peerRoutingNumber: 222, accountPrefix: "265"}
	ctx := context.Background()

	tx := remoteBalancedTx()
	repo.On("CreateTransaction", ctx, mock.Anything).Return(nil)
	repo.On("UpdateTransactionStatus", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	dbMock.ExpectBegin()
	dbMock.ExpectCommit()
	repo.On("GetOutgoingByIdempotence", ctx, mock.Anything, mock.Anything).Return(nil, nil)
	repo.On("CreateMessage", ctx, mock.Anything).Return(nil)

	// Remote responds 200 with VoteYes
	voteYesBody, _ := json.Marshal(domain.TransactionVote{Vote: domain.VoteYes})
	client.On("SendMessage", ctx, mock.Anything).Return(200, voteYesBody, nil)
	repo.On("UpdateMessage", ctx, mock.Anything).Return(nil)

	// Local commit after VoteYes (db.Transaction)
	repo.On("ListReservationsByTx", ctx, mock.Anything).Return([]domain.InterbankReservation{}, nil)
	dbMock.ExpectBegin()
	dbMock.ExpectCommit()
	// COMMIT_TX enqueue (second GetOutgoing + CreateMessage + second SendMessage)
	repo.On("GetOutgoingByIdempotence", ctx, mock.Anything, mock.Anything).Return(nil, nil).Maybe()

	ibTx, err := coord.InitiateInterbankTransaction(ctx, tx, nil)
	require.NoError(t, err)
	assert.NotNil(t, ibTx)
}

// ─── InitiateInterbankPayment ─────────────────────────────────────────────────

func TestInitiateInterbankPayment_InvalidSenderAccountID(t *testing.T) {
	_, _, coord := newCoordSut(t)
	ctx := context.Background()
	_, err := coord.InitiateInterbankPayment(ctx, domain.InterbankPaymentInput{
		SenderAccountID:    0,
		RecipientAccountNo: "222-001-00001",
		Amount:             decimal.NewFromInt(100),
		Currency:           "USD",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SenderAccountID")
}

func TestInitiateInterbankPayment_EmptyRecipientAccountNo(t *testing.T) {
	_, _, coord := newCoordSut(t)
	ctx := context.Background()
	_, err := coord.InitiateInterbankPayment(ctx, domain.InterbankPaymentInput{
		SenderAccountID:    1,
		RecipientAccountNo: "",
		Amount:             decimal.NewFromInt(100),
		Currency:           "USD",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "primaoca")
}

func TestInitiateInterbankPayment_ZeroAmount(t *testing.T) {
	_, _, coord := newCoordSut(t)
	ctx := context.Background()
	_, err := coord.InitiateInterbankPayment(ctx, domain.InterbankPaymentInput{
		SenderAccountID:    1,
		RecipientAccountNo: "222-001-00001",
		Amount:             decimal.Zero,
		Currency:           "USD",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "iznos")
}

func TestInitiateInterbankPayment_EmptyCurrency(t *testing.T) {
	_, _, coord := newCoordSut(t)
	ctx := context.Background()
	_, err := coord.InitiateInterbankPayment(ctx, domain.InterbankPaymentInput{
		SenderAccountID:    1,
		RecipientAccountNo: "222-001-00001",
		Amount:             decimal.NewFromInt(100),
		Currency:           "",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "valuta")
}

func TestInitiateInterbankPayment_AccountNotFound(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	executor := &LocalTransactionExecutor{db: db, repo: repo, ourRouting: 111, accountPrefix: "265"}
	msgSvc := &InterbankMessageService{repo: repo, client: &mockIBClientForCoord{}, ourRoutingNum: 111, maxRetry: 3, backoffDuration: 30 * time.Second}
	coord := &TransactionCoordinator{db: db, repo: repo, executor: executor, msgSvc: msgSvc, ourRoutingNumber: 111, peerRoutingNumber: 222, accountPrefix: "265"}
	ctx := context.Background()

	dbMock.ExpectQuery("SELECT r.broj_racuna").
		WillReturnRows(dbMock.NewRows([]string{"broj_racuna", "oznaka"}))

	_, err := coord.InitiateInterbankPayment(ctx, domain.InterbankPaymentInput{
		SenderAccountID:    5,
		SenderUserID:       1,
		RecipientAccountNo: "222-001-00001",
		Amount:             decimal.NewFromInt(100),
		Currency:           "USD",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pošiljaočev")
}

func TestInitiateInterbankPayment_CurrencyMismatch(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	executor := &LocalTransactionExecutor{db: db, repo: repo, ourRouting: 111, accountPrefix: "265"}
	msgSvc := &InterbankMessageService{repo: repo, client: &mockIBClientForCoord{}, ourRoutingNum: 111, maxRetry: 3, backoffDuration: 30 * time.Second}
	coord := &TransactionCoordinator{db: db, repo: repo, executor: executor, msgSvc: msgSvc, ourRoutingNumber: 111, peerRoutingNumber: 222, accountPrefix: "265"}
	ctx := context.Background()

	dbMock.ExpectQuery("SELECT r.broj_racuna").
		WillReturnRows(dbMock.NewRows([]string{"broj_racuna", "oznaka"}).AddRow("265001000001", "EUR"))

	_, err := coord.InitiateInterbankPayment(ctx, domain.InterbankPaymentInput{
		SenderAccountID:    5,
		SenderUserID:       1,
		RecipientAccountNo: "222-001-00001",
		Amount:             decimal.NewFromInt(100),
		Currency:           "USD",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "valuta")
}

func TestInitiateInterbankPayment_CreateTransactionError(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	executor := &LocalTransactionExecutor{db: db, repo: repo, ourRouting: 111, accountPrefix: "265"}
	msgSvc := &InterbankMessageService{repo: repo, client: &mockIBClientForCoord{}, ourRoutingNum: 111, maxRetry: 3, backoffDuration: 30 * time.Second}
	coord := &TransactionCoordinator{db: db, repo: repo, executor: executor, msgSvc: msgSvc, ourRoutingNumber: 111, peerRoutingNumber: 222, accountPrefix: "265"}
	ctx := context.Background()

	dbMock.ExpectQuery("SELECT r.broj_racuna").
		WillReturnRows(dbMock.NewRows([]string{"broj_racuna", "oznaka"}).AddRow("265001000001", "USD"))
	repo.On("CreateTransaction", ctx, mock.Anything).Return(errors.New("db"))

	_, err := coord.InitiateInterbankPayment(ctx, domain.InterbankPaymentInput{
		SenderAccountID:    5,
		SenderUserID:       1,
		RecipientAccountNo: "222-001-00001",
		Amount:             decimal.NewFromInt(100),
		Currency:           "USD",
	})
	require.Error(t, err)
}

func TestInitiateInterbankPayment_PrepareVoteNo(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	executor := &LocalTransactionExecutor{db: db, repo: repo, ourRouting: 111, accountPrefix: "265"}
	msgSvc := &InterbankMessageService{repo: repo, client: &mockIBClientForCoord{}, ourRoutingNum: 111, maxRetry: 3, backoffDuration: 30 * time.Second}
	coord := &TransactionCoordinator{db: db, repo: repo, executor: executor, msgSvc: msgSvc, ourRoutingNumber: 111, peerRoutingNumber: 222, accountPrefix: "265"}
	ctx := context.Background()

	// Account lookup succeeds
	dbMock.ExpectQuery("SELECT r.broj_racuna").
		WillReturnRows(dbMock.NewRows([]string{"broj_racuna", "oznaka"}).AddRow("265001000001", "USD"))

	// validatePosting for local ACCOUNT posting: account not found (status empty → VoteNo)
	cols := []string{"id", "valuta_oznaka", "stanje_racuna", "rezervisana_sredstva", "status"}
	dbMock.ExpectQuery("SELECT r.id").WillReturnRows(dbMock.NewRows(cols))

	repo.On("CreateTransaction", ctx, mock.Anything).Return(nil)
	repo.On("UpdateTransactionStatus", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	_, err := coord.InitiateInterbankPayment(ctx, domain.InterbankPaymentInput{
		SenderAccountID:    5,
		SenderUserID:       1,
		RecipientAccountNo: "222-001-00001",
		Amount:             decimal.NewFromInt(100),
		Currency:           "USD",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NO")
}

// ─── InitiateInterbankPayment: post-Prepare helpers ───────────────────────────

func newCoordSutWithDB(t *testing.T) (*mockInterbankRepoForExecutor, *mockIBClientForCoord, *TransactionCoordinator, sqlmock.Sqlmock) {
	t.Helper()
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	client := &mockIBClientForCoord{}
	executor := &LocalTransactionExecutor{db: db, repo: repo, ourRouting: 111, accountPrefix: "265"}
	msgSvc := &InterbankMessageService{repo: repo, client: client, ourRoutingNum: 111, maxRetry: 3, backoffDuration: 30 * time.Second}
	coord := &TransactionCoordinator{
		db: db, repo: repo, executor: executor, msgSvc: msgSvc,
		ourRoutingNumber: 111, peerRoutingNumber: 222, accountPrefix: "265",
	}
	return repo, client, coord, dbMock
}

func standardPaymentInput() domain.InterbankPaymentInput {
	return domain.InterbankPaymentInput{
		SenderAccountID:    5,
		SenderUserID:       1,
		RecipientAccountNo: "222-001-00001",
		Amount:             decimal.NewFromInt(100),
		Currency:           "USD",
	}
}

// setupPaymentPrepareExpects adds DB mock expectations for a successful Prepare:
// sender account lookup, validatePosting, and the reservation DB transaction.
func setupPaymentPrepareExpects(dbMock sqlmock.Sqlmock) {
	dbMock.ExpectQuery("SELECT r.broj_racuna").
		WillReturnRows(dbMock.NewRows([]string{"broj_racuna", "oznaka"}).AddRow("265001000001", "USD"))
	cols := []string{"id", "valuta_oznaka", "stanje_racuna", "rezervisana_sredstva", "status"}
	dbMock.ExpectQuery("SELECT r.id").
		WillReturnRows(dbMock.NewRows(cols).AddRow(int64(1), "USD", "1000", "0", "AKTIVAN"))
	dbMock.ExpectBegin()
	dbMock.ExpectExec("rezervisana_sredstva").WillReturnResult(sqlmock.NewResult(0, 1))
	dbMock.ExpectCommit()
}

// setupRollbackExpects adds DB expectations for rolling back the sender reservation.
func setupRollbackExpects(dbMock sqlmock.Sqlmock) {
	dbMock.ExpectBegin()
	dbMock.ExpectExec("rezervisana_sredstva").WillReturnResult(sqlmock.NewResult(0, 1))
	dbMock.ExpectCommit()
}

// registerPrepareRepoMocks adds repo mock expectations common to all post-Prepare tests.
func registerPrepareRepoMocks(repo *mockInterbankRepoForExecutor, ctx context.Context) {
	repo.On("CreateTransaction", ctx, mock.Anything).Return(nil)
	repo.On("UpdateTransactionStatus", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	repo.On("CreateReservation", ctx, mock.Anything).Return(nil)
}

// testSenderReservation returns a sender-side reservation (negative, reserved) for rollback/commit tests.
func testSenderReservation() domain.InterbankReservation {
	num := "265001000001"
	return domain.InterbankReservation{
		AccountKind: domain.AccountKindAccount,
		AssetType:   domain.AssetTypeMonas,
		Amount:      decimal.NewFromInt(-100),
		AccountNum:  &num,
		Reserved:    true,
	}
}

// ─── InitiateInterbankPayment: post-Prepare paths ────────────────────────────

func TestInitiateInterbankPayment_EnqueueError(t *testing.T) {
	repo, _, coord, dbMock := newCoordSutWithDB(t)
	ctx := context.Background()

	setupPaymentPrepareExpects(dbMock)
	setupRollbackExpects(dbMock)

	registerPrepareRepoMocks(repo, ctx)
	repo.On("GetOutgoingByIdempotence", ctx, int64(111), mock.Anything).Return(nil, nil)
	repo.On("CreateMessage", ctx, mock.Anything).Return(errors.New("db"))
	repo.On("ListReservationsByTx", ctx, int64(0)).Return([]domain.InterbankReservation{testSenderReservation()}, nil)

	_, err := coord.InitiateInterbankPayment(ctx, standardPaymentInput())
	require.Error(t, err)
	repo.AssertExpectations(t)
}

func TestInitiateInterbankPayment_AwaitingRemoteVote(t *testing.T) {
	repo, client, coord, dbMock := newCoordSutWithDB(t)
	ctx := context.Background()

	setupPaymentPrepareExpects(dbMock)

	registerPrepareRepoMocks(repo, ctx)
	repo.On("GetOutgoingByIdempotence", ctx, int64(111), mock.Anything).Return(nil, nil)
	repo.On("CreateMessage", ctx, mock.Anything).Return(nil)
	repo.On("UpdateMessage", ctx, mock.Anything).Return(nil)
	client.On("SendMessage", ctx, mock.Anything).Return(202, []byte(`{}`), nil)

	_, err := coord.InitiateInterbankPayment(ctx, standardPaymentInput())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nije izglasala")
	repo.AssertExpectations(t)
	client.AssertExpectations(t)
}

func TestInitiateInterbankPayment_RemoteVoteNo(t *testing.T) {
	repo, client, coord, dbMock := newCoordSutWithDB(t)
	ctx := context.Background()

	setupPaymentPrepareExpects(dbMock)
	setupRollbackExpects(dbMock)

	registerPrepareRepoMocks(repo, ctx)
	repo.On("GetOutgoingByIdempotence", ctx, int64(111), mock.Anything).Return(nil, nil)
	repo.On("CreateMessage", ctx, mock.Anything).Return(nil)
	repo.On("UpdateMessage", ctx, mock.Anything).Return(nil)
	repo.On("ListReservationsByTx", ctx, int64(0)).Return([]domain.InterbankReservation{testSenderReservation()}, nil)
	client.On("SendMessage", ctx, mock.Anything).Return(200, []byte(`{"vote":"NO"}`), nil)

	_, err := coord.InitiateInterbankPayment(ctx, standardPaymentInput())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "odbila")
	repo.AssertExpectations(t)
	client.AssertExpectations(t)
}

func TestInitiateInterbankPayment_CommitError(t *testing.T) {
	repo, client, coord, dbMock := newCoordSutWithDB(t)
	ctx := context.Background()

	setupPaymentPrepareExpects(dbMock)

	registerPrepareRepoMocks(repo, ctx)
	repo.On("GetOutgoingByIdempotence", ctx, int64(111), mock.Anything).Return(nil, nil)
	repo.On("CreateMessage", ctx, mock.Anything).Return(nil)
	repo.On("UpdateMessage", ctx, mock.Anything).Return(nil)
	repo.On("ListReservationsByTx", ctx, int64(0)).Return(nil, errors.New("db commit fail"))
	client.On("SendMessage", ctx, mock.Anything).Return(200, []byte(`{"vote":"YES"}`), nil)

	_, err := coord.InitiateInterbankPayment(ctx, standardPaymentInput())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lokalni commit")
	repo.AssertExpectations(t)
	client.AssertExpectations(t)
}

func TestInitiateInterbankPayment_FullSuccess(t *testing.T) {
	repo, client, coord, dbMock := newCoordSutWithDB(t)
	ctx := context.Background()

	setupPaymentPrepareExpects(dbMock)
	dbMock.ExpectBegin()
	dbMock.ExpectExec("rezervisana_sredstva = rezervisana_sredstva -").
		WillReturnResult(sqlmock.NewResult(0, 1))
	dbMock.ExpectQuery("SELECT id FROM core_banking.racun WHERE").
		WillReturnRows(dbMock.NewRows([]string{"id"}).AddRow(int64(99)))
	dbMock.ExpectExec("INSERT INTO core_banking.transakcija").
		WillReturnResult(sqlmock.NewResult(1, 1))
	dbMock.ExpectCommit()

	registerPrepareRepoMocks(repo, ctx)
	repo.On("GetOutgoingByIdempotence", ctx, int64(111), mock.Anything).Return(nil, nil)
	repo.On("CreateMessage", ctx, mock.Anything).Return(nil)
	repo.On("UpdateMessage", ctx, mock.Anything).Return(nil)
	repo.On("ListReservationsByTx", ctx, int64(0)).Return([]domain.InterbankReservation{testSenderReservation()}, nil)
	client.On("SendMessage", ctx, mock.Anything).Return(200, []byte(`{"vote":"YES"}`), nil)

	ibTx, err := coord.InitiateInterbankPayment(ctx, standardPaymentInput())
	require.NoError(t, err)
	require.NotNil(t, ibTx)
	repo.AssertExpectations(t)
	client.AssertExpectations(t)
}

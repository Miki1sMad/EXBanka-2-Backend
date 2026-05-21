package service

// White-box tests for unexported helpers in interbank_message_service.go.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"banka-backend/services/bank-service/internal/domain"
)

func newMsgSvc(repo *mockInterbankRepoForExecutor, client *mockIBClientForCoord) *InterbankMessageService {
	return &InterbankMessageService{
		repo:            repo,
		client:          client,
		ourRoutingNum:   111,
		maxRetry:        3,
		backoffDuration: 100 * time.Millisecond,
	}
}

// ─── minInt ───────────────────────────────────────────────────────────────────

func TestMinInt_FirstSmaller(t *testing.T) {
	assert.Equal(t, 2, minInt(2, 5))
}

func TestMinInt_SecondSmaller(t *testing.T) {
	assert.Equal(t, 3, minInt(7, 3))
}

func TestMinInt_Equal(t *testing.T) {
	assert.Equal(t, 4, minInt(4, 4))
}

// ─── processRetryBatch ────────────────────────────────────────────────────────

func TestProcessRetryBatch_ListError(t *testing.T) {
	repo := &mockInterbankRepoForExecutor{}
	svc := newMsgSvc(repo, &mockIBClientForCoord{})
	ctx := context.Background()

	repo.On("ListPendingOutgoing", ctx, 50).Return(nil, errors.New("db"))
	// Should not panic; just logs and returns
	svc.processRetryBatch(ctx)
	repo.AssertExpectations(t)
}

func TestProcessRetryBatch_Empty(t *testing.T) {
	repo := &mockInterbankRepoForExecutor{}
	svc := newMsgSvc(repo, &mockIBClientForCoord{})
	ctx := context.Background()

	repo.On("ListPendingOutgoing", ctx, 50).Return([]domain.InterbankMessageLog{}, nil)
	svc.processRetryBatch(ctx)
	repo.AssertExpectations(t)
}

func TestProcessRetryBatch_SkipsExhausted(t *testing.T) {
	repo := &mockInterbankRepoForExecutor{}
	svc := newMsgSvc(repo, &mockIBClientForCoord{})
	ctx := context.Background()

	// retryCount >= maxRetry AND status == Failed → skip
	exhausted := domain.InterbankMessageLog{
		ID:         1,
		RetryCount: 3,
		Status:     domain.MsgStatusFailed,
	}
	repo.On("ListPendingOutgoing", ctx, 50).Return([]domain.InterbankMessageLog{exhausted}, nil)
	// No SendMessage call expected
	svc.processRetryBatch(ctx)
	repo.AssertExpectations(t)
}

func TestProcessRetryBatch_SendsMessage(t *testing.T) {
	repo := &mockInterbankRepoForExecutor{}
	client := &mockIBClientForCoord{}
	svc := newMsgSvc(repo, client)
	ctx := context.Background()

	msg := domain.InterbankMessageLog{
		ID:          2,
		RetryCount:  0,
		Status:      domain.MsgStatusPending,
		MessageType: domain.MessageNewTx,
		Payload:     `{}`,
	}
	repo.On("ListPendingOutgoing", ctx, 50).Return([]domain.InterbankMessageLog{msg}, nil)
	// SendMessage calls client.SendMessage then UpdateMessage
	client.On("SendMessage", ctx, mock.Anything).Return(200, []byte(`{}`), nil)
	repo.On("UpdateMessage", ctx, mock.Anything).Return(nil)

	svc.processRetryBatch(ctx)
	repo.AssertExpectations(t)
	client.AssertExpectations(t)
}

func TestProcessRetryBatch_SendError_Continues(t *testing.T) {
	repo := &mockInterbankRepoForExecutor{}
	client := &mockIBClientForCoord{}
	svc := newMsgSvc(repo, client)
	ctx := context.Background()

	msg := domain.InterbankMessageLog{
		ID:          3,
		RetryCount:  1,
		Status:      domain.MsgStatusPending,
		MessageType: domain.MessageNewTx,
		Payload:     `{}`,
	}
	repo.On("ListPendingOutgoing", ctx, 50).Return([]domain.InterbankMessageLog{msg}, nil)
	// Network error
	client.On("SendMessage", ctx, mock.Anything).Return(0, nil, errors.New("timeout"))
	repo.On("UpdateMessage", ctx, mock.Anything).Return(nil)

	svc.processRetryBatch(ctx) // should not panic
	repo.AssertExpectations(t)
}

// ─── SendNow ──────────────────────────────────────────────────────────────────

func TestSendNow_ReturnsError(t *testing.T) {
	svc := newMsgSvc(&mockInterbankRepoForExecutor{}, &mockIBClientForCoord{})
	_, err := svc.SendNow(context.Background(), 42)
	assert.Error(t, err)
}

// ─── RunRetryLoop ─────────────────────────────────────────────────────────────

func TestRunRetryLoop_StopsOnContextCancel(t *testing.T) {
	svc := newMsgSvc(&mockInterbankRepoForExecutor{}, &mockIBClientForCoord{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		svc.RunRetryLoop(ctx, time.Second)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunRetryLoop did not stop after context cancellation")
	}
}

func TestRunRetryLoop_ZeroInterval_StopsOnContextCancel(t *testing.T) {
	svc := newMsgSvc(&mockInterbankRepoForExecutor{}, &mockIBClientForCoord{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		svc.RunRetryLoop(ctx, 0)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunRetryLoop with zero interval did not stop")
	}
}

// ─── NewInterbankMessageService ───────────────────────────────────────────────

func TestNewInterbankMessageService_Defaults(t *testing.T) {
	svc := NewInterbankMessageService(&mockInterbankRepoForExecutor{}, &mockIBClientForCoord{}, 111, 0, 0)
	assert.Equal(t, 10, svc.maxRetry)
	assert.Equal(t, 30*time.Second, svc.backoffDuration)
}

func TestNewInterbankMessageService_CustomValues(t *testing.T) {
	svc := NewInterbankMessageService(&mockInterbankRepoForExecutor{}, &mockIBClientForCoord{}, 222, 5, 60)
	assert.Equal(t, 5, svc.maxRetry)
	assert.Equal(t, 60*time.Second, svc.backoffDuration)
	assert.Equal(t, int64(222), svc.ourRoutingNum)
}

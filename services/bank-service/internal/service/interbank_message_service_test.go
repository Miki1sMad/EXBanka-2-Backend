package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"banka-backend/services/bank-service/internal/domain"
	"banka-backend/services/bank-service/internal/service"
)

// ─── Mock InterbankClient ─────────────────────────────────────────────────────

type mockInterbankClient struct{ mock.Mock }

func (m *mockInterbankClient) SendMessage(ctx context.Context, msg domain.InterbankMessage) (int, []byte, error) {
	args := m.Called(ctx, msg)
	return args.Int(0), args.Get(1).([]byte), args.Error(2)
}
func (m *mockInterbankClient) GetPublicStock(ctx context.Context) ([]domain.PublicStock, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.PublicStock), args.Error(1)
}
func (m *mockInterbankClient) CreateNegotiation(ctx context.Context, offer domain.OtcOffer) (*domain.ForeignBankId, error) {
	args := m.Called(ctx, offer)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.ForeignBankId), args.Error(1)
}
func (m *mockInterbankClient) CounterNegotiation(ctx context.Context, id domain.ForeignBankId, offer domain.OtcOffer) error {
	return m.Called(ctx, id, offer).Error(0)
}
func (m *mockInterbankClient) GetNegotiation(ctx context.Context, id domain.ForeignBankId) (*domain.OtcNegotiation, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.OtcNegotiation), args.Error(1)
}
func (m *mockInterbankClient) CancelNegotiation(ctx context.Context, id domain.ForeignBankId) error {
	return m.Called(ctx, id).Error(0)
}
func (m *mockInterbankClient) AcceptNegotiation(ctx context.Context, id domain.ForeignBankId) error {
	return m.Called(ctx, id).Error(0)
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func newMsgService(repo domain.InterbankRepository, client domain.InterbankClient) *service.InterbankMessageService {
	return service.NewInterbankMessageService(repo, client, ourRouting, 3, 1)
}

func makeEnvelope(msgType domain.MessageType) domain.InterbankMessage {
	raw, _ := json.Marshal(map[string]string{"key": "value"})
	return domain.InterbankMessage{
		IdempotenceKey: domain.IdempotenceKey{RoutingNumber: 222, LocallyGeneratedKey: "key123"},
		MessageType:    msgType,
		Message:        json.RawMessage(raw),
	}
}

// ─── EnqueueOutgoing ──────────────────────────────────────────────────────────

func TestEnqueueOutgoing_NewMessage(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newMsgService(repo, &mockInterbankClient{})

	key := domain.IdempotenceKey{RoutingNumber: ourRouting, LocallyGeneratedKey: "abc"}
	repo.On("GetOutgoingByIdempotence", ctx, ourRouting, "abc").Return(nil, nil)
	repo.On("CreateMessage", ctx, mock.AnythingOfType("*domain.InterbankMessageLog")).Return(nil)

	target := int64(222)
	msg, err := svc.EnqueueOutgoing(ctx, domain.MessageNewTx, target, key, map[string]string{"x": "y"})
	require.NoError(t, err)
	assert.Equal(t, domain.DirectionOutgoing, msg.Direction)
	assert.Equal(t, domain.MsgStatusPending, msg.Status)
}

func TestEnqueueOutgoing_Idempotent(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newMsgService(repo, &mockInterbankClient{})

	key := domain.IdempotenceKey{RoutingNumber: ourRouting, LocallyGeneratedKey: "abc"}
	existing := &domain.InterbankMessageLog{ID: 99, Status: domain.MsgStatusSent}
	repo.On("GetOutgoingByIdempotence", ctx, ourRouting, "abc").Return(existing, nil)

	msg, err := svc.EnqueueOutgoing(ctx, domain.MessageNewTx, int64(222), key, "body")
	require.NoError(t, err)
	assert.Equal(t, int64(99), msg.ID)
}

func TestEnqueueOutgoing_RepoError(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newMsgService(repo, &mockInterbankClient{})

	key := domain.IdempotenceKey{RoutingNumber: ourRouting, LocallyGeneratedKey: "abc"}
	repo.On("GetOutgoingByIdempotence", ctx, ourRouting, "abc").Return(nil, errors.New("db"))

	_, err := svc.EnqueueOutgoing(ctx, domain.MessageNewTx, int64(222), key, "body")
	require.Error(t, err)
}

func TestEnqueueOutgoing_CreateError(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newMsgService(repo, &mockInterbankClient{})

	key := domain.IdempotenceKey{RoutingNumber: ourRouting, LocallyGeneratedKey: "abc"}
	repo.On("GetOutgoingByIdempotence", ctx, ourRouting, "abc").Return(nil, nil)
	repo.On("CreateMessage", ctx, mock.Anything).Return(errors.New("db"))

	_, err := svc.EnqueueOutgoing(ctx, domain.MessageNewTx, int64(222), key, "body")
	require.Error(t, err)
}

// ─── SendMessage ──────────────────────────────────────────────────────────────

func TestSendMessage_AlreadySent_Noop(t *testing.T) {
	repo := &mockInterbankRepo{}
	client := &mockInterbankClient{}
	ctx := context.Background()
	svc := newMsgService(repo, client)

	m := &domain.InterbankMessageLog{Status: domain.MsgStatusSent, Payload: `{}`}
	err := svc.SendMessage(ctx, m)
	require.NoError(t, err)
	client.AssertNotCalled(t, "SendMessage")
}

func TestSendMessage_AlreadyProcessed_Noop(t *testing.T) {
	repo := &mockInterbankRepo{}
	client := &mockInterbankClient{}
	ctx := context.Background()
	svc := newMsgService(repo, client)

	m := &domain.InterbankMessageLog{Status: domain.MsgStatusProcessed, Payload: `{}`}
	err := svc.SendMessage(ctx, m)
	require.NoError(t, err)
	client.AssertNotCalled(t, "SendMessage")
}

func TestSendMessage_Success200(t *testing.T) {
	repo := &mockInterbankRepo{}
	client := &mockInterbankClient{}
	ctx := context.Background()
	svc := newMsgService(repo, client)

	payload, _ := json.Marshal(map[string]string{})
	m := &domain.InterbankMessageLog{
		Status:                   domain.MsgStatusPending,
		Payload:                  string(payload),
		MessageType:              domain.MessageNewTx,
		IdempotenceRoutingNumber: 222,
		IdempotenceLocalKey:      "k1",
	}

	client.On("SendMessage", ctx, mock.Anything).Return(http.StatusOK, []byte(`ok`), nil)
	repo.On("UpdateMessage", ctx, m).Return(nil)

	err := svc.SendMessage(ctx, m)
	require.NoError(t, err)
	assert.Equal(t, domain.MsgStatusSent, m.Status)
}

func TestSendMessage_Success204(t *testing.T) {
	repo := &mockInterbankRepo{}
	client := &mockInterbankClient{}
	ctx := context.Background()
	svc := newMsgService(repo, client)

	payload, _ := json.Marshal(map[string]string{})
	m := &domain.InterbankMessageLog{
		Status:      domain.MsgStatusPending,
		Payload:     string(payload),
		MessageType: domain.MessageCommitTx,
	}

	client.On("SendMessage", ctx, mock.Anything).Return(http.StatusNoContent, []byte{}, nil)
	repo.On("UpdateMessage", ctx, m).Return(nil)

	err := svc.SendMessage(ctx, m)
	require.NoError(t, err)
	assert.Equal(t, domain.MsgStatusSent, m.Status)
}

func TestSendMessage_Accepted202(t *testing.T) {
	repo := &mockInterbankRepo{}
	client := &mockInterbankClient{}
	ctx := context.Background()
	svc := newMsgService(repo, client)

	payload, _ := json.Marshal(map[string]string{})
	m := &domain.InterbankMessageLog{
		Status:      domain.MsgStatusPending,
		Payload:     string(payload),
		MessageType: domain.MessageNewTx,
	}

	client.On("SendMessage", ctx, mock.Anything).Return(http.StatusAccepted, []byte{}, nil)
	repo.On("UpdateMessage", ctx, m).Return(nil)

	err := svc.SendMessage(ctx, m)
	require.NoError(t, err)
	assert.Equal(t, domain.MsgStatusAccepted, m.Status)
	assert.Equal(t, 1, m.RetryCount)
	assert.NotNil(t, m.NextRetryAt)
}

func TestSendMessage_Error5xx(t *testing.T) {
	repo := &mockInterbankRepo{}
	client := &mockInterbankClient{}
	ctx := context.Background()
	svc := newMsgService(repo, client)

	payload, _ := json.Marshal(map[string]string{})
	m := &domain.InterbankMessageLog{
		Status:      domain.MsgStatusPending,
		Payload:     string(payload),
		MessageType: domain.MessageNewTx,
	}

	client.On("SendMessage", ctx, mock.Anything).Return(http.StatusInternalServerError, []byte(`err`), nil)
	repo.On("UpdateMessage", ctx, m).Return(nil)

	err := svc.SendMessage(ctx, m)
	require.NoError(t, err)
	assert.Equal(t, domain.MsgStatusFailed, m.Status)
}

func TestSendMessage_NetworkError(t *testing.T) {
	repo := &mockInterbankRepo{}
	client := &mockInterbankClient{}
	ctx := context.Background()
	svc := newMsgService(repo, client)

	payload, _ := json.Marshal(map[string]string{})
	m := &domain.InterbankMessageLog{
		Status:      domain.MsgStatusPending,
		Payload:     string(payload),
		MessageType: domain.MessageNewTx,
	}

	netErr := errors.New("connection refused")
	client.On("SendMessage", ctx, mock.Anything).Return(0, []byte{}, netErr)
	repo.On("UpdateMessage", ctx, m).Return(nil)

	err := svc.SendMessage(ctx, m)
	require.Error(t, err)
	assert.Equal(t, domain.MsgStatusFailed, m.Status)
}

// ─── LookupIncomingResponse ───────────────────────────────────────────────────

func TestLookupIncomingResponse_Found(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newMsgService(repo, &mockInterbankClient{})

	key := domain.IdempotenceKey{RoutingNumber: 222, LocallyGeneratedKey: "k1"}
	existing := &domain.InterbankMessageLog{ID: 7}
	repo.On("GetIncomingByIdempotence", ctx, int64(222), "k1").Return(existing, nil)

	got, err := svc.LookupIncomingResponse(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, int64(7), got.ID)
}

func TestLookupIncomingResponse_NotFound(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newMsgService(repo, &mockInterbankClient{})

	key := domain.IdempotenceKey{RoutingNumber: 222, LocallyGeneratedKey: "k1"}
	repo.On("GetIncomingByIdempotence", ctx, int64(222), "k1").Return(nil, nil)

	got, err := svc.LookupIncomingResponse(ctx, key)
	require.NoError(t, err)
	assert.Nil(t, got)
}

// ─── RecordIncoming ───────────────────────────────────────────────────────────

func TestRecordIncoming_OK(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newMsgService(repo, &mockInterbankClient{})

	envelope := makeEnvelope(domain.MessageNewTx)
	repo.On("CreateMessage", ctx, mock.AnythingOfType("*domain.InterbankMessageLog")).Return(nil)

	row, err := svc.RecordIncoming(ctx, envelope)
	require.NoError(t, err)
	assert.Equal(t, domain.DirectionIncoming, row.Direction)
	assert.Equal(t, domain.MsgStatusPending, row.Status)
}

func TestRecordIncoming_RepoError(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newMsgService(repo, &mockInterbankClient{})

	envelope := makeEnvelope(domain.MessageNewTx)
	repo.On("CreateMessage", ctx, mock.Anything).Return(errors.New("db"))

	_, err := svc.RecordIncoming(ctx, envelope)
	require.Error(t, err)
}

// ─── FinishIncoming ───────────────────────────────────────────────────────────

func TestFinishIncoming_OK(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newMsgService(repo, &mockInterbankClient{})

	row := &domain.InterbankMessageLog{ID: 3}
	repo.On("UpdateMessage", ctx, row).Return(nil)

	err := svc.FinishIncoming(ctx, row, http.StatusOK, map[string]string{"vote": "YES"})
	require.NoError(t, err)
	assert.Equal(t, domain.MsgStatusProcessed, row.Status)
	assert.NotNil(t, row.ResponsePayload)
	assert.Equal(t, http.StatusOK, *row.ResponseStatusCode)
}

func TestFinishIncoming_NilResponse(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newMsgService(repo, &mockInterbankClient{})

	row := &domain.InterbankMessageLog{ID: 4}
	repo.On("UpdateMessage", ctx, row).Return(nil)

	err := svc.FinishIncoming(ctx, row, http.StatusNoContent, nil)
	require.NoError(t, err)
	assert.Nil(t, row.ResponsePayload)
}

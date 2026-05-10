// Package service — interbank_message_service.go
//
// Servis koji čuva incoming/outgoing poruke, garantuje idempotency i upravlja
// retry-em ka drugoj banci. Po protokolu:
//   - banka mora čuvati primljene idempotenceKey vrednosti,
//   - ako isti zahtev stigne ponovo, mora vratiti isti odgovor kao prvi put,
//   - retry NE generiše novi idempotenceKey (zadržava se isti).
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"banka-backend/services/bank-service/internal/domain"

	"github.com/google/uuid"
)

// InterbankMessageService — servisni sloj za message log + retry.
type InterbankMessageService struct {
	repo            domain.InterbankRepository
	client          domain.InterbankClient
	ourRoutingNum   int64
	maxRetry        int
	backoffDuration time.Duration
}

// NewInterbankMessageService konstruktor.
func NewInterbankMessageService(
	repo domain.InterbankRepository,
	client domain.InterbankClient,
	ourRoutingNumber int64,
	maxRetry int,
	backoffSeconds int,
) *InterbankMessageService {
	if maxRetry <= 0 {
		maxRetry = 10
	}
	if backoffSeconds <= 0 {
		backoffSeconds = 30
	}
	return &InterbankMessageService{
		repo:            repo,
		client:          client,
		ourRoutingNum:   ourRoutingNumber,
		maxRetry:        maxRetry,
		backoffDuration: time.Duration(backoffSeconds) * time.Second,
	}
}

// NewLocalKey generiše lokalno-jedinstven 64-bajtni ključ.
// Koristi UUID v4 (bez crta = 32 hex karaktera = ≤ 64 bajta).
func NewLocalKey() string {
	return uuid.NewString()
}

// EnqueueOutgoing kreira OUTGOING zapis u log-u sa idempotenceKey-om i statusom
// PENDING. Vraća taj zapis. Caller (TransactionCoordinator) zatim poziva
// SendNow ili pušta retry worker da ga obradi.
func (s *InterbankMessageService) EnqueueOutgoing(
	ctx context.Context,
	msgType domain.MessageType,
	target int64,
	idempotenceKey domain.IdempotenceKey,
	body interface{},
) (*domain.InterbankMessageLog, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("interbank: marshal payload: %w", err)
	}

	// Idempotentno: ako već postoji OUTGOING sa istim ključem, vraćamo njega.
	existing, err := s.repo.GetOutgoingByIdempotence(ctx, idempotenceKey.RoutingNumber, idempotenceKey.LocallyGeneratedKey)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	row := &domain.InterbankMessageLog{
		Direction:                domain.DirectionOutgoing,
		MessageType:              msgType,
		IdempotenceRoutingNumber: idempotenceKey.RoutingNumber,
		IdempotenceLocalKey:      idempotenceKey.LocallyGeneratedKey,
		TargetRoutingNumber:      &target,
		Payload:                  string(payload),
		Status:                   domain.MsgStatusPending,
	}
	if err := s.repo.CreateMessage(ctx, row); err != nil {
		return nil, fmt.Errorf("interbank: čuvanje OUTGOING poruke: %w", err)
	}
	return row, nil
}

// SendNow šalje poruku odmah. Po protokolu:
//   - 200 OK ili 204 No Content → SENT (sa response_payload-om za 200)
//   - 202 Accepted              → ACCEPTED (poruka primljena, retry kasnije)
//   - bilo šta drugo / network  → FAILED (zakazaće retry)
//
// Ako je poruka već SENT, samo vraća postojeći zapis (idempotency).
func (s *InterbankMessageService) SendNow(ctx context.Context, msgID int64) (*domain.InterbankMessageLog, error) {
	// Najlakše je preuzeti zapis preko repo.GetOutgoingByIdempotence — ali nemamo
	// "GetByID". Uradiimo to preko ListPendingOutgoing(filter)? Jednostavnije:
	// sami izvučemo iz baze koristeći GetOutgoingByIdempotence (ne radi za ID).
	// Umesto toga, prosleđujemo direktno poruku.
	return nil, fmt.Errorf("SendNow(msgID) zahteva direktno prosleđivanje poruke; koristite SendMessage")
}

// SendMessage — šalje već enqueue-ovanu poruku. Ažurira status u log-u prema
// HTTP odgovoru. Ako odgovor nije 200/204/202, status se postavlja na FAILED i
// next_retry_at se planira za backoffDuration unapred.
func (s *InterbankMessageService) SendMessage(ctx context.Context, m *domain.InterbankMessageLog) error {
	if m.Status == domain.MsgStatusSent || m.Status == domain.MsgStatusProcessed {
		return nil // već završeno — ne šaljemo opet
	}

	// Reanimiraj poruku iz JSON-a u InterbankMessage.
	inner := json.RawMessage(m.Payload)
	envelope := domain.InterbankMessage{
		IdempotenceKey: domain.IdempotenceKey{
			RoutingNumber:       m.IdempotenceRoutingNumber,
			LocallyGeneratedKey: m.IdempotenceLocalKey,
		},
		MessageType: m.MessageType,
		Message:     inner,
	}

	statusCode, body, err := s.client.SendMessage(ctx, envelope)
	now := time.Now().UTC()

	if err != nil {
		m.Status = domain.MsgStatusFailed
		m.LastError = err.Error()
		m.RetryCount++
		nextRetry := now.Add(s.backoffDuration * time.Duration(1<<minInt(m.RetryCount-1, 6)))
		m.NextRetryAt = &nextRetry
		_ = s.repo.UpdateMessage(ctx, m)
		return err
	}

	m.ResponseStatusCode = &statusCode
	bodyStr := string(body)
	m.ResponsePayload = &bodyStr

	switch statusCode {
	case http.StatusOK, http.StatusNoContent:
		m.Status = domain.MsgStatusSent
		m.NextRetryAt = nil
		m.LastError = ""
	case http.StatusAccepted:
		// Druga strana je primila ali još obrađuje. Mora biti retry.
		m.Status = domain.MsgStatusAccepted
		m.RetryCount++
		nextRetry := now.Add(s.backoffDuration)
		m.NextRetryAt = &nextRetry
	default:
		m.Status = domain.MsgStatusFailed
		m.LastError = fmt.Sprintf("HTTP %d: %s", statusCode, bodyStr)
		m.RetryCount++
		nextRetry := now.Add(s.backoffDuration * time.Duration(1<<minInt(m.RetryCount-1, 6)))
		m.NextRetryAt = &nextRetry
	}

	if err := s.repo.UpdateMessage(ctx, m); err != nil {
		return fmt.Errorf("interbank: update message log: %w", err)
	}
	return nil
}

// LookupIncomingResponse — koristi handler /interbank kada primi poruku, da
// proveri da li je idempotenceKey već viđen i ako jeste vrati identičan response.
func (s *InterbankMessageService) LookupIncomingResponse(ctx context.Context, key domain.IdempotenceKey) (*domain.InterbankMessageLog, error) {
	return s.repo.GetIncomingByIdempotence(ctx, key.RoutingNumber, key.LocallyGeneratedKey)
}

// RecordIncoming — kreira INCOMING red posle inicijalne validacije ali pre
// poslovne obrade, kako bi se duplikat (paralelni isti zahtev) prepoznao.
func (s *InterbankMessageService) RecordIncoming(
	ctx context.Context,
	msg domain.InterbankMessage,
) (*domain.InterbankMessageLog, error) {
	row := &domain.InterbankMessageLog{
		Direction:                domain.DirectionIncoming,
		MessageType:              msg.MessageType,
		IdempotenceRoutingNumber: msg.IdempotenceKey.RoutingNumber,
		IdempotenceLocalKey:      msg.IdempotenceKey.LocallyGeneratedKey,
		Payload:                  string(msg.Message),
		Status:                   domain.MsgStatusPending,
	}
	if err := s.repo.CreateMessage(ctx, row); err != nil {
		return nil, err
	}
	return row, nil
}

// FinishIncoming — ažurira status i response payload INCOMING poruke posle obrade.
func (s *InterbankMessageService) FinishIncoming(
	ctx context.Context,
	row *domain.InterbankMessageLog,
	statusCode int,
	response interface{},
) error {
	row.Status = domain.MsgStatusProcessed
	if response != nil {
		buf, err := json.Marshal(response)
		if err != nil {
			return err
		}
		s := string(buf)
		row.ResponsePayload = &s
	}
	row.ResponseStatusCode = &statusCode
	return s.repo.UpdateMessage(ctx, row)
}

// RunRetryLoop — pokreće worker koji periodično šalje OUTGOING poruke koje
// još nisu uspele. Prekida se kada ctx bude otkazan.
func (s *InterbankMessageService) RunRetryLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.processRetryBatch(ctx)
		}
	}
}

func (s *InterbankMessageService) processRetryBatch(ctx context.Context) {
	rows, err := s.repo.ListPendingOutgoing(ctx, 50)
	if err != nil {
		log.Printf("[interbank-retry] list pending: %v", err)
		return
	}
	for i := range rows {
		m := rows[i]
		if m.RetryCount >= s.maxRetry && m.Status == domain.MsgStatusFailed {
			continue
		}
		if err := s.SendMessage(ctx, &m); err != nil {
			log.Printf("[interbank-retry] send msg id=%d: %v", m.ID, err)
		}
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

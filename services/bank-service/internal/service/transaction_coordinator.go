// Package service — transaction_coordinator.go
//
// TransactionCoordinator orkestrira protokol između banaka:
//
//	InitiateInterbankPayment:
//	  1. Kreira Transaction objekat sa postings (sender − , receiver +).
//	  2. Lokalno priprema (LocalTransactionExecutor.Prepare).
//	  3. Šalje NEW_TX drugoj banci preko InterbankMessageService.
//	  4. Ako druga banka vrati YES → šalje COMMIT_TX i lokalno commit-uje.
//	  5. Ako vrati NO ili ne odgovori → šalje ROLLBACK_TX i lokalno rollback-uje.
//
//	HandleIncomingMessage:
//	  - NEW_TX     → Prepare i vraća TransactionVote.
//	  - COMMIT_TX  → Commit i vraća 204.
//	  - ROLLBACK_TX→ Rollback i vraća 204.
//
//	Idempotency: handler proverava InterbankMessageService.LookupIncomingResponse
//	pre obrade. Ako poruka već postoji, vraća se identičan odgovor.
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"banka-backend/services/bank-service/internal/domain"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type TransactionCoordinator struct {
	db                *gorm.DB
	repo              domain.InterbankRepository
	executor          *LocalTransactionExecutor
	msgSvc            *InterbankMessageService
	client            domain.InterbankClient
	ourRoutingNumber  int64
	peerRoutingNumber int64
	accountPrefix     string
}

// NewTransactionCoordinator konstruktor.
func NewTransactionCoordinator(
	db *gorm.DB,
	repo domain.InterbankRepository,
	executor *LocalTransactionExecutor,
	msgSvc *InterbankMessageService,
	client domain.InterbankClient,
	ourRoutingNumber, peerRoutingNumber int64,
	accountPrefix string,
) *TransactionCoordinator {
	if accountPrefix == "" {
		accountPrefix = strconv.FormatInt(ourRoutingNumber, 10)
	}
	return &TransactionCoordinator{
		db:                db,
		repo:              repo,
		executor:          executor,
		msgSvc:            msgSvc,
		client:            client,
		ourRoutingNumber:  ourRoutingNumber,
		peerRoutingNumber: peerRoutingNumber,
		accountPrefix:     accountPrefix,
	}
}

// IsLocalAccount — public helper za PaymentService da odluči da li je
// dati broj računa u našoj banci ili nije.
func (c *TransactionCoordinator) IsLocalAccount(brojRacuna string) bool {
	if brojRacuna == "" {
		return false
	}
	return strings.HasPrefix(brojRacuna, c.accountPrefix)
}

// PeerRoutingNumber — javni getter za druge servise.
func (c *TransactionCoordinator) PeerRoutingNumber() int64 {
	return c.peerRoutingNumber
}

// ─── InitiateInterbankPayment ────────────────────────────────────────────────

// InitiateInterbankPayment formira Transaction i izvršava 2-phase commit.
// Vraća zapis interbank_transaction sa krajnjim statusom (COMMITTED ili FAILED).
func (c *TransactionCoordinator) InitiateInterbankPayment(ctx context.Context, in domain.InterbankPaymentInput) (*domain.InterbankTransaction, error) {
	// 0) Validacija ulaza
	if in.SenderAccountID <= 0 {
		return nil, fmt.Errorf("interbank payment: SenderAccountID je obavezan")
	}
	if in.RecipientAccountNo == "" {
		return nil, fmt.Errorf("interbank payment: broj računa primaoca je obavezan")
	}
	if in.Amount.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf("interbank payment: iznos mora biti veći od 0")
	}
	if in.Currency == "" {
		return nil, fmt.Errorf("interbank payment: valuta je obavezna")
	}

	// Resolve broj računa pošiljaoca
	var senderAccount struct {
		BrojRacuna string `gorm:"column:broj_racuna"`
		Valuta     string `gorm:"column:oznaka"`
	}
	err := c.db.WithContext(ctx).Raw(`
		SELECT r.broj_racuna, v.oznaka
		FROM core_banking.racun r
		JOIN core_banking.valuta v ON v.id = r.id_valute
		WHERE r.id = ? AND r.id_vlasnika = ?
	`, in.SenderAccountID, in.SenderUserID).Scan(&senderAccount).Error
	if err != nil || senderAccount.BrojRacuna == "" {
		return nil, fmt.Errorf("interbank payment: pošiljaočev račun nije pronađen ili ne pripada korisniku")
	}
	if senderAccount.Valuta != in.Currency {
		return nil, fmt.Errorf("interbank payment: valuta računa pošiljaoca ne odgovara")
	}

	txID := domain.ForeignBankId{
		RoutingNumber: c.ourRoutingNumber,
		ID:            NewLocalKey(),
	}

	// Postings: pošiljalac negativan, primalac pozitivan; balance = 0.
	senderNum := senderAccount.BrojRacuna
	receiverNum := in.RecipientAccountNo

	negAmt := in.Amount.Neg()
	posAmt := in.Amount

	postings := []domain.Posting{
		{
			Account: domain.TxAccount{Type: domain.AccountKindAccount, Num: &senderNum},
			Amount:  negAmt,
			Asset:   domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: in.Currency}},
		},
		{
			Account: domain.TxAccount{Type: domain.AccountKindAccount, Num: &receiverNum},
			Amount:  posAmt,
			Asset:   domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: in.Currency}},
		},
	}

	callNumberPtr := &in.CallNumber
	if in.CallNumber == "" {
		callNumberPtr = nil
	}
	transaction := domain.Transaction{
		Postings:       postings,
		TransactionID:  txID,
		Message:        in.Message,
		CallNumber:     callNumberPtr,
		PaymentCode:    in.PaymentCode,
		PaymentPurpose: in.PaymentPurpose,
	}

	// 1) Kreiraj interbank_transaction zapis
	payloadStr, err := MarshalTransaction(transaction)
	if err != nil {
		return nil, err
	}
	ibTx := &domain.InterbankTransaction{
		TransactionRoutingNumber: txID.RoutingNumber,
		TransactionForeignID:     txID.ID,
		Role:                     domain.TxRoleCoordinator,
		Status:                   domain.TxStatusNew,
		CurrentStep:              "PREPARING_LOCAL",
		Payload:                  payloadStr,
		InitiatorUserID:          ptrInt64(in.SenderUserID),
		InitiatorAccountID:       ptrInt64(in.SenderAccountID),
	}
	if err := c.repo.CreateTransaction(ctx, ibTx); err != nil {
		return nil, fmt.Errorf("interbank payment: kreiranje saga zapisa: %w", err)
	}

	// 2) Lokalna priprema (rezervacija sredstava pošiljaoca)
	vote, err := c.executor.Prepare(ctx, ibTx.ID, transaction)
	if err != nil {
		_ = c.repo.UpdateTransactionStatus(ctx, ibTx.ID, domain.TxStatusFailed, "LOCAL_PREPARE_ERROR", err.Error())
		return ibTx, err
	}
	if vote.Vote == domain.VoteNo {
		_ = c.repo.UpdateTransactionStatus(ctx, ibTx.ID, domain.TxStatusFailed, "LOCAL_PREPARE_NO", joinReasons(vote.Reasons))
		return ibTx, fmt.Errorf("interbank payment: lokalna priprema vraća NO: %s", joinReasons(vote.Reasons))
	}
	_ = c.repo.UpdateTransactionStatus(ctx, ibTx.ID, domain.TxStatusPrepared, "LOCAL_PREPARED", "")

	// 3) Pošalji NEW_TX drugoj banci
	idemKey := domain.IdempotenceKey{
		RoutingNumber:       c.ourRoutingNumber,
		LocallyGeneratedKey: NewLocalKey(),
	}
	logRow, err := c.msgSvc.EnqueueOutgoing(ctx, domain.MessageNewTx, c.peerRoutingNumber, idemKey, transaction)
	if err != nil {
		_ = c.executor.Rollback(ctx, ibTx.ID)
		_ = c.repo.UpdateTransactionStatus(ctx, ibTx.ID, domain.TxStatusFailed, "ENQUEUE_NEW_TX", err.Error())
		return ibTx, err
	}
	if err := c.msgSvc.SendMessage(ctx, logRow); err != nil {
		// Network/misc error → ROLLBACK lokalno (ne šaljemo COMMIT bez glasa).
		_ = c.executor.Rollback(ctx, ibTx.ID)
		_ = c.repo.UpdateTransactionStatus(ctx, ibTx.ID, domain.TxStatusFailed, "SEND_NEW_TX", err.Error())
		return ibTx, err
	}

	// 4) Parse vote
	if logRow.Status != domain.MsgStatusSent || logRow.ResponsePayload == nil {
		// 202 Accepted ili neuspeh — ne komitujemo dok ne dobijemo YES/NO.
		_ = c.repo.UpdateTransactionStatus(ctx, ibTx.ID, domain.TxStatusFailed, "AWAITING_REMOTE_VOTE", "remote bank not finished")
		return ibTx, fmt.Errorf("interbank payment: druga banka još nije izglasala (retry će probati ponovo)")
	}
	var remoteVote domain.TransactionVote
	if err := json.Unmarshal([]byte(*logRow.ResponsePayload), &remoteVote); err != nil {
		_ = c.executor.Rollback(ctx, ibTx.ID)
		_ = c.repo.UpdateTransactionStatus(ctx, ibTx.ID, domain.TxStatusFailed, "PARSE_REMOTE_VOTE", err.Error())
		return ibTx, err
	}
	if remoteVote.Vote == domain.VoteNo {
		_ = c.executor.Rollback(ctx, ibTx.ID)
		_ = c.repo.UpdateTransactionStatus(ctx, ibTx.ID, domain.TxStatusRolledBack, "REMOTE_VOTE_NO", joinReasons(remoteVote.Reasons))
		return ibTx, fmt.Errorf("interbank payment: druga banka odbila: %s", joinReasons(remoteVote.Reasons))
	}

	// 5) Lokalni commit + COMMIT_TX poruka
	if err := c.executor.Commit(ctx, ibTx.ID); err != nil {
		// Lokalni commit pukao posle YES/YES → kritičan stanje, mark FAILED, traži operatora.
		_ = c.repo.UpdateTransactionStatus(ctx, ibTx.ID, domain.TxStatusFailed, "LOCAL_COMMIT", err.Error())
		return ibTx, fmt.Errorf("interbank payment: lokalni commit pukao: %w", err)
	}
	commitKey := domain.IdempotenceKey{
		RoutingNumber:       c.ourRoutingNumber,
		LocallyGeneratedKey: NewLocalKey(),
	}
	commitRow, err := c.msgSvc.EnqueueOutgoing(ctx, domain.MessageCommitTx, c.peerRoutingNumber, commitKey,
		domain.CommitTransaction{TransactionID: txID})
	if err == nil {
		_ = c.msgSvc.SendMessage(ctx, commitRow)
	}

	_ = c.repo.UpdateTransactionStatus(ctx, ibTx.ID, domain.TxStatusCommitted, "COMMITTED", "")
	return ibTx, nil
}

// InitiateInterbankTransaction — generička verzija za bilo koji Transaction
// (npr. exercise OTC option). Pokreće isti 2-phase commit ciklus kao i
// InitiateInterbankPayment. Vraća zapis interbank_transaction.
func (c *TransactionCoordinator) InitiateInterbankTransaction(ctx context.Context, transaction domain.Transaction, initiatorUserID *int64) (*domain.InterbankTransaction, error) {
	payloadStr, err := MarshalTransaction(transaction)
	if err != nil {
		return nil, err
	}
	ibTx := &domain.InterbankTransaction{
		TransactionRoutingNumber: transaction.TransactionID.RoutingNumber,
		TransactionForeignID:     transaction.TransactionID.ID,
		Role:                     domain.TxRoleCoordinator,
		Status:                   domain.TxStatusNew,
		CurrentStep:              "PREPARING_LOCAL",
		Payload:                  payloadStr,
		InitiatorUserID:          initiatorUserID,
	}
	if err := c.repo.CreateTransaction(ctx, ibTx); err != nil {
		return nil, err
	}

	vote, err := c.executor.Prepare(ctx, ibTx.ID, transaction)
	if err != nil {
		_ = c.repo.UpdateTransactionStatus(ctx, ibTx.ID, domain.TxStatusFailed, "LOCAL_PREPARE_ERROR", err.Error())
		return ibTx, err
	}
	if vote.Vote == domain.VoteNo {
		_ = c.repo.UpdateTransactionStatus(ctx, ibTx.ID, domain.TxStatusFailed, "LOCAL_PREPARE_NO", joinReasons(vote.Reasons))
		return ibTx, fmt.Errorf("interbank tx: lokalna priprema vraća NO: %s", joinReasons(vote.Reasons))
	}
	_ = c.repo.UpdateTransactionStatus(ctx, ibTx.ID, domain.TxStatusPrepared, "LOCAL_PREPARED", "")

	// Identifikuj koje banke su u igri (pojednostavljeno: gledamo samo peer).
	hasRemote := transactionHasRemote(transaction, c.ourRoutingNumber, c.accountPrefix)
	if !hasRemote {
		// Sve lokalno → odmah commit.
		if err := c.executor.Commit(ctx, ibTx.ID); err != nil {
			_ = c.repo.UpdateTransactionStatus(ctx, ibTx.ID, domain.TxStatusFailed, "LOCAL_COMMIT", err.Error())
			return ibTx, err
		}
		return ibTx, nil
	}

	// Inter-bank: NEW_TX → vote → COMMIT/ROLLBACK
	idemKey := domain.IdempotenceKey{RoutingNumber: c.ourRoutingNumber, LocallyGeneratedKey: NewLocalKey()}
	logRow, err := c.msgSvc.EnqueueOutgoing(ctx, domain.MessageNewTx, c.peerRoutingNumber, idemKey, transaction)
	if err != nil {
		_ = c.executor.Rollback(ctx, ibTx.ID)
		_ = c.repo.UpdateTransactionStatus(ctx, ibTx.ID, domain.TxStatusFailed, "ENQUEUE_NEW_TX", err.Error())
		return ibTx, err
	}
	if err := c.msgSvc.SendMessage(ctx, logRow); err != nil {
		_ = c.executor.Rollback(ctx, ibTx.ID)
		_ = c.repo.UpdateTransactionStatus(ctx, ibTx.ID, domain.TxStatusFailed, "SEND_NEW_TX", err.Error())
		return ibTx, err
	}
	if logRow.Status != domain.MsgStatusSent || logRow.ResponsePayload == nil {
		_ = c.repo.UpdateTransactionStatus(ctx, ibTx.ID, domain.TxStatusFailed, "AWAITING_REMOTE_VOTE", "remote not done")
		return ibTx, fmt.Errorf("interbank tx: druga banka još nije izglasala")
	}
	var remoteVote domain.TransactionVote
	if err := json.Unmarshal([]byte(*logRow.ResponsePayload), &remoteVote); err != nil {
		_ = c.executor.Rollback(ctx, ibTx.ID)
		_ = c.repo.UpdateTransactionStatus(ctx, ibTx.ID, domain.TxStatusFailed, "PARSE_REMOTE_VOTE", err.Error())
		return ibTx, err
	}
	if remoteVote.Vote == domain.VoteNo {
		_ = c.executor.Rollback(ctx, ibTx.ID)
		rollKey := domain.IdempotenceKey{RoutingNumber: c.ourRoutingNumber, LocallyGeneratedKey: NewLocalKey()}
		if rb, err := c.msgSvc.EnqueueOutgoing(ctx, domain.MessageRollbackTx, c.peerRoutingNumber, rollKey,
			domain.RollbackTransaction{TransactionID: transaction.TransactionID}); err == nil {
			_ = c.msgSvc.SendMessage(ctx, rb)
		}
		_ = c.repo.UpdateTransactionStatus(ctx, ibTx.ID, domain.TxStatusRolledBack, "REMOTE_VOTE_NO", joinReasons(remoteVote.Reasons))
		return ibTx, fmt.Errorf("interbank tx: druga banka odbila: %s", joinReasons(remoteVote.Reasons))
	}
	if err := c.executor.Commit(ctx, ibTx.ID); err != nil {
		_ = c.repo.UpdateTransactionStatus(ctx, ibTx.ID, domain.TxStatusFailed, "LOCAL_COMMIT", err.Error())
		return ibTx, err
	}
	commitKey := domain.IdempotenceKey{RoutingNumber: c.ourRoutingNumber, LocallyGeneratedKey: NewLocalKey()}
	if cm, err := c.msgSvc.EnqueueOutgoing(ctx, domain.MessageCommitTx, c.peerRoutingNumber, commitKey,
		domain.CommitTransaction{TransactionID: transaction.TransactionID}); err == nil {
		_ = c.msgSvc.SendMessage(ctx, cm)
	}
	_ = c.repo.UpdateTransactionStatus(ctx, ibTx.ID, domain.TxStatusCommitted, "COMMITTED", "")
	return ibTx, nil
}

// transactionHasRemote — true ako bilo koji posting nije lokalni.
func transactionHasRemote(tx domain.Transaction, ourRoutingNumber int64, accountPrefix string) bool {
	for _, p := range tx.Postings {
		switch p.Account.Type {
		case domain.AccountKindAccount:
			if p.Account.Num != nil && !strings.HasPrefix(*p.Account.Num, accountPrefix) {
				return true
			}
		case domain.AccountKindPerson, domain.AccountKindOption:
			if p.Account.ID != nil && p.Account.ID.RoutingNumber != ourRoutingNumber {
				return true
			}
		}
	}
	return false
}

// ─── HandleIncomingMessage ───────────────────────────────────────────────────

// HandleIncomingMessage je entry point za POST /interbank.
//
// Vraća: HTTP statusCode + response (struct ili nil) + error.
// Idempotency: ako poruka sa tim ključem već postoji, vraća se identičan odgovor.
func (c *TransactionCoordinator) HandleIncomingMessage(ctx context.Context, msg domain.InterbankMessage) (int, interface{}, error) {
	// Validacija ključa
	if msg.IdempotenceKey.LocallyGeneratedKey == "" || len(msg.IdempotenceKey.LocallyGeneratedKey) > 64 {
		return http.StatusBadRequest, nil, fmt.Errorf("idempotenceKey nedostaje ili je predugačak")
	}

	// 1) Idempotency lookup
	prev, err := c.msgSvc.LookupIncomingResponse(ctx, msg.IdempotenceKey)
	if err != nil {
		return http.StatusInternalServerError, nil, err
	}
	if prev != nil {
		// Vrati identičan odgovor.
		statusCode := http.StatusOK
		if prev.ResponseStatusCode != nil {
			statusCode = *prev.ResponseStatusCode
		}
		var body interface{}
		if prev.ResponsePayload != nil {
			body = json.RawMessage(*prev.ResponsePayload)
		}
		return statusCode, body, nil
	}

	// 2) Snimi INCOMING zapis pre obrade
	row, err := c.msgSvc.RecordIncoming(ctx, msg)
	if err != nil {
		return http.StatusInternalServerError, nil, err
	}

	// 3) Dispatch — svaka case-grana setuje statusCode pre korišćenja.
	var statusCode int
	var responseBody interface{}

	switch msg.MessageType {
	case domain.MessageNewTx:
		var tx domain.Transaction
		if err := json.Unmarshal(msg.Message, &tx); err != nil {
			return http.StatusBadRequest, nil, fmt.Errorf("nevalidan NEW_TX payload: %w", err)
		}
		// Snimi interbank_transaction sa Role=PARTICIPANT
		ibTx := &domain.InterbankTransaction{
			TransactionRoutingNumber: tx.TransactionID.RoutingNumber,
			TransactionForeignID:     tx.TransactionID.ID,
			Role:                     domain.TxRoleParticipant,
			Status:                   domain.TxStatusNew,
			CurrentStep:              "PARTICIPANT_PREPARING",
			Payload:                  string(msg.Message),
		}
		// Idempotentno: ako već postoji (npr. retry od koordinatora pre nego smo videli idempotenceKey)
		existing, _ := c.repo.GetTransactionByForeignID(ctx, ibTx.TransactionRoutingNumber, ibTx.TransactionForeignID)
		if existing != nil {
			ibTx = existing
		} else {
			if err := c.repo.CreateTransaction(ctx, ibTx); err != nil {
				return http.StatusInternalServerError, nil, err
			}
		}

		vote, err := c.executor.Prepare(ctx, ibTx.ID, tx)
		if err != nil {
			_ = c.repo.UpdateTransactionStatus(ctx, ibTx.ID, domain.TxStatusFailed, "PREPARE_ERROR", err.Error())
			return http.StatusInternalServerError, nil, err
		}
		if vote.Vote == domain.VoteYes {
			_ = c.repo.UpdateTransactionStatus(ctx, ibTx.ID, domain.TxStatusPrepared, "PREPARED", "")
		} else {
			_ = c.repo.UpdateTransactionStatus(ctx, ibTx.ID, domain.TxStatusFailed, "VOTE_NO", joinReasons(vote.Reasons))
		}
		responseBody = vote
		statusCode = http.StatusOK

	case domain.MessageCommitTx:
		var ct domain.CommitTransaction
		if err := json.Unmarshal(msg.Message, &ct); err != nil {
			return http.StatusBadRequest, nil, fmt.Errorf("nevalidan COMMIT_TX payload: %w", err)
		}
		ibTx, err := c.repo.GetTransactionByForeignID(ctx, ct.TransactionID.RoutingNumber, ct.TransactionID.ID)
		if err != nil {
			return http.StatusInternalServerError, nil, err
		}
		if ibTx == nil {
			return http.StatusNotFound, nil, fmt.Errorf("transakcija nije pronađena")
		}
		if ibTx.Status == domain.TxStatusCommitted {
			// Već komitovano — idempotentno OK.
			statusCode = http.StatusNoContent
			responseBody = nil
			break
		}
		if err := c.executor.Commit(ctx, ibTx.ID); err != nil {
			_ = c.repo.UpdateTransactionStatus(ctx, ibTx.ID, domain.TxStatusFailed, "COMMIT_ERROR", err.Error())
			return http.StatusInternalServerError, nil, err
		}
		statusCode = http.StatusNoContent
		responseBody = nil

	case domain.MessageRollbackTx:
		var rt domain.RollbackTransaction
		if err := json.Unmarshal(msg.Message, &rt); err != nil {
			return http.StatusBadRequest, nil, fmt.Errorf("nevalidan ROLLBACK_TX payload: %w", err)
		}
		ibTx, err := c.repo.GetTransactionByForeignID(ctx, rt.TransactionID.RoutingNumber, rt.TransactionID.ID)
		if err != nil {
			return http.StatusInternalServerError, nil, err
		}
		if ibTx == nil {
			// Ne postoji — idempotentno OK.
			statusCode = http.StatusNoContent
			responseBody = nil
			break
		}
		if ibTx.Status == domain.TxStatusRolledBack {
			statusCode = http.StatusNoContent
			responseBody = nil
			break
		}
		if err := c.executor.Rollback(ctx, ibTx.ID); err != nil {
			_ = c.repo.UpdateTransactionStatus(ctx, ibTx.ID, domain.TxStatusFailed, "ROLLBACK_ERROR", err.Error())
			return http.StatusInternalServerError, nil, err
		}
		statusCode = http.StatusNoContent
		responseBody = nil

	default:
		return http.StatusBadRequest, nil, fmt.Errorf("nepoznat messageType: %s", msg.MessageType)
	}

	// 4) Zapamti odgovor (idempotency cache)
	_ = c.msgSvc.FinishIncoming(ctx, row, statusCode, responseBody)
	return statusCode, responseBody, nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func ptrInt64(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

func joinReasons(rs []domain.NoVoteReason) string {
	if len(rs) == 0 {
		return ""
	}
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, string(r.Reason))
	}
	return strings.Join(out, ",")
}

// suppress unused-import warning for time
var _ = time.Now

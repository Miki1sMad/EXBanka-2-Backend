// Package repository — interbank_repository.go
//
// Persistencija interbank modula: message log, transaction state, OTC
// negotiations i opcioni ugovori. Sve operacije rade preko GORM-a u istoj
// šemi (core_banking) kao i ostatak servisa.
package repository

import (
	"context"
	"errors"
	"time"

	"banka-backend/services/bank-service/internal/domain"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// ─── GORM modeli ─────────────────────────────────────────────────────────────

type interbankMessageModel struct {
	ID                       int64           `gorm:"column:id;primaryKey;autoIncrement"`
	Direction                string          `gorm:"column:direction"`
	MessageType              string          `gorm:"column:message_type"`
	IdempotenceRoutingNumber int64           `gorm:"column:idempotence_routing_number"`
	IdempotenceLocalKey      string          `gorm:"column:idempotence_local_key"`
	TargetRoutingNumber      *int64          `gorm:"column:target_routing_number"`
	Payload                  string          `gorm:"column:payload"`
	ResponsePayload          *string         `gorm:"column:response_payload"`
	ResponseStatusCode       *int            `gorm:"column:response_status_code"`
	Status                   string          `gorm:"column:status"`
	RetryCount               int             `gorm:"column:retry_count"`
	NextRetryAt              *time.Time      `gorm:"column:next_retry_at"`
	LastError                string          `gorm:"column:last_error"`
	CreatedAt                time.Time       `gorm:"column:created_at"`
	UpdatedAt                time.Time       `gorm:"column:updated_at"`
}

func (interbankMessageModel) TableName() string { return "core_banking.interbank_message_log" }

type interbankTxModel struct {
	ID                       int64     `gorm:"column:id;primaryKey;autoIncrement"`
	TransactionRoutingNumber int64     `gorm:"column:transaction_routing_number"`
	TransactionForeignID     string    `gorm:"column:transaction_foreign_id"`
	Role                     string    `gorm:"column:role"`
	Status                   string    `gorm:"column:status"`
	CurrentStep              string    `gorm:"column:current_step"`
	Payload                  string    `gorm:"column:payload"`
	FailureReason            string    `gorm:"column:failure_reason"`
	InitiatorUserID          *int64    `gorm:"column:initiator_user_id"`
	InitiatorAccountID       *int64    `gorm:"column:initiator_account_id"`
	CreatedAt                time.Time `gorm:"column:created_at"`
	UpdatedAt                time.Time `gorm:"column:updated_at"`
}

func (interbankTxModel) TableName() string { return "core_banking.interbank_transaction" }

type interbankReservationModel struct {
	ID                       int64           `gorm:"column:id;primaryKey;autoIncrement"`
	InterbankTransactionID   int64           `gorm:"column:interbank_transaction_id"`
	PostingIndex             int             `gorm:"column:posting_index"`
	AccountKind              string          `gorm:"column:account_kind"`
	AccountNum               *string         `gorm:"column:account_num"`
	ForeignRoutingNumber     *int64          `gorm:"column:foreign_routing_number"`
	ForeignID                *string         `gorm:"column:foreign_id"`
	AssetType                string          `gorm:"column:asset_type"`
	AssetCurrency            *string         `gorm:"column:asset_currency"`
	AssetTicker              *string         `gorm:"column:asset_ticker"`
	AssetNegotiationRouting  *int64          `gorm:"column:asset_negotiation_routing"`
	AssetNegotiationLocalID  *string         `gorm:"column:asset_negotiation_local_id"`
	Amount                   decimal.Decimal `gorm:"column:amount"`
	Reserved                 bool            `gorm:"column:reserved"`
	CreatedAt                time.Time       `gorm:"column:created_at"`
}

func (interbankReservationModel) TableName() string { return "core_banking.interbank_reservation" }

type interbankNegotiationModel struct {
	ID                          int64           `gorm:"column:id;primaryKey;autoIncrement"`
	NegotiationRoutingNumber    int64           `gorm:"column:negotiation_routing_number"`
	NegotiationForeignID        string          `gorm:"column:negotiation_foreign_id"`
	StockTicker                 string          `gorm:"column:stock_ticker"`
	SettlementDate              time.Time       `gorm:"column:settlement_date"`
	PriceCurrency               string          `gorm:"column:price_currency"`
	PriceAmount                 decimal.Decimal `gorm:"column:price_amount"`
	PremiumCurrency             string          `gorm:"column:premium_currency"`
	PremiumAmount               decimal.Decimal `gorm:"column:premium_amount"`
	Amount                      int32           `gorm:"column:amount"`
	BuyerRoutingNumber          int64           `gorm:"column:buyer_routing_number"`
	BuyerID                     string          `gorm:"column:buyer_id"`
	SellerRoutingNumber         int64           `gorm:"column:seller_routing_number"`
	SellerID                    string          `gorm:"column:seller_id"`
	LastModifiedRoutingNumber   int64           `gorm:"column:last_modified_routing_number"`
	LastModifiedID              string          `gorm:"column:last_modified_id"`
	IsOngoing                   bool            `gorm:"column:is_ongoing"`
	Status                      string          `gorm:"column:status"`
	CreatedAt                   time.Time       `gorm:"column:created_at"`
	UpdatedAt                   time.Time       `gorm:"column:updated_at"`
}

func (interbankNegotiationModel) TableName() string { return "core_banking.interbank_negotiation" }

type interbankOptionContractModel struct {
	ID                       int64           `gorm:"column:id;primaryKey;autoIncrement"`
	NegotiationRoutingNumber int64           `gorm:"column:negotiation_routing_number"`
	NegotiationForeignID     string          `gorm:"column:negotiation_foreign_id"`
	StockTicker              string          `gorm:"column:stock_ticker"`
	PriceCurrency            string          `gorm:"column:price_currency"`
	PriceAmount              decimal.Decimal `gorm:"column:price_amount"`
	PremiumCurrency          string          `gorm:"column:premium_currency"`
	PremiumAmount            decimal.Decimal `gorm:"column:premium_amount"`
	SettlementDate           time.Time       `gorm:"column:settlement_date"`
	Amount                   int32           `gorm:"column:amount"`
	BuyerRoutingNumber       int64           `gorm:"column:buyer_routing_number"`
	BuyerID                  string          `gorm:"column:buyer_id"`
	SellerRoutingNumber      int64           `gorm:"column:seller_routing_number"`
	SellerID                 string          `gorm:"column:seller_id"`
	Status                   string          `gorm:"column:status"`
	UsedAt                   *time.Time      `gorm:"column:used_at"`
	CreatedAt                time.Time       `gorm:"column:created_at"`
	UpdatedAt                time.Time       `gorm:"column:updated_at"`
}

func (interbankOptionContractModel) TableName() string { return "core_banking.interbank_option_contract" }

// ─── Repository implementacija ───────────────────────────────────────────────

type interbankRepository struct {
	db *gorm.DB
}

// NewInterbankRepository konstruktor.
func NewInterbankRepository(db *gorm.DB) domain.InterbankRepository {
	return &interbankRepository{db: db}
}

// ── Helpers (model ↔ domain) ──────────────────────────────────────────────────

func msgFromDomain(m *domain.InterbankMessageLog) *interbankMessageModel {
	return &interbankMessageModel{
		ID:                       m.ID,
		Direction:                string(m.Direction),
		MessageType:              string(m.MessageType),
		IdempotenceRoutingNumber: m.IdempotenceRoutingNumber,
		IdempotenceLocalKey:      m.IdempotenceLocalKey,
		TargetRoutingNumber:      m.TargetRoutingNumber,
		Payload:                  m.Payload,
		ResponsePayload:          m.ResponsePayload,
		ResponseStatusCode:       m.ResponseStatusCode,
		Status:                   string(m.Status),
		RetryCount:               m.RetryCount,
		NextRetryAt:              m.NextRetryAt,
		LastError:                m.LastError,
		CreatedAt:                m.CreatedAt,
		UpdatedAt:                m.UpdatedAt,
	}
}

func msgToDomain(m *interbankMessageModel) *domain.InterbankMessageLog {
	return &domain.InterbankMessageLog{
		ID:                       m.ID,
		Direction:                domain.InterbankMessageDirection(m.Direction),
		MessageType:              domain.MessageType(m.MessageType),
		IdempotenceRoutingNumber: m.IdempotenceRoutingNumber,
		IdempotenceLocalKey:      m.IdempotenceLocalKey,
		TargetRoutingNumber:      m.TargetRoutingNumber,
		Payload:                  m.Payload,
		ResponsePayload:          m.ResponsePayload,
		ResponseStatusCode:       m.ResponseStatusCode,
		Status:                   domain.InterbankMessageStatus(m.Status),
		RetryCount:               m.RetryCount,
		NextRetryAt:              m.NextRetryAt,
		LastError:                m.LastError,
		CreatedAt:                m.CreatedAt,
		UpdatedAt:                m.UpdatedAt,
	}
}

func txFromDomain(t *domain.InterbankTransaction) *interbankTxModel {
	return &interbankTxModel{
		ID:                       t.ID,
		TransactionRoutingNumber: t.TransactionRoutingNumber,
		TransactionForeignID:     t.TransactionForeignID,
		Role:                     string(t.Role),
		Status:                   string(t.Status),
		CurrentStep:              t.CurrentStep,
		Payload:                  t.Payload,
		FailureReason:            t.FailureReason,
		InitiatorUserID:          t.InitiatorUserID,
		InitiatorAccountID:       t.InitiatorAccountID,
		CreatedAt:                t.CreatedAt,
		UpdatedAt:                t.UpdatedAt,
	}
}

func txToDomain(m *interbankTxModel) *domain.InterbankTransaction {
	return &domain.InterbankTransaction{
		ID:                       m.ID,
		TransactionRoutingNumber: m.TransactionRoutingNumber,
		TransactionForeignID:     m.TransactionForeignID,
		Role:                     domain.InterbankTxRole(m.Role),
		Status:                   domain.InterbankTxStatus(m.Status),
		CurrentStep:              m.CurrentStep,
		Payload:                  m.Payload,
		FailureReason:            m.FailureReason,
		InitiatorUserID:          m.InitiatorUserID,
		InitiatorAccountID:       m.InitiatorAccountID,
		CreatedAt:                m.CreatedAt,
		UpdatedAt:                m.UpdatedAt,
	}
}

func resvFromDomain(r *domain.InterbankReservation) *interbankReservationModel {
	return &interbankReservationModel{
		ID:                      r.ID,
		InterbankTransactionID:  r.InterbankTransactionID,
		PostingIndex:            r.PostingIndex,
		AccountKind:             string(r.AccountKind),
		AccountNum:              r.AccountNum,
		ForeignRoutingNumber:    r.ForeignRoutingNumber,
		ForeignID:               r.ForeignID,
		AssetType:               string(r.AssetType),
		AssetCurrency:           r.AssetCurrency,
		AssetTicker:             r.AssetTicker,
		AssetNegotiationRouting: r.AssetNegotiationRouting,
		AssetNegotiationLocalID: r.AssetNegotiationLocalID,
		Amount:                  r.Amount,
		Reserved:                r.Reserved,
		CreatedAt:               r.CreatedAt,
	}
}

func resvToDomain(m *interbankReservationModel) domain.InterbankReservation {
	return domain.InterbankReservation{
		ID:                      m.ID,
		InterbankTransactionID:  m.InterbankTransactionID,
		PostingIndex:            m.PostingIndex,
		AccountKind:             domain.AccountKind(m.AccountKind),
		AccountNum:              m.AccountNum,
		ForeignRoutingNumber:    m.ForeignRoutingNumber,
		ForeignID:               m.ForeignID,
		AssetType:               domain.AssetType(m.AssetType),
		AssetCurrency:           m.AssetCurrency,
		AssetTicker:             m.AssetTicker,
		AssetNegotiationRouting: m.AssetNegotiationRouting,
		AssetNegotiationLocalID: m.AssetNegotiationLocalID,
		Amount:                  m.Amount,
		Reserved:                m.Reserved,
		CreatedAt:               m.CreatedAt,
	}
}

func negFromDomain(n *domain.InterbankNegotiation) *interbankNegotiationModel {
	return &interbankNegotiationModel{
		ID:                        n.ID,
		NegotiationRoutingNumber:  n.NegotiationRoutingNumber,
		NegotiationForeignID:      n.NegotiationForeignID,
		StockTicker:               n.StockTicker,
		SettlementDate:            n.SettlementDate,
		PriceCurrency:             n.PriceCurrency,
		PriceAmount:               n.PriceAmount,
		PremiumCurrency:           n.PremiumCurrency,
		PremiumAmount:             n.PremiumAmount,
		Amount:                    n.Amount,
		BuyerRoutingNumber:        n.BuyerRoutingNumber,
		BuyerID:                   n.BuyerID,
		SellerRoutingNumber:       n.SellerRoutingNumber,
		SellerID:                  n.SellerID,
		LastModifiedRoutingNumber: n.LastModifiedRoutingNumber,
		LastModifiedID:            n.LastModifiedID,
		IsOngoing:                 n.IsOngoing,
		Status:                    n.Status,
		CreatedAt:                 n.CreatedAt,
		UpdatedAt:                 n.UpdatedAt,
	}
}

func negToDomain(m *interbankNegotiationModel) *domain.InterbankNegotiation {
	return &domain.InterbankNegotiation{
		ID:                        m.ID,
		NegotiationRoutingNumber:  m.NegotiationRoutingNumber,
		NegotiationForeignID:      m.NegotiationForeignID,
		StockTicker:               m.StockTicker,
		SettlementDate:            m.SettlementDate,
		PriceCurrency:             m.PriceCurrency,
		PriceAmount:               m.PriceAmount,
		PremiumCurrency:           m.PremiumCurrency,
		PremiumAmount:             m.PremiumAmount,
		Amount:                    m.Amount,
		BuyerRoutingNumber:        m.BuyerRoutingNumber,
		BuyerID:                   m.BuyerID,
		SellerRoutingNumber:       m.SellerRoutingNumber,
		SellerID:                  m.SellerID,
		LastModifiedRoutingNumber: m.LastModifiedRoutingNumber,
		LastModifiedID:            m.LastModifiedID,
		IsOngoing:                 m.IsOngoing,
		Status:                    m.Status,
		CreatedAt:                 m.CreatedAt,
		UpdatedAt:                 m.UpdatedAt,
	}
}

func optFromDomain(c *domain.InterbankOptionContract) *interbankOptionContractModel {
	return &interbankOptionContractModel{
		ID:                       c.ID,
		NegotiationRoutingNumber: c.NegotiationRoutingNumber,
		NegotiationForeignID:     c.NegotiationForeignID,
		StockTicker:              c.StockTicker,
		PriceCurrency:            c.PriceCurrency,
		PriceAmount:              c.PriceAmount,
		PremiumCurrency:          c.PremiumCurrency,
		PremiumAmount:            c.PremiumAmount,
		SettlementDate:           c.SettlementDate,
		Amount:                   c.Amount,
		BuyerRoutingNumber:       c.BuyerRoutingNumber,
		BuyerID:                  c.BuyerID,
		SellerRoutingNumber:      c.SellerRoutingNumber,
		SellerID:                 c.SellerID,
		Status:                   c.Status,
		UsedAt:                   c.UsedAt,
		CreatedAt:                c.CreatedAt,
		UpdatedAt:                c.UpdatedAt,
	}
}

func optToDomain(m *interbankOptionContractModel) *domain.InterbankOptionContract {
	return &domain.InterbankOptionContract{
		ID:                       m.ID,
		NegotiationRoutingNumber: m.NegotiationRoutingNumber,
		NegotiationForeignID:     m.NegotiationForeignID,
		StockTicker:              m.StockTicker,
		PriceCurrency:            m.PriceCurrency,
		PriceAmount:              m.PriceAmount,
		PremiumCurrency:          m.PremiumCurrency,
		PremiumAmount:            m.PremiumAmount,
		SettlementDate:           m.SettlementDate,
		Amount:                   m.Amount,
		BuyerRoutingNumber:       m.BuyerRoutingNumber,
		BuyerID:                  m.BuyerID,
		SellerRoutingNumber:      m.SellerRoutingNumber,
		SellerID:                 m.SellerID,
		Status:                   m.Status,
		UsedAt:                   m.UsedAt,
		CreatedAt:                m.CreatedAt,
		UpdatedAt:                m.UpdatedAt,
	}
}

// ── Message log ──────────────────────────────────────────────────────────────

func (r *interbankRepository) GetIncomingByIdempotence(ctx context.Context, routingNumber int64, localKey string) (*domain.InterbankMessageLog, error) {
	var m interbankMessageModel
	err := r.db.WithContext(ctx).
		Where("direction = ? AND idempotence_routing_number = ? AND idempotence_local_key = ?",
			string(domain.DirectionIncoming), routingNumber, localKey).
		Take(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return msgToDomain(&m), nil
}

func (r *interbankRepository) GetOutgoingByIdempotence(ctx context.Context, routingNumber int64, localKey string) (*domain.InterbankMessageLog, error) {
	var m interbankMessageModel
	err := r.db.WithContext(ctx).
		Where("direction = ? AND idempotence_routing_number = ? AND idempotence_local_key = ?",
			string(domain.DirectionOutgoing), routingNumber, localKey).
		Take(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return msgToDomain(&m), nil
}

func (r *interbankRepository) CreateMessage(ctx context.Context, m *domain.InterbankMessageLog) error {
	mod := msgFromDomain(m)
	if mod.CreatedAt.IsZero() {
		now := time.Now().UTC()
		mod.CreatedAt = now
		mod.UpdatedAt = now
	}
	if err := r.db.WithContext(ctx).Create(mod).Error; err != nil {
		return err
	}
	m.ID = mod.ID
	m.CreatedAt = mod.CreatedAt
	m.UpdatedAt = mod.UpdatedAt
	return nil
}

func (r *interbankRepository) UpdateMessage(ctx context.Context, m *domain.InterbankMessageLog) error {
	m.UpdatedAt = time.Now().UTC()
	mod := msgFromDomain(m)
	return r.db.WithContext(ctx).Save(mod).Error
}

func (r *interbankRepository) ListPendingOutgoing(ctx context.Context, limit int) ([]domain.InterbankMessageLog, error) {
	var rows []interbankMessageModel
	err := r.db.WithContext(ctx).
		Where("direction = ? AND status IN ?", string(domain.DirectionOutgoing),
			[]string{string(domain.MsgStatusPending), string(domain.MsgStatusAccepted), string(domain.MsgStatusFailed)}).
		Where("(next_retry_at IS NULL OR next_retry_at <= ?)", time.Now().UTC()).
		Order("created_at ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]domain.InterbankMessageLog, 0, len(rows))
	for i := range rows {
		out = append(out, *msgToDomain(&rows[i]))
	}
	return out, nil
}

// ── Interbank transaction ────────────────────────────────────────────────────

func (r *interbankRepository) CreateTransaction(ctx context.Context, t *domain.InterbankTransaction) error {
	mod := txFromDomain(t)
	if mod.CreatedAt.IsZero() {
		now := time.Now().UTC()
		mod.CreatedAt = now
		mod.UpdatedAt = now
	}
	if err := r.db.WithContext(ctx).Create(mod).Error; err != nil {
		return err
	}
	t.ID = mod.ID
	t.CreatedAt = mod.CreatedAt
	t.UpdatedAt = mod.UpdatedAt
	return nil
}

func (r *interbankRepository) GetTransactionByForeignID(ctx context.Context, routingNumber int64, foreignID string) (*domain.InterbankTransaction, error) {
	var m interbankTxModel
	err := r.db.WithContext(ctx).
		Where("transaction_routing_number = ? AND transaction_foreign_id = ?", routingNumber, foreignID).
		Take(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return txToDomain(&m), nil
}

func (r *interbankRepository) UpdateTransactionStatus(ctx context.Context, id int64, status domain.InterbankTxStatus, step, failureReason string) error {
	updates := map[string]interface{}{
		"status":         string(status),
		"current_step":   step,
		"failure_reason": failureReason,
		"updated_at":     time.Now().UTC(),
	}
	return r.db.WithContext(ctx).Model(&interbankTxModel{}).Where("id = ?", id).Updates(updates).Error
}

// ── Reservations ─────────────────────────────────────────────────────────────

func (r *interbankRepository) CreateReservation(ctx context.Context, x *domain.InterbankReservation) error {
	mod := resvFromDomain(x)
	if mod.CreatedAt.IsZero() {
		mod.CreatedAt = time.Now().UTC()
	}
	if err := r.db.WithContext(ctx).Create(mod).Error; err != nil {
		return err
	}
	x.ID = mod.ID
	x.CreatedAt = mod.CreatedAt
	return nil
}

func (r *interbankRepository) ListReservationsByTx(ctx context.Context, txID int64) ([]domain.InterbankReservation, error) {
	var rows []interbankReservationModel
	if err := r.db.WithContext(ctx).
		Where("interbank_transaction_id = ?", txID).
		Order("posting_index ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.InterbankReservation, 0, len(rows))
	for i := range rows {
		out = append(out, resvToDomain(&rows[i]))
	}
	return out, nil
}

// ── Negotiations ─────────────────────────────────────────────────────────────

func (r *interbankRepository) CreateNegotiation(ctx context.Context, n *domain.InterbankNegotiation) error {
	mod := negFromDomain(n)
	if mod.CreatedAt.IsZero() {
		now := time.Now().UTC()
		mod.CreatedAt = now
		mod.UpdatedAt = now
	}
	if err := r.db.WithContext(ctx).Create(mod).Error; err != nil {
		return err
	}
	n.ID = mod.ID
	n.CreatedAt = mod.CreatedAt
	n.UpdatedAt = mod.UpdatedAt
	return nil
}

func (r *interbankRepository) GetNegotiationByID(ctx context.Context, routingNumber int64, foreignID string) (*domain.InterbankNegotiation, error) {
	var m interbankNegotiationModel
	err := r.db.WithContext(ctx).
		Where("negotiation_routing_number = ? AND negotiation_foreign_id = ?", routingNumber, foreignID).
		Take(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return negToDomain(&m), nil
}

func (r *interbankRepository) UpdateNegotiation(ctx context.Context, n *domain.InterbankNegotiation) error {
	n.UpdatedAt = time.Now().UTC()
	mod := negFromDomain(n)
	return r.db.WithContext(ctx).Save(mod).Error
}

// ListPublicStocks agregira public_shares iz lokalnog portfolija po (listing, user)
// — public_shares.quantity je već "javni režim" za OTC. Vraća listu po tickerima.
func (r *interbankRepository) ListPublicStocks(ctx context.Context) ([]domain.PublicStock, error) {
	type row struct {
		Ticker  string `gorm:"column:ticker"`
		UserID  int64  `gorm:"column:user_id"`
		Qty     int32  `gorm:"column:qty"`
	}
	var rows []row
	err := r.db.WithContext(ctx).Raw(`
		SELECT l.ticker AS ticker, ps.user_id AS user_id, SUM(ps.quantity)::int AS qty
		FROM core_banking.public_shares ps
		JOIN core_banking.listing l ON l.id = ps.listing_id
		GROUP BY l.ticker, ps.user_id
		HAVING SUM(ps.quantity) > 0
		ORDER BY l.ticker
	`).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	// Grupiši po tickeru.
	byTicker := map[string][]domain.PublicStockSeller{}
	order := []string{}
	for _, rr := range rows {
		key := rr.Ticker
		if _, ok := byTicker[key]; !ok {
			order = append(order, key)
		}
		byTicker[key] = append(byTicker[key], domain.PublicStockSeller{
			Seller: domain.ForeignBankId{
				RoutingNumber: 0, // popunjava service layer iz cfg.InterbankRoutingNumber
				ID:            int64ToString(rr.UserID),
			},
			Amount: rr.Qty,
		})
	}
	out := make([]domain.PublicStock, 0, len(order))
	for _, t := range order {
		out = append(out, domain.PublicStock{
			Stock:   domain.StockDescription{Ticker: t},
			Sellers: byTicker[t],
		})
	}
	return out, nil
}

// ── Option contracts ─────────────────────────────────────────────────────────

func (r *interbankRepository) CreateOptionContract(ctx context.Context, c *domain.InterbankOptionContract) error {
	mod := optFromDomain(c)
	if mod.CreatedAt.IsZero() {
		now := time.Now().UTC()
		mod.CreatedAt = now
		mod.UpdatedAt = now
	}
	if err := r.db.WithContext(ctx).Create(mod).Error; err != nil {
		return err
	}
	c.ID = mod.ID
	c.CreatedAt = mod.CreatedAt
	c.UpdatedAt = mod.UpdatedAt
	return nil
}

func (r *interbankRepository) GetOptionContract(ctx context.Context, routingNumber int64, foreignID string) (*domain.InterbankOptionContract, error) {
	var m interbankOptionContractModel
	err := r.db.WithContext(ctx).
		Where("negotiation_routing_number = ? AND negotiation_foreign_id = ?", routingNumber, foreignID).
		Take(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return optToDomain(&m), nil
}

func (r *interbankRepository) UpdateOptionContractStatus(ctx context.Context, routingNumber int64, foreignID, status string, usedAt *time.Time) error {
	updates := map[string]interface{}{
		"status":     status,
		"used_at":    usedAt,
		"updated_at": time.Now().UTC(),
	}
	return r.db.WithContext(ctx).Model(&interbankOptionContractModel{}).
		Where("negotiation_routing_number = ? AND negotiation_foreign_id = ?", routingNumber, foreignID).
		Updates(updates).Error
}

func (r *interbankRepository) ListContractsForUser(ctx context.Context, routingNumber int64, userID string) ([]domain.InterbankOptionContract, error) {
	var rows []interbankOptionContractModel
	err := r.db.WithContext(ctx).
		Where("(buyer_routing_number = ? AND buyer_id = ?) OR (seller_routing_number = ? AND seller_id = ?)",
			routingNumber, userID, routingNumber, userID).
		Order("created_at DESC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]domain.InterbankOptionContract, 0, len(rows))
	for i := range rows {
		out = append(out, *optToDomain(&rows[i]))
	}
	return out, nil
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// int64ToString — interno (izbegava strconv import na vrhu).
func int64ToString(n int64) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

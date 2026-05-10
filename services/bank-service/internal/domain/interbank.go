// Package domain — interbank.go: DTO modeli i interfejsi po protokolu si-tx-proto.
//
// Spec: https://arsen.srht.site/si-tx-proto/notes.html
//
// Sva polja i JSON oznake odgovaraju definiciji protokola. Decimal vrednosti
// koriste shopspring/decimal kako bi se izbegli plivajući zarezi pri novčanim
// iznosima i količinama hartija.
package domain

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

// ─── Greške interbank modula ─────────────────────────────────────────────────

var (
	ErrInterbankPeerNotConfigured = errors.New("interbank: URL druge banke nije podešen")
	ErrInterbankBadAPIKey         = errors.New("interbank: nevalidan X-Api-Key")
	ErrInterbankNotFound          = errors.New("interbank: resurs nije pronađen")
	ErrInterbankConflict          = errors.New("interbank: konflikt — operacija nije dozvoljena u trenutnom stanju")
	ErrInterbankInvalidPayload    = errors.New("interbank: nevalidan payload poruke")
	ErrInterbankUnbalanced        = errors.New("interbank: transakcija nije balansirana")
)

// ─── Bank & Message identification ───────────────────────────────────────────

// IdempotenceKey — identifikator poruke prema specifikaciji.
//   - routingNumber: routing number banke koja generiše ključ
//   - locallyGeneratedKey: max 64 bajta string
type IdempotenceKey struct {
	RoutingNumber       int64  `json:"routingNumber"`
	LocallyGeneratedKey string `json:"locallyGeneratedKey"`
}

// ForeignBankId — globalni identifikator entiteta (transakcija, korisnik, opcija).
type ForeignBankId struct {
	RoutingNumber int64  `json:"routingNumber"`
	ID            string `json:"id"`
}

// ─── Asset / Account ─────────────────────────────────────────────────────────

// AssetType — diskriminator za Asset.
type AssetType string

const (
	AssetTypeMonas  AssetType = "MONAS"
	AssetTypeStock  AssetType = "STOCK"
	AssetTypeOption AssetType = "OPTION"
)

// MonetaryAsset — novčana valuta.
type MonetaryAsset struct {
	Currency string `json:"currency"`
}

// StockDescription — akcija identifikovana po tickeru.
type StockDescription struct {
	Ticker string `json:"ticker"`
}

// MonetaryValue — novčani iznos s valutom.
type MonetaryValue struct {
	Currency string          `json:"currency"`
	Amount   decimal.Decimal `json:"amount"`
}

// OptionDescription — opcioni ugovor.
type OptionDescription struct {
	NegotiationID  ForeignBankId    `json:"negotiationId"`
	Stock          StockDescription `json:"stock"`
	PricePerUnit   MonetaryValue    `json:"pricePerUnit"`
	SettlementDate string           `json:"settlementDate"` // ISO8601
	Amount         int32            `json:"amount"`
}

// Asset — sum-type sa diskriminatorom "type".
//
//	{ "type": "MONAS",  "asset": { "currency": "RSD" } }
//	{ "type": "STOCK",  "asset": { "ticker": "AAPL" } }
//	{ "type": "OPTION", "asset": { ...OptionDescription } }
type Asset struct {
	Type   AssetType          `json:"type"`
	MonAs  *MonetaryAsset     `json:"-"`
	Stock  *StockDescription  `json:"-"`
	Option *OptionDescription `json:"-"`
}

func (a Asset) MarshalJSON() ([]byte, error) {
	type wrap struct {
		Type  AssetType   `json:"type"`
		Asset interface{} `json:"asset"`
	}
	switch a.Type {
	case AssetTypeMonas:
		return json.Marshal(wrap{Type: a.Type, Asset: a.MonAs})
	case AssetTypeStock:
		return json.Marshal(wrap{Type: a.Type, Asset: a.Stock})
	case AssetTypeOption:
		return json.Marshal(wrap{Type: a.Type, Asset: a.Option})
	}
	return nil, errors.New("interbank: nepoznat asset type")
}

func (a *Asset) UnmarshalJSON(data []byte) error {
	var head struct {
		Type  AssetType       `json:"type"`
		Asset json.RawMessage `json:"asset"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return err
	}
	a.Type = head.Type
	switch head.Type {
	case AssetTypeMonas:
		var v MonetaryAsset
		if err := json.Unmarshal(head.Asset, &v); err != nil {
			return err
		}
		a.MonAs = &v
	case AssetTypeStock:
		var v StockDescription
		if err := json.Unmarshal(head.Asset, &v); err != nil {
			return err
		}
		a.Stock = &v
	case AssetTypeOption:
		var v OptionDescription
		if err := json.Unmarshal(head.Asset, &v); err != nil {
			return err
		}
		a.Option = &v
	default:
		return errors.New("interbank: nepoznat asset type")
	}
	return nil
}

// AccountKind — diskriminator za TxAccount.
type AccountKind string

const (
	AccountKindPerson  AccountKind = "PERSON"
	AccountKindAccount AccountKind = "ACCOUNT"
	AccountKindOption  AccountKind = "OPTION"
)

// TxAccount — entitet u knjiženju.
//
//	PERSON  → ForeignBankId
//	ACCOUNT → broj računa (string)
//	OPTION  → ForeignBankId pseudo-account opcije
type TxAccount struct {
	Type AccountKind    `json:"type"`
	ID   *ForeignBankId `json:"id,omitempty"`  // PERSON / OPTION
	Num  *string        `json:"num,omitempty"` // ACCOUNT
}

// Posting — jedan red u transakciji (knjiženje).
type Posting struct {
	Account TxAccount       `json:"account"`
	Amount  decimal.Decimal `json:"amount"`
	Asset   Asset           `json:"asset"`
}

// Transaction — pun NEW_TX payload.
type Transaction struct {
	Postings       []Posting     `json:"postings"`
	TransactionID  ForeignBankId `json:"transactionId"`
	Message        string        `json:"message"`
	CallNumber     *string       `json:"callNumber,omitempty"`
	PaymentCode    string        `json:"paymentCode"`
	PaymentPurpose string        `json:"paymentPurpose"`
}

// CommitTransaction / RollbackTransaction — payload za faze 2/2-rollback.
type CommitTransaction struct {
	TransactionID ForeignBankId `json:"transactionId"`
}

type RollbackTransaction struct {
	TransactionID ForeignBankId `json:"transactionId"`
}

// ─── Message envelope ────────────────────────────────────────────────────────

type MessageType string

const (
	MessageNewTx      MessageType = "NEW_TX"
	MessageCommitTx   MessageType = "COMMIT_TX"
	MessageRollbackTx MessageType = "ROLLBACK_TX"
)

// InterbankMessage — generička poruka u POST /interbank.
// Telo "message" je RawMessage da bismo mogli da odlučimo u handleru kako
// da je dekodiramo na osnovu polja messageType.
type InterbankMessage struct {
	IdempotenceKey IdempotenceKey  `json:"idempotenceKey"`
	MessageType    MessageType     `json:"messageType"`
	Message        json.RawMessage `json:"message"`
}

// ─── Vote response ───────────────────────────────────────────────────────────

type VoteAnswer string

const (
	VoteYes VoteAnswer = "YES"
	VoteNo  VoteAnswer = "NO"
)

type NoVoteReasonCode string

const (
	NoReasonUnbalancedTx               NoVoteReasonCode = "UNBALANCED_TX"
	NoReasonNoSuchAccount              NoVoteReasonCode = "NO_SUCH_ACCOUNT"
	NoReasonNoSuchAsset                NoVoteReasonCode = "NO_SUCH_ASSET"
	NoReasonUnacceptableAsset          NoVoteReasonCode = "UNACCEPTABLE_ASSET"
	NoReasonInsufficientAsset          NoVoteReasonCode = "INSUFFICIENT_ASSET"
	NoReasonOptionAmountIncorrect      NoVoteReasonCode = "OPTION_AMOUNT_INCORRECT"
	NoReasonOptionUsedOrExpired        NoVoteReasonCode = "OPTION_USED_OR_EXPIRED"
	NoReasonOptionNegotiationNotFound  NoVoteReasonCode = "OPTION_NEGOTIATION_NOT_FOUND"
)

// NoVoteReason — razlog NO glasa, sa povezanim posting-om kada je primenljivo.
type NoVoteReason struct {
	Reason  NoVoteReasonCode `json:"reason"`
	Posting *Posting         `json:"posting,omitempty"`
}

// TransactionVote — odgovor na NEW_TX.
type TransactionVote struct {
	Vote    VoteAnswer     `json:"vote"`
	Reasons []NoVoteReason `json:"reasons,omitempty"`
}

// ─── OTC: public stock & negotiations ────────────────────────────────────────

// PublicStockSeller — jedan prodavac (i njegova količina) za dati stock.
type PublicStockSeller struct {
	Seller ForeignBankId `json:"seller"`
	Amount int32         `json:"amount"`
}

// PublicStock — agregirani entitet za GET /public-stock.
type PublicStock struct {
	Stock   StockDescription    `json:"stock"`
	Sellers []PublicStockSeller `json:"sellers"`
}

// OtcOffer — predlog ugovora (počinje ga kupac, pregovaranje je naizmenično).
type OtcOffer struct {
	Stock          StockDescription `json:"stock"`
	SettlementDate string           `json:"settlementDate"`
	PricePerUnit   MonetaryValue    `json:"pricePerUnit"`
	Premium        MonetaryValue    `json:"premium"`
	BuyerID        ForeignBankId    `json:"buyerId"`
	SellerID       ForeignBankId    `json:"sellerId"`
	Amount         int32            `json:"amount"`
	LastModifiedBy ForeignBankId    `json:"lastModifiedBy"`
}

// OtcNegotiation — OtcOffer + isOngoing flag (vraća se na GET).
type OtcNegotiation struct {
	OtcOffer
	IsOngoing bool `json:"isOngoing"`
}

// UserInfoResponse — odgovor na GET /user/{routing}/{id}.
type UserInfoResponse struct {
	BankDisplayName string `json:"bankDisplayName"`
	DisplayName     string `json:"displayName"`
}

// ─── Persistencija (entiteti baze) ───────────────────────────────────────────

type InterbankMessageDirection string

const (
	DirectionIncoming InterbankMessageDirection = "INCOMING"
	DirectionOutgoing InterbankMessageDirection = "OUTGOING"
)

type InterbankMessageStatus string

const (
	MsgStatusPending   InterbankMessageStatus = "PENDING"
	MsgStatusProcessed InterbankMessageStatus = "PROCESSED"
	MsgStatusSent      InterbankMessageStatus = "SENT"
	MsgStatusFailed    InterbankMessageStatus = "FAILED"
	MsgStatusAccepted  InterbankMessageStatus = "ACCEPTED" // 202 received
	MsgStatusNoVote    InterbankMessageStatus = "NO_VOTE"  // remote bank vraćio NO
)

// InterbankMessageLog — entitet za sve incoming/outgoing poruke.
type InterbankMessageLog struct {
	ID                       int64
	Direction                InterbankMessageDirection
	MessageType              MessageType
	IdempotenceRoutingNumber int64
	IdempotenceLocalKey      string
	TargetRoutingNumber      *int64
	Payload                  string
	ResponsePayload          *string
	ResponseStatusCode       *int
	Status                   InterbankMessageStatus
	RetryCount               int
	NextRetryAt              *time.Time
	LastError                string
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

type InterbankTxRole string

const (
	TxRoleCoordinator InterbankTxRole = "COORDINATOR"
	TxRoleParticipant InterbankTxRole = "PARTICIPANT"
)

type InterbankTxStatus string

const (
	TxStatusNew         InterbankTxStatus = "NEW"
	TxStatusPrepared    InterbankTxStatus = "PREPARED"
	TxStatusCommitted   InterbankTxStatus = "COMMITTED"
	TxStatusRolledBack  InterbankTxStatus = "ROLLED_BACK"
	TxStatusFailed      InterbankTxStatus = "FAILED"
)

// InterbankTransaction — saga state za jednu međubankarsku transakciju.
type InterbankTransaction struct {
	ID                       int64
	TransactionRoutingNumber int64
	TransactionForeignID     string
	Role                     InterbankTxRole
	Status                   InterbankTxStatus
	CurrentStep              string
	Payload                  string
	FailureReason            string
	InitiatorUserID          *int64
	InitiatorAccountID       *int64
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

// InterbankReservation — čuva detalje rezervacija prikačene za prepared tx.
type InterbankReservation struct {
	ID                       int64
	InterbankTransactionID   int64
	PostingIndex             int
	AccountKind              AccountKind
	AccountNum               *string
	ForeignRoutingNumber     *int64
	ForeignID                *string
	AssetType                AssetType
	AssetCurrency            *string
	AssetTicker              *string
	AssetNegotiationRouting  *int64
	AssetNegotiationLocalID  *string
	Amount                   decimal.Decimal
	Reserved                 bool
	CreatedAt                time.Time
}

// InterbankNegotiation — entitet OTC pregovaranja na strani prodavca.
type InterbankNegotiation struct {
	ID                          int64
	NegotiationRoutingNumber    int64
	NegotiationForeignID        string
	StockTicker                 string
	SettlementDate              time.Time
	PriceCurrency               string
	PriceAmount                 decimal.Decimal
	PremiumCurrency             string
	PremiumAmount               decimal.Decimal
	Amount                      int32
	BuyerRoutingNumber          int64
	BuyerID                     string
	SellerRoutingNumber         int64
	SellerID                    string
	LastModifiedRoutingNumber   int64
	LastModifiedID              string
	IsOngoing                   bool
	Status                      string
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
}

// InterbankOptionContract — opcioni ugovor po prihvaćenoj OTC ponudi.
type InterbankOptionContract struct {
	ID                       int64
	NegotiationRoutingNumber int64
	NegotiationForeignID     string
	StockTicker              string
	PriceCurrency            string
	PriceAmount              decimal.Decimal
	PremiumCurrency          string
	PremiumAmount            decimal.Decimal
	SettlementDate           time.Time
	Amount                   int32
	BuyerRoutingNumber       int64
	BuyerID                  string
	SellerRoutingNumber      int64
	SellerID                 string
	Status                   string
	UsedAt                   *time.Time
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

// ─── Repository interfejsi ───────────────────────────────────────────────────

type InterbankRepository interface {
	// Message log
	GetIncomingByIdempotence(ctx context.Context, routingNumber int64, localKey string) (*InterbankMessageLog, error)
	GetOutgoingByIdempotence(ctx context.Context, routingNumber int64, localKey string) (*InterbankMessageLog, error)
	CreateMessage(ctx context.Context, m *InterbankMessageLog) error
	UpdateMessage(ctx context.Context, m *InterbankMessageLog) error
	ListPendingOutgoing(ctx context.Context, limit int) ([]InterbankMessageLog, error)

	// Interbank transaction
	CreateTransaction(ctx context.Context, t *InterbankTransaction) error
	GetTransactionByForeignID(ctx context.Context, routingNumber int64, foreignID string) (*InterbankTransaction, error)
	UpdateTransactionStatus(ctx context.Context, id int64, status InterbankTxStatus, step, failureReason string) error

	// Reservations
	CreateReservation(ctx context.Context, r *InterbankReservation) error
	ListReservationsByTx(ctx context.Context, txID int64) ([]InterbankReservation, error)

	// Negotiations
	CreateNegotiation(ctx context.Context, n *InterbankNegotiation) error
	GetNegotiationByID(ctx context.Context, routingNumber int64, foreignID string) (*InterbankNegotiation, error)
	UpdateNegotiation(ctx context.Context, n *InterbankNegotiation) error
	ListPublicStocks(ctx context.Context) ([]PublicStock, error)

	// Option contracts (interbank)
	CreateOptionContract(ctx context.Context, c *InterbankOptionContract) error
	GetOptionContract(ctx context.Context, routingNumber int64, foreignID string) (*InterbankOptionContract, error)
	UpdateOptionContractStatus(ctx context.Context, routingNumber int64, foreignID, status string, usedAt *time.Time) error
	ListContractsForUser(ctx context.Context, routingNumber int64, userID string) ([]InterbankOptionContract, error)
}

// ─── Service interfejsi ──────────────────────────────────────────────────────

// InterbankClient — HTTP klijent ka drugoj banci.
type InterbankClient interface {
	// SendMessage šalje POST /interbank ka konfiguriranoj drugoj banci.
	// Vraća (statusCode, responseBody, error).
	SendMessage(ctx context.Context, msg InterbankMessage) (statusCode int, body []byte, err error)

	// GetPublicStock pribavlja akcije u javnom režimu druge banke.
	GetPublicStock(ctx context.Context) ([]PublicStock, error)

	// CreateNegotiation šalje POST /negotiations ka banci prodavca.
	CreateNegotiation(ctx context.Context, offer OtcOffer) (*ForeignBankId, error)

	// CounterNegotiation šalje PUT /negotiations/{routing}/{id}.
	CounterNegotiation(ctx context.Context, id ForeignBankId, offer OtcOffer) error

	// GetNegotiation šalje GET /negotiations/{routing}/{id}.
	GetNegotiation(ctx context.Context, id ForeignBankId) (*OtcNegotiation, error)

	// CancelNegotiation šalje DELETE /negotiations/{routing}/{id}.
	CancelNegotiation(ctx context.Context, id ForeignBankId) error

	// AcceptNegotiation šalje GET /negotiations/{routing}/{id}/accept.
	AcceptNegotiation(ctx context.Context, id ForeignBankId) error
}

// LocalTransactionExecutor — validira i izvršava lokalni deo Transaction objekta.
type LocalTransactionExecutor interface {
	// Prepare validira i rezerviše lokalne resurse za sve postings koji se tiču
	// ove banke. Vraća YES vote ako je sve ok, ili NO + reasons.
	Prepare(ctx context.Context, ibTxID int64, tx Transaction) (TransactionVote, error)

	// Commit primenjuje lokalne efekte (skida rezervaciju, knjiži novi balans)
	// i upisuje knjiženja u core_banking.transakcija.
	Commit(ctx context.Context, ibTxID int64) error

	// Rollback oslobađa rezervacije i postavlja status transakcije na ROLLED_BACK.
	Rollback(ctx context.Context, ibTxID int64) error
}

// TransactionCoordinatorService — orkestrira 2-phase commit između banaka.
type TransactionCoordinatorService interface {
	// InitiateInterbankPayment — kreira Transaction objekat za međubankarsko
	// plaćanje, lokalno priprema, šalje NEW_TX, prikuplja vote, COMMIT/ROLLBACK.
	InitiateInterbankPayment(ctx context.Context, in InterbankPaymentInput) (*InterbankTransaction, error)

	// HandleIncomingMessage — entry point za POST /interbank handler.
	HandleIncomingMessage(ctx context.Context, msg InterbankMessage) (statusCode int, response interface{}, err error)
}

// InterbankPaymentInput — ulazni parametri za međubankarsko plaćanje.
type InterbankPaymentInput struct {
	SenderAccountID    int64
	SenderUserID       int64
	RecipientAccountNo string
	RecipientName      string
	Amount             decimal.Decimal
	Currency           string
	PaymentCode        string
	PaymentPurpose     string
	CallNumber         string
	Message            string
}

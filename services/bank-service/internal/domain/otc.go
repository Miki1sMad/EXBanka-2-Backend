package domain

// otc.go — domen za Fazu 2 (OTC pregovaranje akcijama klijent-klijent).
// Inter-bank trgovina nije u scope-u ove iteracije; sve operacije se vrše
// unutar iste banke, ali shema ostavlja mesta za seller_bank_id/buyer_bank_id
// proširenje kasnije.

import (
	"context"
	"errors"
	"time"
)

// ─── Status enumi (verbatim po specifikaciji) ────────────────────────────────

type OTCOfferStatus string

const (
	OTCOfferPending     OTCOfferStatus = "PENDING"     // u pregovoru
	OTCOfferAccepted    OTCOfferStatus = "ACCEPTED"    // prihvaćeno → ugovor kreiran
	OTCOfferRejected    OTCOfferStatus = "REJECTED"    // odbijeno od druge strane
	OTCOfferDeactivated OTCOfferStatus = "DEACTIVATED" // povučeno od strane koja je poslala / isteklo
)

type OTCContractStatus string

const (
	OTCContractValid     OTCContractStatus = "VALID"
	OTCContractExpired   OTCContractStatus = "EXPIRED"
	OTCContractExercised OTCContractStatus = "EXERCISED"
)

// ─── SAGA tipovi ─────────────────────────────────────────────────────────────

// OTCSagaStep je korak SAGA orkestratora za izvršavanje OTC ugovora.
// Vrednost predstavlja POSLEDNJI USPEŠNO ZAVRŠEN korak (ne tekući).
type OTCSagaStep string

const (
	OTCSagaStepPending           OTCSagaStep = "PENDING"            // još nije počelo
	OTCSagaStepReserveFunds      OTCSagaStep = "RESERVE_FUNDS"      // sredstva kupca rezervisana
	OTCSagaStepReserveSecurities OTCSagaStep = "RESERVE_SECURITIES" // akcije prodavca sklonjene iz public_shares
	OTCSagaStepTransferFunds     OTCSagaStep = "TRANSFER_FUNDS"     // sredstva prebačena kupac → prodavac
	OTCSagaStepTransferOwnership OTCSagaStep = "TRANSFER_OWNERSHIP" // vlasništvo preneseno na kupca
	OTCSagaStepCompleted         OTCSagaStep = "COMPLETED"          // sve faze uspešne
)

// OTCSagaStatus je ukupan status SAGA egzekucije.
type OTCSagaStatus string

const (
	OTCSagaStatusInProgress         OTCSagaStatus = "IN_PROGRESS"
	OTCSagaStatusCompleted          OTCSagaStatus = "COMPLETED"
	OTCSagaStatusFailed             OTCSagaStatus = "FAILED"
	OTCSagaStatusCompensating       OTCSagaStatus = "COMPENSATING"
	OTCSagaStatusCompensationFailed OTCSagaStatus = "COMPENSATION_FAILED"
)

// OTCSagaExecution prati stanje jedne SAGA egzekucije u bazi.
type OTCSagaExecution struct {
	ID                  int64
	ContractID          int64
	CurrentStep         OTCSagaStep
	Status              OTCSagaStatus
	BuyerReservedAmount float64 // iznos rezervisan na buyerAccount (za rollback korak 1)
	ErrorMessage        string
	RetryCount          int
	InitiatedBy         int64
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// OTCSagaStepStatus je rezultat jednog koraka.
type OTCSagaStepStatus string

const (
	OTCSagaStepStatusCompleted          OTCSagaStepStatus = "COMPLETED"
	OTCSagaStepStatusFailed             OTCSagaStepStatus = "FAILED"
	OTCSagaStepStatusCompensated        OTCSagaStepStatus = "COMPENSATED"
	OTCSagaStepStatusCompensationFailed OTCSagaStepStatus = "COMPENSATION_FAILED"
)

// OTCSagaStepLogEntry je jedan red u otc_saga_step_log.
type OTCSagaStepLogEntry struct {
	ID          int64
	ExecutionID int64
	Step        OTCSagaStep
	StepStatus  OTCSagaStepStatus
	ErrorMsg    string
	Attempt     int
	CreatedAt   time.Time
}

// ─── Contract DTO-ovi za listanje ─────────────────────────────────────────────

// OTCContractExpiringSoon je ugovor koji uskoro ističe (za notifikacije).
type OTCContractExpiringSoon struct {
	OTCContract
	Ticker string
}

// OTCOfferHistoryEntry is one recorded step in a negotiation.
type OTCOfferHistoryEntry struct {
	ID               int64
	OfferID          int64
	Action           string // CREATED | COUNTER | ACCEPTED | DECLINED
	ChangedBy        int64
	Amount           *int32
	PricePerStock    *float64
	Premium          *float64
	SettlementDate   *time.Time
	OldAmount        *int32
	OldPricePerStock *float64
	OldPremium       *float64
	OldSettlementDate *time.Time
	NewStatus        *string
	CreatedAt        time.Time
}

// NegotiationHistoryItem is a completed offer summary with its full step history.
type NegotiationHistoryItem struct {
	OTCOfferListItem
	History []OTCOfferHistoryEntry
}

// ListCompletedOffersFilter filters completed (non-PENDING) offers.
type ListCompletedOffersFilter struct {
	UserID        int64
	Status        *OTCOfferStatus
	From          *time.Time
	To            *time.Time
	CounterpartID *int64
}

// OTCContractListItem je projekcija ugovora za GET /api/otc/contracts.
// Profit je izvedena vrednost: (TrenutnaTrazisCena - StrikePrice) * Amount - Premium.
type OTCContractListItem struct {
	OTCContract
	Ticker         string
	StockName      string
	Exchange       string
	CurrentPrice   float64 // tržišna cena akcije u trenutku listanja
	Profit         float64 // (CurrentPrice - StrikePrice) * Amount - Premium
	SellerName     string  // "Ime Prezime" prodavca (iz user-service)
	SellerBankName string  // naziv banke prodavca (npr. "Banka 1")
	SellerInfo     string  // formatirano "Ime Prezime, Naziv Banke" za UI
}

// ExerciseOTCContractInput ulazni parametri za izvršavanje OTC ugovora.
type ExerciseOTCContractInput struct {
	ContractID int64
	CallerID   int64 // mora biti BuyerID ugovora
}

// ─── Greške ───────────────────────────────────────────────────────────────────

var (
	ErrOTCOfferNotFound           = errors.New("OTC ponuda nije pronađena")
	ErrOTCContractNotFound        = errors.New("OTC ugovor nije pronađen")
	ErrOTCNotInPublicRegime       = errors.New("akcija nije postavljena u javni režim")
	ErrOTCInsufficientCapacity    = errors.New("prodavac nema dovoljno akcija u javnom režimu (suma aktivnih ponuda i ugovora bi prešla raspoloživost)")
	ErrOTCNotCounterparty         = errors.New("odgovor na ponudu može poslati samo druga strana (nije vaš red)")
	ErrOTCInvalidStatus           = errors.New("operacija nije dozvoljena u trenutnom statusu ponude")
	ErrOTCSelfTrade               = errors.New("kupac i prodavac moraju biti različite osobe")
	ErrOTCSelfAccept              = errors.New("ne možete prihvatiti ponudu koju ste sami poslednji izmenili")
	ErrOTCNotParticipant          = errors.New("niste učesnik u ovoj ponudi")
	ErrOTCAccountNotOwned         = errors.New("račun ne pripada korisniku")
	ErrOTCAccountCurrency         = errors.New("valuta računa ne odgovara valuti hartije")
	ErrOTCInsufficientFunds       = errors.New("nedovoljno raspoloživih sredstava na računu kupca za isplatu premije")
	ErrOTCSellerAccountMissing    = errors.New("prodavac još nije postavio svoj račun za premiju")
	ErrOTCInvalidInput            = errors.New("neispravan unos (količina, cena, premija ili datum)")
	ErrOTCListingNotFound         = errors.New("hartija od vrednosti nije pronađena")
	ErrOTCContractNotBuyer        = errors.New("samo kupac može izvršiti OTC ugovor")
	ErrOTCContractExpired         = errors.New("OTC ugovor je istekao (settlementDate je prošao)")
	ErrOTCContractAlreadyExecuted = errors.New("OTC ugovor je već izvršen")
	ErrOTCSagaAlreadyRunning      = errors.New("izvršavanje ovog ugovora je već u toku — pokušajte ponovo za trenutak")
)

// ─── Entiteti ─────────────────────────────────────────────────────────────────

// OTCOffer je entitet ponude u pregovoru. Polja koja se menjaju po kontraponudi
// (Amount, PricePerStock, Premium, SettlementDate, LastModified, ModifiedBy)
// uskladena su sa specifikacijom (Faza 2, tabela "Entitet ponude").
type OTCOffer struct {
	ID              int64
	ListingID       int64
	SellerID        int64
	BuyerID         int64
	BuyerAccountID  int64  // kupac eksplicitno bira pri POST-u
	SellerAccountID *int64 // prodavac postavlja pri prvom counter/accept (nullable do tada)
	Amount          int32
	PricePerStock   float64
	Premium         float64
	SettlementDate  time.Time
	Status          OTCOfferStatus
	LastModified    time.Time
	ModifiedBy      int64
	CreatedAt       time.Time
	// Forward-compat: dok se ne uvede inter-bank trgovina, oba polja su NULL
	// (ili jednaka ID-u naše banke). Šema je spremna da razlikuje banke učesnice.
	SellerBankID *int64
	BuyerBankID  *int64
}

// OTCContract — opcioni ugovor koji nastaje pri AcceptOffer.
type OTCContract struct {
	ID              int64
	OfferID         int64
	ListingID       int64
	SellerID        int64
	BuyerID         int64
	BuyerAccountID  int64
	SellerAccountID int64
	Amount          int32
	StrikePrice     float64 // == ponuda.PricePerStock u trenutku prihvatanja
	Premium         float64
	SettlementDate  time.Time
	Status          OTCContractStatus
	CreatedAt       time.Time
	ExercisedAt     *time.Time
	SellerBankID    *int64
	BuyerBankID     *int64
}

// ─── DTO za listanje ──────────────────────────────────────────────────────────

// OTCMarketplaceItem — agregirana stavka za /api/otc/marketplace
// (akcije koje su drugi klijenti stavili u "javni režim", umanjeno za
// količinu već vezanu u aktivnim PENDING ponudama i VALID ugovorima).
type OTCMarketplaceItem struct {
	ListingID         int64
	Ticker            string
	StockName         string
	Exchange          string
	MarketPriceUSD    float64
	SellerID          int64
	SellerName        string
	AvailableQuantity int32
}

// OTCOfferListItem — projekcija za GET /otc/offers (sa derivacijama za FE).
type OTCOfferListItem struct {
	OTCOffer
	Ticker            string  // listing.ticker
	StockName         string  // listing.name
	Exchange          string  // listing.exchange (oznaka)
	MarketPriceUSD    float64 // listing.price (referenca za color indikator)
	NeedsReview       bool    // true ako modified_by != caller (isti kao "nepročitano")
	PriceDeviationPct float64 // (price_per_stock - market_price) / market_price * 100
	PriceColor        string  // "GREEN" | "YELLOW" | "RED" — pomoć FE-u
}

// ─── Ulazni DTO-ovi za servis ─────────────────────────────────────────────────

type CreateOTCOfferInput struct {
	ListingID      int64
	BuyerID        int64
	SellerID       int64
	BuyerAccountID int64
	Amount         int32
	PricePerStock  float64
	Premium        float64
	SettlementDate time.Time
	// Inter-bank: postavljaju se na ownBankID ako nije inter-bank ponuda.
	BuyerBankID  *int64
	SellerBankID *int64
}

type CounterOTCOfferInput struct {
	OfferID         int64
	CallerID        int64
	Amount          int32
	PricePerStock   float64
	Premium         float64
	SettlementDate  time.Time
	SellerAccountID *int64 // popunjava prodavac kada šalje counter (kupac ne menja)
}

type AcceptOTCOfferInput struct {
	OfferID         int64
	CallerID        int64
	SellerAccountID *int64 // ako prodavac prihvata i nije ranije postavio
}

type ListOTCOffersFilter struct {
	UserID     int64           // ulogovani korisnik
	Status     *OTCOfferStatus // opciono
	Role       string          // "BUYER" | "SELLER" | "" (svi učesnici)
	OnlyMyTurn bool            // ako true, samo one gde modified_by != caller
	// BankFilter filtrira ponude po tipu banke učesnice.
	// "" | "ALL" = sve, "OWN" = intra-bank, "INTERBANK" = cross-bank ponude.
	BankFilter string
	OwnBankID  int64 // ID naše banke — potreban za filtriranje
}

// ─── Repository i Service interfejsi ─────────────────────────────────────────

type OTCRepository interface {
	CreateOffer(ctx context.Context, offer OTCOffer) (*OTCOffer, error)

	GetOfferByID(ctx context.Context, id int64) (*OTCOffer, error)

	// GetOfferByIDForUpdate čita ponudu sa SELECT ... FOR UPDATE — mora se zvati
	// unutar transakcije pre svake modifikacije (counter/accept/decline).
	GetOfferByIDForUpdate(ctx context.Context, id int64) (*OTCOffer, error)

	UpdateOfferOnCounter(ctx context.Context, offer OTCOffer) error

	UpdateOfferStatus(ctx context.Context, id int64, status OTCOfferStatus, modifiedBy int64) error

	// AvailablePublicShares vraća (raspoloživo za novu ponudu) za (seller, listing).
	// public_shares.quantity − Σ PENDING offers (sem excludeOfferID) − Σ VALID contracts.
	AvailablePublicShares(ctx context.Context, sellerID, listingID, excludeOfferID int64) (int32, error)

	ListOffers(ctx context.Context, filter ListOTCOffersFilter) ([]OTCOfferListItem, error)
	GetOfferListItem(ctx context.Context, id int64, callerID int64) (*OTCOfferListItem, error)

	// ListMarketplace — klijentske akcije u javnom režimu (drugi klijenti ≠ callerID).
	ListMarketplace(ctx context.Context, callerID int64) ([]OTCMarketplaceItem, error)

	CreateContract(ctx context.Context, contract OTCContract) (*OTCContract, error)

	// GetAccountInfo vraća (vlasnikID, valutaOznaka) za dati račun.
	// Servis koristi za validaciju vlasništva računa nad kojim se izvršava OTC operacija.
	GetAccountInfo(ctx context.Context, accountID int64) (ownerID int64, currency string, err error)

	// GetListingCurrency vraća valutu (oznaka, npr "USD") u kojoj se trguje data hartija.
	GetListingCurrency(ctx context.Context, listingID int64) (string, error)

	// ListContracts vraća sve ugovore u kojima učestvuje korisnik (kao buyer ili seller).
	ListContracts(ctx context.Context, userID int64) ([]OTCContract, error)

	// GetContractByID vraća ugovor po ID-u.
	GetContractByID(ctx context.Context, id int64) (*OTCContract, error)

	// GetContractByIDForUpdate čita ugovor sa SELECT FOR UPDATE (mora biti unutar tx).
	GetContractByIDForUpdate(ctx context.Context, id int64) (*OTCContract, error)

	// UpdateContractStatus postavlja novi status ugovora i exercised_at (ako je EXERCISED).
	UpdateContractStatus(ctx context.Context, id int64, status OTCContractStatus) error

	// ExpireOverdueContracts bulk-ažurira sve VALID ugovore čiji je settlement_date
	// prošao na status EXPIRED. Vraća broj ažuriranih redova.
	ExpireOverdueContracts(ctx context.Context) (int, error)

	// ListContractsExpiringSoon vraća VALID ugovore čiji je settlement_date
	// tačno withinDays kalendarskih dana od danas. Koristi se za upozorenja.
	ListContractsExpiringSoon(ctx context.Context, withinDays int) ([]OTCContractExpiringSoon, error)

	// RecordOfferHistory inserts one history entry. Must be called within the same transaction.
	RecordOfferHistory(ctx context.Context, entry OTCOfferHistoryEntry) error

	// ListCompletedNegotiations returns finished offers (status != PENDING) for a user with embedded history.
	ListCompletedNegotiations(ctx context.Context, filter ListCompletedOffersFilter) ([]NegotiationHistoryItem, error)

	// WithTx vraća instancu repoa koja radi nad datom *gorm.DB transakcijom.
	WithTx(tx interface{}) OTCRepository
}

// OTCSagaRepository upravlja perzistencijom SAGA stanja.
type OTCSagaRepository interface {
	// CreateExecution kreira novi saga red u IN_PROGRESS, korak PENDING.
	CreateExecution(ctx context.Context, contractID, initiatedBy int64, buyerReservedAmount float64) (*OTCSagaExecution, error)

	// GetExecution čita egzekuciju po ID-u.
	GetExecution(ctx context.Context, id int64) (*OTCSagaExecution, error)

	// GetExecutionByContractID čita egzekuciju za dati contract_id.
	GetExecutionByContractID(ctx context.Context, contractID int64) (*OTCSagaExecution, error)

	// UpdateStep ažurira current_step, status i error_message.
	UpdateStep(ctx context.Context, id int64, step OTCSagaStep, status OTCSagaStatus, errMsg string) error

	// IncrementRetry povećava retry_count za 1 i vraća novi retry_count.
	IncrementRetry(ctx context.Context, id int64) (int, error)

	// LogStep upisuje jedan red u otc_saga_step_log.
	LogStep(ctx context.Context, executionID int64, step OTCSagaStep, stepStatus OTCSagaStepStatus, errMsg string, attempt int) error

	// DeleteExecution briše SAGA egzekuciju po ID-u (koristi se za retry posle FAILED/COMPENSATION_FAILED).
	DeleteExecution(ctx context.Context, id int64) error

	// WithTx vraća instancu koja radi nad datom *gorm.DB transakcijom.
	WithTx(tx interface{}) OTCSagaRepository
}

// OTCPaymentPort — uži port koji OTCService koristi za isplatu premije kroz
// PaymentService (umesto direktnog UPDATE-a baze). Garantuje audit i FX podršku.
type OTCPaymentPort interface {
	// ExecuteOTCPremiumTransfer atomski (unutar prosleđene tx) skida iznos sa
	// kupčevog računa i upisuje ga na prodavčev. Ako se valute računa razlikuju
	// od valute hartije, vrši se konverzija po kursu banke + provizija.
	// `tx` mora biti *gorm.DB iz iste transakcije u kojoj se kreira OTC ugovor.
	ExecuteOTCPremiumTransfer(ctx context.Context, tx interface{}, in OTCPremiumTransferInput) error
}

type OTCPremiumTransferInput struct {
	OfferID                 int64
	BuyerAccountID          int64
	SellerAccountID         int64
	AmountInListingCurrency float64
	ListingCurrency         string // npr "USD"
	InitiatedByUserID       int64
}

type OTCService interface {
	CreateOffer(ctx context.Context, in CreateOTCOfferInput) (*OTCOffer, error)
	CounterOffer(ctx context.Context, in CounterOTCOfferInput) (*OTCOffer, error)
	AcceptOffer(ctx context.Context, in AcceptOTCOfferInput) (*OTCContract, error)
	DeclineOffer(ctx context.Context, offerID, callerID int64) (*OTCOffer, error)
	ListOffers(ctx context.Context, filter ListOTCOffersFilter) ([]OTCOfferListItem, error)
	GetOffer(ctx context.Context, offerID, callerID int64) (*OTCOfferListItem, error)
	ListMarketplace(ctx context.Context, callerID int64) ([]OTCMarketplaceItem, error)
	// ListCompletedNegotiations returns all finished negotiations for the caller with their step history.
	ListCompletedNegotiations(ctx context.Context, callerID int64, filter ListCompletedOffersFilter) ([]NegotiationHistoryItem, error)
}

// OTCContractService upravlja OTC ugovorima i njihovim SAGA izvršavanjem.
type OTCContractService interface {
	// ListContracts vraća sve ugovore korisnika sa deriviranim vrednostima (Profit, SellerInfo).
	ListContracts(ctx context.Context, userID int64) ([]OTCContractListItem, error)

	// GetContract vraća jedan ugovor sa deriviranim vrednostima.
	GetContract(ctx context.Context, contractID, callerID int64) (*OTCContractListItem, error)

	// ExerciseContract pokreće SAGA tok za izvršavanje OTC ugovora.
	// Samo kupac (BuyerID) može pozvati ovu metodu.
	// Vraća sagaExecution.ID za praćenje; greška ako se SAGA ne može pokrenuti.
	ExerciseContract(ctx context.Context, in ExerciseOTCContractInput) (*OTCSagaExecution, error)
}

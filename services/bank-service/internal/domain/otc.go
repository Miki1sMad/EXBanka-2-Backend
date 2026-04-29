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

// ─── Greške ───────────────────────────────────────────────────────────────────

var (
	ErrOTCOfferNotFound        = errors.New("OTC ponuda nije pronađena")
	ErrOTCNotInPublicRegime    = errors.New("akcija nije postavljena u javni režim")
	ErrOTCInsufficientCapacity = errors.New("prodavac nema dovoljno akcija u javnom režimu (suma aktivnih ponuda i ugovora bi prešla raspoloživost)")
	ErrOTCNotCounterparty      = errors.New("odgovor na ponudu može poslati samo druga strana (nije vaš red)")
	ErrOTCInvalidStatus        = errors.New("operacija nije dozvoljena u trenutnom statusu ponude")
	ErrOTCSelfTrade            = errors.New("kupac i prodavac moraju biti različite osobe")
	ErrOTCSelfAccept           = errors.New("ne možete prihvatiti ponudu koju ste sami poslednji izmenili")
	ErrOTCNotParticipant       = errors.New("niste učesnik u ovoj ponudi")
	ErrOTCAccountNotOwned      = errors.New("račun ne pripada korisniku")
	ErrOTCAccountCurrency      = errors.New("valuta računa ne odgovara valuti hartije")
	ErrOTCInsufficientFunds    = errors.New("nedovoljno raspoloživih sredstava na računu kupca za isplatu premije")
	ErrOTCSellerAccountMissing = errors.New("prodavac još nije postavio svoj račun za premiju")
	ErrOTCInvalidInput         = errors.New("neispravan unos (količina, cena, premija ili datum)")
	ErrOTCListingNotFound      = errors.New("hartija od vrednosti nije pronađena")
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

	// ListMarketplace — sve akcije koje su drugi (≠ callerID) stavili u javni
	// režim, agregirane po (seller, listing) sa preostalom raspoloživom količinom.
	ListMarketplace(ctx context.Context, callerID int64) ([]OTCMarketplaceItem, error)

	CreateContract(ctx context.Context, contract OTCContract) (*OTCContract, error)

	// GetAccountInfo vraća (vlasnikID, valutaOznaka) za dati račun.
	// Servis koristi za validaciju vlasništva računa nad kojim se izvršava OTC operacija.
	GetAccountInfo(ctx context.Context, accountID int64) (ownerID int64, currency string, err error)

	// GetListingCurrency vraća valutu (oznaka, npr "USD") u kojoj se trguje data hartija.
	GetListingCurrency(ctx context.Context, listingID int64) (string, error)

	// WithTx vraća instancu repoa koja radi nad datom *gorm.DB transakcijom.
	WithTx(tx interface{}) OTCRepository
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
}

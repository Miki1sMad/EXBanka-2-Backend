package domain

import (
	"context"
	"errors"
	"time"
)

// ─── Greške ───────────────────────────────────────────────────────────────────

var (
	ErrFundNotFound         = errors.New("fond nije pronađen")
	ErrFundNameExists       = errors.New("fond sa ovim nazivom već postoji")
	ErrNotSupervisorForFund = errors.New("samo supervizori mogu kreirati fondove")
)

// ─── Entiteti ─────────────────────────────────────────────────────────────────

// InvestmentFund je osnovna domenska entiteta investicionog fonda.
// VrednostFonda i Profit se ne čuvaju u bazi — računaju se po zahtevu u servisu.
type InvestmentFund struct {
	ID                  int64
	Name                string
	Description         string
	MinimumContribution float64   // MinimalniUlog u RSD
	ManagerID           int64     // MenadžerId — employee_id supervizora iz user-service
	LiquidAssets        float64   // LikvidnaSredstva — stanje na RSD računu fonda
	AccountID           int64     // FK na core_banking.racun (dinarski račun fonda)
	CreatedAt           time.Time // DatumKreiranja — sistem postavlja pri kreiranju
}

// FundSecurity je hartija od vrednosti koje fond poseduje.
// Veza između fonda i listinga; quantity je agregatna količina.
type FundSecurity struct {
	ID              int64
	FundID          int64
	ListingID       int64
	Quantity        float64   // agregirana količina u posedu fonda
	AcquisitionDate time.Time // datum prve/poslednje nabavke
	InitialCostRSD  float64   // nabavna cena u RSD (za interne analize)
}

// ClientFundPosition je ukupna pozicija klijenta u fondu (agregat).
// ProcenatFonda i TrenutnaVrednostPozicije su izvedeni — računaju se u servisu.
type ClientFundPosition struct {
	ID               int64
	FundID           int64
	UserID           int64
	TotalInvestedRSD float64   // UkupanUlozeniIznos u RSD
	LastChanged      time.Time // DatumPoslednjePromene
}

// ClientFundTransaction je jedna uplata ili isplata klijenta u/iz fonda.
type ClientFundTransaction struct {
	ID        int64
	FundID    int64
	UserID    int64
	AmountRSD float64
	Status    string // pending | completed | failed
	CreatedAt time.Time
	IsInflow  bool // true = uplata, false = isplata
}

// ─── DTO-ovi za ulaz/izlaz servisa ───────────────────────────────────────────

// CreateFundInput ulazni parametri za kreiranje novog fonda.
type CreateFundInput struct {
	Name                string
	Description         string
	MinimumContribution float64
	ManagerID           int64 // employee_id supervizora koji kreira fond
}

// FundFilter parametri za filtriranje i sortiranje liste fondova (Discovery).
type FundFilter struct {
	Search    string // pretraga po nazivu i opisu (case-insensitive)
	SortBy    string // name | description | fundValue | profit | minimumContribution
	SortOrder string // ASC | DESC
}

// FundListItem je projekcija fonda za Discovery stranicu.
// FundValueRSD i Profit su izvedeni podaci izračunati u servisu.
type FundListItem struct {
	InvestmentFund
	FundValueRSD float64 // LikvidnaSredstva + tržišna vrednost svih hartija u RSD
	Profit       float64 // FundValueRSD − zbir svih uloženih iznosa iz pozicija
}

// FundSecurityDetail je hartija sa tržišnim podacima za prikaz u detalju fonda.
type FundSecurityDetail struct {
	FundSecurity
	Ticker            string
	CurrentPriceUSD   float64
	CurrentPriceRSD   float64
	ChangePercent     float64 // dnevna promena cene u procentima
	Volume            int64
	InitialMarginCost float64 // za prikaz na detalju (spec: initialMarginCost)
}

// BankAccountItem je projekcija jednog RSD računa banke (vlasnik_id=2 / trezor).
type BankAccountItem struct {
	ID                  int64
	BrojRacuna          string
	NazivRacuna         string
	StanjeRacuna        float64
	RezervovanaSredstva float64
	RaspolozivoStanje   float64
}

// FundDetails je pun prikaz fonda za GET /bank/investment-funds/{id}.
type FundDetails struct {
	InvestmentFund
	FundValueRSD float64
	Profit       float64
	Securities   []FundSecurityDetail
}

// ─── Repository interfejs ─────────────────────────────────────────────────────

// InvestmentFundRepository definiše ugovor prema sloju podataka.
type InvestmentFundRepository interface {
	// Create persist novi fond i vraća persitovani objekat.
	Create(ctx context.Context, fund InvestmentFund) (*InvestmentFund, error)

	// GetByID vraća fond po ID-u; ErrFundNotFound ako ne postoji.
	GetByID(ctx context.Context, id int64) (*InvestmentFund, error)

	// GetAccountNumber vraća broj_racuna (npr. "666-0001-12-...") za dati surogat PK računa.
	GetAccountNumber(ctx context.Context, accountID int64) (string, error)

	// ListBankRSDAccounts vraća sve aktivne RSD račune koje banka (trezor, vlasnik_id=2) drži,
	// ISKLJUČUJUĆI račune koji su namenski povezani sa investicionim fondovima.
	// Koristi supervizor pri "Investiraj u ime banke" i pri kupovini hartija u ime banke.
	ListBankRSDAccounts(ctx context.Context) ([]BankAccountItem, error)

	// List vraća sve fondove (bez filtriranja — filtriranje je u servisnom sloju).
	List(ctx context.Context) ([]InvestmentFund, error)

	// TransferManagerFunds prebacuje sve fondove sa oldManagerID na newManagerID.
	// Koristi se kada admin ukloni isSupervisor permisiju.
	TransferManagerFunds(ctx context.Context, oldManagerID, newManagerID int64) error

	// GetSecurities vraća sve hartije koje fond poseduje.
	GetSecurities(ctx context.Context, fundID int64) ([]FundSecurity, error)

	// UpsertSecurity dodaje ili ažurira poziciju hartije u fondu (setuje quantity apsolutno).
	UpsertSecurity(ctx context.Context, sec FundSecurity) error

	// AddSecurityQuantity akumulira količinu i nabavnu cenu za hartiju fonda.
	// Ako zapis već postoji, quantity i initial_cost_rsd se povećavaju (ne zamenjuju).
	// Poziva se iz trading engine-a nakon svakog uspešnog BUY fill-a.
	AddSecurityQuantity(ctx context.Context, fundID, listingID int64, deltaQty float64, acquisitionDate time.Time, deltaCostRSD float64) error

	// DeductLiquidAssets smanjuje liquid_assets fonda za zadati iznos u RSD.
	// Poziva se atomično unutar fill transakcije zajedno sa SettleBuyFill.
	DeductLiquidAssets(ctx context.Context, fundID int64, amountRSD float64) error

	// GetPositions vraća sve klijentske pozicije u fondu.
	GetPositions(ctx context.Context, fundID int64) ([]ClientFundPosition, error)

	// GetTotalInvested vraća zbir svih uloženih iznosa u fond (suma invested_rsd svih pozicija).
	GetTotalInvested(ctx context.Context, fundID int64) (float64, error)

	// WithDB vraća novu instancu koja koristi dati *gorm.DB (može biti transakcija).
	// Argument mora biti *gorm.DB; neophodan za atomično izvršavanje fill operacija u engine.go.
	WithDB(db interface{}) InvestmentFundRepository
}

// ErrInsufficientFundLiquidity se vraća kada fond nema dovoljno liquid_assets za kupovinu.
var ErrInsufficientFundLiquidity = errors.New("fond nema dovoljno likvidnih sredstava za ovu kupovinu")

// ─── Service interfejs ────────────────────────────────────────────────────────

// InvestmentFundService definiše ugovor poslovne logike za investicione fondove.
type InvestmentFundService interface {
	// CreateFund kreira fond i automatski otvara dinarski račun u ime banke.
	// Samo supervizori mogu pozivati ovu metodu (provjera je u handler sloju).
	CreateFund(ctx context.Context, input CreateFundInput) (*InvestmentFund, error)

	// GetFundByID vraća fond po ID-u; ErrFundNotFound ako ne postoji.
	GetFundByID(ctx context.Context, id int64) (*InvestmentFund, error)

	// GetFundDetails vraća detaljan prikaz fonda sa hartijama i izvedenim vrednostima.
	GetFundDetails(ctx context.Context, id int64) (*FundDetails, error)

	// ListFunds vraća sve fondove sa izvedenim vrednostima, sa opcionalnim filtriranjem i sortiranjem.
	ListFunds(ctx context.Context, filter FundFilter) ([]FundListItem, error)

	// TransferManagerFunds prebacuje sve fondove sa oldManagerID na adminID.
	// Poziva InternalActuaryHandler kada admin ukloni isSupervisor permisiju.
	TransferManagerFunds(ctx context.Context, oldManagerID, adminID int64) error
}

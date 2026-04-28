package repository

import (
	"context"
	"errors"
	"time"

	"banka-backend/services/bank-service/internal/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ─── GORM modeli ──────────────────────────────────────────────────────────────

type investmentFundFullModel struct {
	ID                  int64     `gorm:"column:id;primaryKey"`
	Name                string    `gorm:"column:name"`
	Description         string    `gorm:"column:description"`
	MinimumContribution float64   `gorm:"column:minimum_contribution"`
	ManagerID           int64     `gorm:"column:manager_id"`
	LiquidAssets        float64   `gorm:"column:liquid_assets"`
	AccountID           *int64    `gorm:"column:account_id"`
	CreatedAt           time.Time `gorm:"column:created_at"`
}

func (investmentFundFullModel) TableName() string { return "core_banking.investment_funds" }

func (m investmentFundFullModel) toDomain() domain.InvestmentFund {
	var accountID int64
	if m.AccountID != nil {
		accountID = *m.AccountID
	}
	return domain.InvestmentFund{
		ID:                  m.ID,
		Name:                m.Name,
		Description:         m.Description,
		MinimumContribution: m.MinimumContribution,
		ManagerID:           m.ManagerID,
		LiquidAssets:        m.LiquidAssets,
		AccountID:           accountID,
		CreatedAt:           m.CreatedAt,
	}
}

type fundSecurityFullModel struct {
	ID              int64     `gorm:"column:id;primaryKey"`
	FundID          int64     `gorm:"column:fund_id"`
	ListingID       int64     `gorm:"column:listing_id"`
	Quantity        float64   `gorm:"column:quantity"`
	AcquisitionDate time.Time `gorm:"column:acquisition_date"`
	InitialCostRSD  float64   `gorm:"column:initial_cost_rsd"`
}

func (fundSecurityFullModel) TableName() string { return "core_banking.fund_securities" }

func (m fundSecurityFullModel) toDomain() domain.FundSecurity {
	return domain.FundSecurity{
		ID:              m.ID,
		FundID:          m.FundID,
		ListingID:       m.ListingID,
		Quantity:        m.Quantity,
		AcquisitionDate: m.AcquisitionDate,
		InitialCostRSD:  m.InitialCostRSD,
	}
}

// clientFundPositionFullModel mapira na fund_positions (kreirao fund_handler.go).
// Polje last_changed je dodato migracijom 000044.
type clientFundPositionFullModel struct {
	ID          int64     `gorm:"column:id;primaryKey"`
	FundID      int64     `gorm:"column:fund_id"`
	UserID      int64     `gorm:"column:user_id"`
	InvestedRSD float64   `gorm:"column:invested_rsd"`
	LastChanged time.Time `gorm:"column:last_changed"`
}

func (clientFundPositionFullModel) TableName() string { return "core_banking.fund_positions" }

// ─── investmentFundRepository ─────────────────────────────────────────────────

type investmentFundRepository struct {
	db *gorm.DB
}

// NewInvestmentFundRepository vraća implementaciju domain.InvestmentFundRepository.
func NewInvestmentFundRepository(db *gorm.DB) domain.InvestmentFundRepository {
	return &investmentFundRepository{db: db}
}

func (r *investmentFundRepository) Create(ctx context.Context, fund domain.InvestmentFund) (*domain.InvestmentFund, error) {
	var accountID *int64
	if fund.AccountID != 0 {
		id := fund.AccountID
		accountID = &id
	}
	m := investmentFundFullModel{
		Name:                fund.Name,
		Description:         fund.Description,
		MinimumContribution: fund.MinimumContribution,
		ManagerID:           fund.ManagerID,
		LiquidAssets:        0,
		AccountID:           accountID,
		CreatedAt:           time.Now().UTC(),
	}
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		return nil, err
	}
	result := m.toDomain()
	return &result, nil
}

// GetAccountNumber vraća broj_racuna za dati surogat PK računa.
// Koristi se za prikaz "pravog" broja računa fonda u response-u (umesto interne ID vrednosti).
func (r *investmentFundRepository) GetAccountNumber(ctx context.Context, accountID int64) (string, error) {
	var brojRacuna string
	err := r.db.WithContext(ctx).
		Raw(`SELECT broj_racuna FROM core_banking.racun WHERE id = ?`, accountID).
		Scan(&brojRacuna).Error
	if err != nil {
		return "", err
	}
	return brojRacuna, nil
}

// ListBankRSDAccounts vraća sve aktivne RSD račune banke (vlasnik_id=2),
// isključujući račune koji su povezani sa investicionim fondovima.
type bankAccountRow struct {
	ID                  int64   `gorm:"column:id"`
	BrojRacuna          string  `gorm:"column:broj_racuna"`
	NazivRacuna         string  `gorm:"column:naziv_racuna"`
	StanjeRacuna        float64 `gorm:"column:stanje_racuna"`
	RezervovanaSredstva float64 `gorm:"column:rezervisana_sredstva"`
}

func (r *investmentFundRepository) ListBankRSDAccounts(ctx context.Context) ([]domain.BankAccountItem, error) {
	var rows []bankAccountRow
	err := r.db.WithContext(ctx).Raw(`
		SELECT r.id, r.broj_racuna, r.naziv_racuna, r.stanje_racuna, r.rezervisana_sredstva
		FROM core_banking.racun r
		JOIN core_banking.valuta v ON v.id = r.id_valute
		WHERE r.id_vlasnika = 2
		  AND r.status = 'AKTIVAN'
		  AND v.oznaka = 'RSD'
		  AND r.id NOT IN (
		      SELECT account_id FROM core_banking.investment_funds WHERE account_id IS NOT NULL
		  )
		ORDER BY r.id ASC
	`).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	items := make([]domain.BankAccountItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, domain.BankAccountItem{
			ID:                  row.ID,
			BrojRacuna:          row.BrojRacuna,
			NazivRacuna:         row.NazivRacuna,
			StanjeRacuna:        row.StanjeRacuna,
			RezervovanaSredstva: row.RezervovanaSredstva,
			RaspolozivoStanje:   row.StanjeRacuna - row.RezervovanaSredstva,
		})
	}
	return items, nil
}

func (r *investmentFundRepository) GetByID(ctx context.Context, id int64) (*domain.InvestmentFund, error) {
	var m investmentFundFullModel
	if err := r.db.WithContext(ctx).First(&m, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrFundNotFound
		}
		return nil, err
	}
	result := m.toDomain()
	return &result, nil
}

func (r *investmentFundRepository) List(ctx context.Context) ([]domain.InvestmentFund, error) {
	var models []investmentFundFullModel
	if err := r.db.WithContext(ctx).Order("created_at DESC").Find(&models).Error; err != nil {
		return nil, err
	}
	funds := make([]domain.InvestmentFund, 0, len(models))
	for _, m := range models {
		funds = append(funds, m.toDomain())
	}
	return funds, nil
}

func (r *investmentFundRepository) TransferManagerFunds(ctx context.Context, oldManagerID, newManagerID int64) error {
	return r.db.WithContext(ctx).
		Model(&investmentFundFullModel{}).
		Where("manager_id = ?", oldManagerID).
		Update("manager_id", newManagerID).Error
}

func (r *investmentFundRepository) GetSecurities(ctx context.Context, fundID int64) ([]domain.FundSecurity, error) {
	var models []fundSecurityFullModel
	if err := r.db.WithContext(ctx).Where("fund_id = ?", fundID).Find(&models).Error; err != nil {
		return nil, err
	}
	securities := make([]domain.FundSecurity, 0, len(models))
	for _, m := range models {
		securities = append(securities, m.toDomain())
	}
	return securities, nil
}

// UpsertSecurity dodaje novu poziciju ili ažurira quantity i initial_cost_rsd ako hartija već postoji.
func (r *investmentFundRepository) UpsertSecurity(ctx context.Context, sec domain.FundSecurity) error {
	acquisitionDate := sec.AcquisitionDate
	if acquisitionDate.IsZero() {
		acquisitionDate = time.Now().UTC()
	}
	m := fundSecurityFullModel{
		FundID:          sec.FundID,
		ListingID:       sec.ListingID,
		Quantity:        sec.Quantity,
		AcquisitionDate: acquisitionDate,
		InitialCostRSD:  sec.InitialCostRSD,
	}
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "fund_id"}, {Name: "listing_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"quantity", "initial_cost_rsd", "acquisition_date"}),
		}).
		Create(&m).Error
}

func (r *investmentFundRepository) GetPositions(ctx context.Context, fundID int64) ([]domain.ClientFundPosition, error) {
	var models []clientFundPositionFullModel
	if err := r.db.WithContext(ctx).Where("fund_id = ?", fundID).Find(&models).Error; err != nil {
		return nil, err
	}
	positions := make([]domain.ClientFundPosition, 0, len(models))
	for _, m := range models {
		positions = append(positions, domain.ClientFundPosition{
			ID:               m.ID,
			FundID:           m.FundID,
			UserID:           m.UserID,
			TotalInvestedRSD: m.InvestedRSD,
			LastChanged:      m.LastChanged,
		})
	}
	return positions, nil
}

func (r *investmentFundRepository) GetTotalInvested(ctx context.Context, fundID int64) (float64, error) {
	var total float64
	err := r.db.WithContext(ctx).
		Model(&clientFundPositionFullModel{}).
		Where("fund_id = ?", fundID).
		Select("COALESCE(SUM(invested_rsd), 0)").
		Scan(&total).Error
	return total, err
}

// AddSecurityQuantity accumulates quantity and cost for a fund security.
// Uses INSERT … ON CONFLICT to add deltaQty to the existing quantity rather than replacing it.
func (r *investmentFundRepository) AddSecurityQuantity(ctx context.Context, fundID, listingID int64, deltaQty float64, acquisitionDate time.Time, deltaCostRSD float64) error {
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO core_banking.fund_securities
		    (fund_id, listing_id, quantity, acquisition_date, initial_cost_rsd)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (fund_id, listing_id) DO UPDATE SET
		    quantity         = core_banking.fund_securities.quantity + EXCLUDED.quantity,
		    initial_cost_rsd = core_banking.fund_securities.initial_cost_rsd + EXCLUDED.initial_cost_rsd,
		    acquisition_date = EXCLUDED.acquisition_date`,
		fundID, listingID, deltaQty, acquisitionDate, deltaCostRSD,
	).Error
}

// DeductLiquidAssets decrements liquid_assets of the fund by amountRSD.
func (r *investmentFundRepository) DeductLiquidAssets(ctx context.Context, fundID int64, amountRSD float64) error {
	return r.db.WithContext(ctx).Exec(
		`UPDATE core_banking.investment_funds
		 SET liquid_assets = GREATEST(0, liquid_assets - ?)
		 WHERE id = ?`,
		amountRSD, fundID,
	).Error
}

// WithDB returns a new repository instance scoped to the given *gorm.DB (typically a transaction).
func (r *investmentFundRepository) WithDB(db interface{}) domain.InvestmentFundRepository {
	if gdb, ok := db.(*gorm.DB); ok {
		return &investmentFundRepository{db: gdb}
	}
	return r
}

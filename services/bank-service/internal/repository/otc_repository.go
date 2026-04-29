package repository

// otc_repository.go — implementacija domain.OTCRepository (Faza 2).
// Sve operacije koje menjaju stanje (capacity, transfer premije) se pozivaju
// unutar GORM transakcije iz servisa preko WithTx.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"banka-backend/services/bank-service/internal/domain"

	"gorm.io/gorm"
)

// ─── GORM modeli ──────────────────────────────────────────────────────────────

type otcOfferModel struct {
	ID              int64     `gorm:"column:id;primaryKey"`
	ListingID       int64     `gorm:"column:listing_id"`
	SellerID        int64     `gorm:"column:seller_id"`
	BuyerID         int64     `gorm:"column:buyer_id"`
	BuyerAccountID  int64     `gorm:"column:buyer_account_id"`
	SellerAccountID *int64    `gorm:"column:seller_account_id"`
	Amount          int32     `gorm:"column:amount"`
	PricePerStock   float64   `gorm:"column:price_per_stock"`
	Premium         float64   `gorm:"column:premium"`
	SettlementDate  time.Time `gorm:"column:settlement_date"`
	Status          string    `gorm:"column:status"`
	LastModified    time.Time `gorm:"column:last_modified"`
	ModifiedBy      int64     `gorm:"column:modified_by"`
	CreatedAt       time.Time `gorm:"column:created_at"`
	SellerBankID    *int64    `gorm:"column:seller_bank_id"`
	BuyerBankID     *int64    `gorm:"column:buyer_bank_id"`
}

func (otcOfferModel) TableName() string { return "core_banking.otc_offers" }

func (m otcOfferModel) toDomain() domain.OTCOffer {
	return domain.OTCOffer{
		ID:              m.ID,
		ListingID:       m.ListingID,
		SellerID:        m.SellerID,
		BuyerID:         m.BuyerID,
		BuyerAccountID:  m.BuyerAccountID,
		SellerAccountID: m.SellerAccountID,
		Amount:          m.Amount,
		PricePerStock:   m.PricePerStock,
		Premium:         m.Premium,
		SettlementDate:  m.SettlementDate,
		Status:          domain.OTCOfferStatus(m.Status),
		LastModified:    m.LastModified,
		ModifiedBy:      m.ModifiedBy,
		CreatedAt:       m.CreatedAt,
		SellerBankID:    m.SellerBankID,
		BuyerBankID:     m.BuyerBankID,
	}
}

type otcContractModel struct {
	ID              int64      `gorm:"column:id;primaryKey"`
	OfferID         int64      `gorm:"column:offer_id"`
	ListingID       int64      `gorm:"column:listing_id"`
	SellerID        int64      `gorm:"column:seller_id"`
	BuyerID         int64      `gorm:"column:buyer_id"`
	BuyerAccountID  int64      `gorm:"column:buyer_account_id"`
	SellerAccountID int64      `gorm:"column:seller_account_id"`
	Amount          int32      `gorm:"column:amount"`
	StrikePrice     float64    `gorm:"column:strike_price"`
	Premium         float64    `gorm:"column:premium"`
	SettlementDate  time.Time  `gorm:"column:settlement_date"`
	Status          string     `gorm:"column:status"`
	CreatedAt       time.Time  `gorm:"column:created_at"`
	ExercisedAt     *time.Time `gorm:"column:exercised_at"`
	SellerBankID    *int64     `gorm:"column:seller_bank_id"`
	BuyerBankID     *int64     `gorm:"column:buyer_bank_id"`
}

func (otcContractModel) TableName() string { return "core_banking.otc_contracts" }

func (m otcContractModel) toDomain() domain.OTCContract {
	return domain.OTCContract{
		ID:              m.ID,
		OfferID:         m.OfferID,
		ListingID:       m.ListingID,
		SellerID:        m.SellerID,
		BuyerID:         m.BuyerID,
		BuyerAccountID:  m.BuyerAccountID,
		SellerAccountID: m.SellerAccountID,
		Amount:          m.Amount,
		StrikePrice:     m.StrikePrice,
		Premium:         m.Premium,
		SettlementDate:  m.SettlementDate,
		Status:          domain.OTCContractStatus(m.Status),
		CreatedAt:       m.CreatedAt,
		ExercisedAt:     m.ExercisedAt,
		SellerBankID:    m.SellerBankID,
		BuyerBankID:     m.BuyerBankID,
	}
}

// ─── Repository ───────────────────────────────────────────────────────────────

type otcRepository struct {
	db *gorm.DB
}

func NewOTCRepository(db *gorm.DB) domain.OTCRepository {
	return &otcRepository{db: db}
}

func (r *otcRepository) WithTx(tx interface{}) domain.OTCRepository {
	g, ok := tx.(*gorm.DB)
	if !ok || g == nil {
		return r
	}
	return &otcRepository{db: g}
}

func (r *otcRepository) CreateOffer(ctx context.Context, offer domain.OTCOffer) (*domain.OTCOffer, error) {
	now := time.Now().UTC()
	m := otcOfferModel{
		ListingID:       offer.ListingID,
		SellerID:        offer.SellerID,
		BuyerID:         offer.BuyerID,
		BuyerAccountID:  offer.BuyerAccountID,
		SellerAccountID: offer.SellerAccountID,
		Amount:          offer.Amount,
		PricePerStock:   offer.PricePerStock,
		Premium:         offer.Premium,
		SettlementDate:  offer.SettlementDate,
		Status:          string(domain.OTCOfferPending),
		LastModified:    now,
		ModifiedBy:      offer.ModifiedBy,
		CreatedAt:       now,
		SellerBankID:    offer.SellerBankID,
		BuyerBankID:     offer.BuyerBankID,
	}
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		return nil, fmt.Errorf("create otc offer: %w", err)
	}
	d := m.toDomain()
	return &d, nil
}

func (r *otcRepository) GetOfferByID(ctx context.Context, id int64) (*domain.OTCOffer, error) {
	var m otcOfferModel
	if err := r.db.WithContext(ctx).First(&m, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrOTCOfferNotFound
		}
		return nil, err
	}
	d := m.toDomain()
	return &d, nil
}

// GetOfferByIDForUpdate — koristi se u counter/accept/decline da spreči race
// između paralelnih modifikacija iste ponude.
func (r *otcRepository) GetOfferByIDForUpdate(ctx context.Context, id int64) (*domain.OTCOffer, error) {
	var m otcOfferModel
	if err := r.db.WithContext(ctx).
		Raw("SELECT * FROM core_banking.otc_offers WHERE id = ? FOR UPDATE", id).
		Scan(&m).Error; err != nil {
		return nil, fmt.Errorf("select otc offer for update: %w", err)
	}
	if m.ID == 0 {
		return nil, domain.ErrOTCOfferNotFound
	}
	d := m.toDomain()
	return &d, nil
}

// GetAccountInfo vraća (vlasnikID, valuta) za dati račun.
func (r *otcRepository) GetAccountInfo(ctx context.Context, accountID int64) (int64, string, error) {
	var row struct {
		IDVlasnika   int64  `gorm:"column:id_vlasnika"`
		ValutaOznaka string `gorm:"column:valuta_oznaka"`
	}
	if err := r.db.WithContext(ctx).Raw(`
		SELECT ra.id_vlasnika, v.oznaka AS valuta_oznaka
		FROM core_banking.racun ra
		JOIN core_banking.valuta v ON v.id = ra.id_valute
		WHERE ra.id = ?
	`, accountID).Scan(&row).Error; err != nil {
		return 0, "", fmt.Errorf("lookup racun: %w", err)
	}
	if row.IDVlasnika == 0 {
		return 0, "", domain.ErrOTCAccountNotOwned
	}
	return row.IDVlasnika, row.ValutaOznaka, nil
}

// GetListingCurrency vraća oznaku valute u kojoj se trguje listing (npr. "USD").
func (r *otcRepository) GetListingCurrency(ctx context.Context, listingID int64) (string, error) {
	var row struct {
		ValutaOznaka string `gorm:"column:valuta_oznaka"`
	}
	if err := r.db.WithContext(ctx).Raw(`
		SELECT v.oznaka AS valuta_oznaka
		FROM core_banking.listing l
		LEFT JOIN core_banking.exchange e ON e.id = l.exchange_id
		LEFT JOIN core_banking.valuta v ON v.id = e.currency_id
		WHERE l.id = ?
	`, listingID).Scan(&row).Error; err != nil {
		return "", fmt.Errorf("lookup listing currency: %w", err)
	}
	if row.ValutaOznaka == "" {
		return "", domain.ErrOTCListingNotFound
	}
	return row.ValutaOznaka, nil
}

// UpdateOfferOnCounter — koristi se kada druga strana šalje counter.
// Postavlja modified_by/last_modified na trenutni timestamp.
func (r *otcRepository) UpdateOfferOnCounter(ctx context.Context, offer domain.OTCOffer) error {
	updates := map[string]interface{}{
		"amount":          offer.Amount,
		"price_per_stock": offer.PricePerStock,
		"premium":         offer.Premium,
		"settlement_date": offer.SettlementDate,
		"last_modified":   time.Now().UTC(),
		"modified_by":     offer.ModifiedBy,
	}
	if offer.SellerAccountID != nil {
		updates["seller_account_id"] = *offer.SellerAccountID
	}
	res := r.db.WithContext(ctx).
		Model(&otcOfferModel{}).
		Where("id = ?", offer.ID).
		Updates(updates)
	if res.Error != nil {
		return fmt.Errorf("update otc offer (counter): %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrOTCOfferNotFound
	}
	return nil
}

func (r *otcRepository) UpdateOfferStatus(ctx context.Context, id int64, status domain.OTCOfferStatus, modifiedBy int64) error {
	res := r.db.WithContext(ctx).
		Model(&otcOfferModel{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":        string(status),
			"last_modified": time.Now().UTC(),
			"modified_by":   modifiedBy,
		})
	if res.Error != nil {
		return fmt.Errorf("update otc offer status: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrOTCOfferNotFound
	}
	return nil
}

// AvailablePublicShares vraća (public_shares.quantity − Σ pending offers (excludeOfferID) − Σ valid contracts).
// Treba pozvati nakon SELECT FOR UPDATE u istoj transakciji da se zaštiti od race-a.
func (r *otcRepository) AvailablePublicShares(ctx context.Context, sellerID, listingID, excludeOfferID int64) (int32, error) {
	// 1. quantity iz public_shares (suma redova ako ih ima više).
	var publicQty int64
	if err := r.db.WithContext(ctx).Raw(`
		SELECT COALESCE(SUM(quantity), 0) FROM core_banking.public_shares
		WHERE user_id = ? AND listing_id = ?
	`, sellerID, listingID).Scan(&publicQty).Error; err != nil {
		return 0, fmt.Errorf("read public_shares: %w", err)
	}
	if publicQty <= 0 {
		// Akcija nije postavljena u javni režim za tog vlasnika.
		return 0, domain.ErrOTCNotInPublicRegime
	}

	// 2. SUM aktivnih PENDING ponuda za istog seller-a/listing-a (osim trenutne).
	var pendingSum int64
	if err := r.db.WithContext(ctx).Raw(`
		SELECT COALESCE(SUM(amount), 0) FROM core_banking.otc_offers
		WHERE seller_id = ? AND listing_id = ? AND status = 'PENDING' AND id <> ?
	`, sellerID, listingID, excludeOfferID).Scan(&pendingSum).Error; err != nil {
		return 0, fmt.Errorf("sum pending offers: %w", err)
	}

	// 3. SUM VALID ugovora.
	var contractSum int64
	if err := r.db.WithContext(ctx).Raw(`
		SELECT COALESCE(SUM(amount), 0) FROM core_banking.otc_contracts
		WHERE seller_id = ? AND listing_id = ? AND status = 'VALID'
	`, sellerID, listingID).Scan(&contractSum).Error; err != nil {
		return 0, fmt.Errorf("sum valid contracts: %w", err)
	}

	avail := publicQty - pendingSum - contractSum
	if avail < 0 {
		avail = 0
	}
	return int32(avail), nil
}

// ListOffers — vraća sve ponude u kojima učestvuje korisnik (kao kupac ili prodavac),
// sa derivacijama: ticker, name, exchange acronym, market price, price deviation, color, needsReview.
func (r *otcRepository) ListOffers(ctx context.Context, filter domain.ListOTCOffersFilter) ([]domain.OTCOfferListItem, error) {
	type row struct {
		// otc_offers
		ID              int64
		ListingID       int64
		SellerID        int64
		BuyerID         int64
		BuyerAccountID  int64
		SellerAccountID *int64
		Amount          int32
		PricePerStock   float64
		Premium         float64
		SettlementDate  time.Time
		Status          string
		LastModified    time.Time
		ModifiedBy      int64
		CreatedAt       time.Time
		// joins
		Ticker         string
		StockName      string
		ExchangeAcr    string
		MarketPriceUSD float64
	}

	q := r.db.WithContext(ctx).
		Table("core_banking.otc_offers o").
		Select(`
			o.id, o.listing_id, o.seller_id, o.buyer_id, o.buyer_account_id, o.seller_account_id,
			o.amount, o.price_per_stock, o.premium, o.settlement_date, o.status,
			o.last_modified, o.modified_by, o.created_at,
			l.ticker AS ticker, l.name AS stock_name, e.acronym AS exchange_acr, l.price AS market_price_usd
		`).
		Joins("JOIN core_banking.listing l  ON l.id = o.listing_id").
		Joins("LEFT JOIN core_banking.exchange e ON e.id = l.exchange_id").
		Where("(o.buyer_id = ? OR o.seller_id = ?)", filter.UserID, filter.UserID).
		Order("o.last_modified DESC")

	if filter.Status != nil && *filter.Status != "" {
		q = q.Where("o.status = ?", string(*filter.Status))
	}
	switch filter.Role {
	case "BUYER":
		q = q.Where("o.buyer_id = ?", filter.UserID)
	case "SELLER":
		q = q.Where("o.seller_id = ?", filter.UserID)
	}
	if filter.OnlyMyTurn {
		q = q.Where("o.modified_by <> ?", filter.UserID)
	}

	var rows []row
	if err := q.Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list otc offers: %w", err)
	}

	items := make([]domain.OTCOfferListItem, 0, len(rows))
	for _, x := range rows {
		base := domain.OTCOffer{
			ID:              x.ID,
			ListingID:       x.ListingID,
			SellerID:        x.SellerID,
			BuyerID:         x.BuyerID,
			BuyerAccountID:  x.BuyerAccountID,
			SellerAccountID: x.SellerAccountID,
			Amount:          x.Amount,
			PricePerStock:   x.PricePerStock,
			Premium:         x.Premium,
			SettlementDate:  x.SettlementDate,
			Status:          domain.OTCOfferStatus(x.Status),
			LastModified:    x.LastModified,
			ModifiedBy:      x.ModifiedBy,
			CreatedAt:       x.CreatedAt,
		}
		dev, color := priceDeviation(x.PricePerStock, x.MarketPriceUSD)
		items = append(items, domain.OTCOfferListItem{
			OTCOffer:          base,
			Ticker:            x.Ticker,
			StockName:         x.StockName,
			Exchange:          x.ExchangeAcr,
			MarketPriceUSD:    x.MarketPriceUSD,
			NeedsReview:       x.ModifiedBy != filter.UserID,
			PriceDeviationPct: dev,
			PriceColor:        color,
		})
	}
	return items, nil
}

// ListMarketplace agregira public_shares po (seller, listing), oduzima
// already-reserved količinu (PENDING ponude + VALID ugovori) i isključuje
// callerID-ja kao prodavca (ne prikazujemo svoje akcije za samo-kupovinu).
func (r *otcRepository) ListMarketplace(ctx context.Context, callerID int64) ([]domain.OTCMarketplaceItem, error) {
	type row struct {
		ListingID      int64   `gorm:"column:listing_id"`
		SellerID       int64   `gorm:"column:user_id"`
		PublicQty      int64   `gorm:"column:public_qty"`
		ReservedOffers int64   `gorm:"column:reserved_offers"`
		ReservedConts  int64   `gorm:"column:reserved_contracts"`
		Ticker         string  `gorm:"column:ticker"`
		StockName      string  `gorm:"column:stock_name"`
		ExchangeAcr    string  `gorm:"column:exchange_acr"`
		MarketPriceUSD float64 `gorm:"column:market_price_usd"`
	}
	var rows []row
	if err := r.db.WithContext(ctx).Raw(`
		SELECT
		    ps.listing_id,
		    ps.user_id,
		    SUM(ps.quantity) AS public_qty,
		    COALESCE((SELECT SUM(o.amount) FROM core_banking.otc_offers o
		              WHERE o.seller_id = ps.user_id AND o.listing_id = ps.listing_id
		                AND o.status = 'PENDING'), 0) AS reserved_offers,
		    COALESCE((SELECT SUM(c.amount) FROM core_banking.otc_contracts c
		              WHERE c.seller_id = ps.user_id AND c.listing_id = ps.listing_id
		                AND c.status = 'VALID'), 0) AS reserved_contracts,
		    l.ticker AS ticker,
		    l.name   AS stock_name,
		    e.acronym AS exchange_acr,
		    l.price  AS market_price_usd
		FROM core_banking.public_shares ps
		JOIN core_banking.listing l  ON l.id = ps.listing_id
		LEFT JOIN core_banking.exchange e ON e.id = l.exchange_id
		WHERE ps.user_id <> ?
		GROUP BY ps.listing_id, ps.user_id, l.ticker, l.name, e.acronym, l.price
		ORDER BY l.ticker ASC
	`, callerID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list marketplace: %w", err)
	}

	out := make([]domain.OTCMarketplaceItem, 0, len(rows))
	for _, x := range rows {
		avail := x.PublicQty - x.ReservedOffers - x.ReservedConts
		if avail <= 0 {
			continue
		}
		out = append(out, domain.OTCMarketplaceItem{
			ListingID:         x.ListingID,
			Ticker:            x.Ticker,
			StockName:         x.StockName,
			Exchange:          x.ExchangeAcr,
			MarketPriceUSD:    x.MarketPriceUSD,
			SellerID:          x.SellerID,
			AvailableQuantity: int32(avail),
		})
	}
	return out, nil
}

func (r *otcRepository) GetOfferListItem(ctx context.Context, id, callerID int64) (*domain.OTCOfferListItem, error) {
	items, err := r.ListOffers(ctx, domain.ListOTCOffersFilter{UserID: callerID})
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if it.ID == id {
			return &it, nil
		}
	}
	return nil, domain.ErrOTCOfferNotFound
}

// priceDeviation — pomoć za prikaz: računa odstupanje (%) i vraća boju po specifikaciji.
func priceDeviation(offered, market float64) (pct float64, color string) {
	if market <= 0 {
		return 0, "GREEN"
	}
	pct = (offered - market) / market * 100
	abs := pct
	if abs < 0 {
		abs = -abs
	}
	switch {
	case abs <= 5:
		color = "GREEN"
	case abs <= 20:
		color = "YELLOW"
	default:
		color = "RED"
	}
	return pct, color
}

func (r *otcRepository) CreateContract(ctx context.Context, c domain.OTCContract) (*domain.OTCContract, error) {
	m := otcContractModel{
		OfferID:         c.OfferID,
		ListingID:       c.ListingID,
		SellerID:        c.SellerID,
		BuyerID:         c.BuyerID,
		BuyerAccountID:  c.BuyerAccountID,
		SellerAccountID: c.SellerAccountID,
		Amount:          c.Amount,
		StrikePrice:     c.StrikePrice,
		Premium:         c.Premium,
		SettlementDate:  c.SettlementDate,
		Status:          string(domain.OTCContractValid),
		CreatedAt:       time.Now().UTC(),
		SellerBankID:    c.SellerBankID,
		BuyerBankID:     c.BuyerBankID,
	}
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		return nil, fmt.Errorf("create otc contract: %w", err)
	}
	d := m.toDomain()
	return &d, nil
}

// Napomena: transfer premije je preseljen u PaymentService
// (ExecuteOTCPremiumTransfer) zbog audit logova, knjiženja i FX podrške.

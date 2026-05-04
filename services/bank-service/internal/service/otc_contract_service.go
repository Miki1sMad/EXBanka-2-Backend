package service

// otc_contract_service.go — implementacija domain.OTCContractService.
//
// Ova datoteka sadrži:
//   1. ListContracts / GetContract  — čitanje OTC ugovora sa izvedenim vrednostima
//   2. ExerciseContract             — SAGA orchestrator za izvršavanje OTC ugovora
//
// SAGA koraci (po specifikaciji Celine 4):
//   STEP 1 — RESERVE_FUNDS:       Rezerviši iznos (amount × strikePrice) na buyerAccount
//   STEP 2 — RESERVE_SECURITIES:  Ukloni amount akcija iz prodavčevih public_shares
//   STEP 3 — TRANSFER_FUNDS:      Prebaci sredstva: buyerAccount → sellerAccount
//   STEP 4 — TRANSFER_OWNERSHIP:  Kreiraj sintetički DONE BUY/SELL nalog (tagovan sa otc_saga_execution_id)
//   STEP 5 — COMPLETED:           Double-check + označi ugovor EXERCISED, SAGA COMPLETED
//
// Kompenzacije (rollback od poslednjeg koraka unazad):
//   comp(TRANSFER_OWNERSHIP) — delete sintetičkih naloga po otc_saga_execution_id (precizno, bez heuristike)
//   comp(TRANSFER_FUNDS)     — vrati sredstva: sellerAccount → buyerAccount
//   comp(RESERVE_SECURITIES) — vrati akcije u public_shares prodavca
//   comp(RESERVE_FUNDS)      — oslobodi rezervisana sredstva na buyerAccount
//
// Svaka kompenzacija se pokušava do maxCompRetries puta, sa kratkim sleep-om
// između pokušaja. Ako sve retry-eve potroši, SAGA prelazi u COMPENSATION_FAILED
// i zahteva manuelnu intervenciju — admin može ponovo pokrenuti iz step_log-a.

import (
	"context"
	"fmt"
	"log"
	"time"

	"banka-backend/services/bank-service/internal/domain"

	"gorm.io/gorm"
)

const (
	maxCompRetries = 3
	compRetryDelay = 500 * time.Millisecond
)

// ─── Tip i konstruktor ────────────────────────────────────────────────────────

type otcContractService struct {
	db              *gorm.DB
	contractRepo    domain.OTCRepository
	sagaRepo        domain.OTCSagaRepository
	listingService  domain.ListingService
	exchangeService domain.ExchangeService
}

// NewOTCContractService kreira OTCContractService sa svim zavisnostima.
func NewOTCContractService(
	db *gorm.DB,
	contractRepo domain.OTCRepository,
	sagaRepo domain.OTCSagaRepository,
	listingService domain.ListingService,
	exchangeService domain.ExchangeService,
) domain.OTCContractService {
	return &otcContractService{
		db:              db,
		contractRepo:    contractRepo,
		sagaRepo:        sagaRepo,
		listingService:  listingService,
		exchangeService: exchangeService,
	}
}

// ─── ListContracts ────────────────────────────────────────────────────────────

func (s *otcContractService) ListContracts(ctx context.Context, userID int64) ([]domain.OTCContractListItem, error) {
	contracts, err := s.contractRepo.ListContracts(ctx, userID)
	if err != nil {
		return nil, err
	}

	usdRate := s.usdToRSDRate(ctx)
	items := make([]domain.OTCContractListItem, 0, len(contracts))
	for _, c := range contracts {
		item := s.enrichContract(ctx, c, usdRate)
		items = append(items, item)
	}
	return items, nil
}

// ─── GetContract ──────────────────────────────────────────────────────────────

func (s *otcContractService) GetContract(ctx context.Context, contractID, callerID int64) (*domain.OTCContractListItem, error) {
	c, err := s.contractRepo.GetContractByID(ctx, contractID)
	if err != nil {
		return nil, err
	}
	if c.BuyerID != callerID && c.SellerID != callerID {
		return nil, domain.ErrOTCNotParticipant
	}
	usdRate := s.usdToRSDRate(ctx)
	item := s.enrichContract(ctx, *c, usdRate)
	return &item, nil
}

// enrichContract popunjava izvedena polja (Profit, SellerInfo, Ticker...) za jedan ugovor.
func (s *otcContractService) enrichContract(ctx context.Context, c domain.OTCContract, usdToRSD float64) domain.OTCContractListItem {
	item := domain.OTCContractListItem{OTCContract: c}

	listing, err := s.listingService.GetListingByID(ctx, c.ListingID)
	if err == nil {
		item.Ticker = listing.Ticker
		item.StockName = listing.Name
		item.CurrentPrice = listing.Price
		// Profit = (tržišna cena - strike) × količina - premija (sva u USD, prikaz na FE-u)
		item.Profit = (listing.Price-c.StrikePrice)*float64(c.Amount) - c.Premium
	}

	// SellerInfo = "Ime Prezime, BankaNaziv" (best-effort; prazno ako lookup ne uspe)
	// Ime se rezolvira naknadno u handler-u (gde je userClient dostupan).
	item.SellerBankName = "EXBanka"
	_ = usdToRSD
	return item
}

// ─── ExerciseContract (SAGA orchestrator) ─────────────────────────────────────

func (s *otcContractService) ExerciseContract(ctx context.Context, in domain.ExerciseOTCContractInput) (*domain.OTCSagaExecution, error) {
	// ── Preduslov: učitaj i validiraj ugovor ──────────────────────────────────
	contract, err := s.contractRepo.GetContractByID(ctx, in.ContractID)
	if err != nil {
		return nil, err
	}
	if contract.BuyerID != in.CallerID {
		return nil, domain.ErrOTCContractNotBuyer
	}
	if contract.Status == domain.OTCContractExercised {
		return nil, domain.ErrOTCContractAlreadyExecuted
	}
	if contract.Status == domain.OTCContractExpired || time.Now().After(contract.SettlementDate) {
		return nil, domain.ErrOTCContractExpired
	}

	// ── Sprečavanje dvostrukog pokretanja ─────────────────────────────────────
	existing, err := s.sagaRepo.GetExecutionByContractID(ctx, in.ContractID)
	if err != nil {
		return nil, fmt.Errorf("provjera SAGA stanja: %w", err)
	}
	if existing != nil && existing.Status == domain.OTCSagaStatusInProgress {
		return nil, domain.ErrOTCSagaAlreadyRunning
	}

	// Izračunaj ukupan iznos koji kupac treba platiti (u USD jer je račun u USD/RSD).
	totalCost := float64(contract.Amount) * contract.StrikePrice

	// ── Kreiraj SAGA execution red u bazi ────────────────────────────────────
	exec, err := s.sagaRepo.CreateExecution(ctx, in.ContractID, in.CallerID, totalCost)
	if err != nil {
		return nil, fmt.Errorf("inicijalizacija SAGA: %w", err)
	}

	// ── Pokretanje SAGA u pozadini (ne blokiramo HTTP request) ───────────────
	// Koristimo background context da SAGA ne bude prekinuta ako HTTP klijent
	// zatvori konekciju pre nego što se završi.
	go s.runSaga(context.Background(), exec, contract)

	return exec, nil
}

// ─── runSaga — glavna SAGA state mašina ──────────────────────────────────────

func (s *otcContractService) runSaga(ctx context.Context, exec *domain.OTCSagaExecution, c *domain.OTCContract) {
	log.Printf("[SAGA] contract=%d exec=%d: start", c.ID, exec.ID)

	// Svaki step se izvršava u sopstvenoj DB transakciji.
	// Ako jedan step ne uspe, kreće kompenzacija od tog koraka unazad.

	// STEP 1: RESERVE_FUNDS
	if err := s.stepReserveFunds(ctx, exec, c); err != nil {
		s.compensateFrom(ctx, exec, domain.OTCSagaStepPending, err)
		return
	}

	// STEP 2: RESERVE_SECURITIES
	if err := s.stepReserveSecurities(ctx, exec, c); err != nil {
		s.compensateFrom(ctx, exec, domain.OTCSagaStepReserveFunds, err)
		return
	}

	// STEP 3: TRANSFER_FUNDS
	if err := s.stepTransferFunds(ctx, exec, c); err != nil {
		s.compensateFrom(ctx, exec, domain.OTCSagaStepReserveSecurities, err)
		return
	}

	// STEP 4: TRANSFER_OWNERSHIP
	if err := s.stepTransferOwnership(ctx, exec, c); err != nil {
		s.compensateFrom(ctx, exec, domain.OTCSagaStepTransferFunds, err)
		return
	}

	// STEP 5: COMPLETED — double-check konzistentnosti + finalizacija
	if err := s.markCompleted(ctx, exec, c); err != nil {
		s.compensateFrom(ctx, exec, domain.OTCSagaStepTransferOwnership, err)
		return
	}
}

// ─── SAGA koraci ──────────────────────────────────────────────────────────────

// stepReserveFunds rezerviše totalCost na kupčevom računu (povećava rezervisana_sredstva).
func (s *otcContractService) stepReserveFunds(ctx context.Context, exec *domain.OTCSagaExecution, c *domain.OTCContract) error {
	txErr := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Provjera da kupac ima dovoljno raspoloživih sredstava.
		var row struct {
			Stanje float64 `gorm:"column:stanje"`
			Rezerv float64 `gorm:"column:rezerv"`
		}
		if err := tx.Raw(`
			SELECT stanje_racuna AS stanje, rezervisana_sredstva AS rezerv
			FROM core_banking.racun WHERE id = ? AND status = 'AKTIVAN' FOR UPDATE
		`, c.BuyerAccountID).Scan(&row).Error; err != nil {
			return fmt.Errorf("lock buyer account: %w", err)
		}
		if row.Stanje == 0 && row.Rezerv == 0 {
			return domain.ErrOTCAccountNotOwned
		}
		available := row.Stanje - row.Rezerv
		if available < exec.BuyerReservedAmount {
			return domain.ErrOTCInsufficientFunds
		}
		// Povećaj rezervisana_sredstva.
		return tx.Exec(`
			UPDATE core_banking.racun
			SET rezervisana_sredstva = rezervisana_sredstva + ?
			WHERE id = ? AND status = 'AKTIVAN'
		`, exec.BuyerReservedAmount, c.BuyerAccountID).Error
	})

	return s.recordStep(ctx, exec, domain.OTCSagaStepReserveFunds, txErr)
}

// stepReserveSecurities smanjuje količinu u public_shares prodavca.
func (s *otcContractService) stepReserveSecurities(ctx context.Context, exec *domain.OTCSagaExecution, c *domain.OTCContract) error {
	txErr := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Provjera da prodavac ima dovoljno akcija u public_shares.
		var pubQty int64
		if err := tx.Raw(`
			SELECT COALESCE(SUM(quantity), 0)
			FROM core_banking.public_shares
			WHERE user_id = ? AND listing_id = ? FOR UPDATE
		`, c.SellerID, c.ListingID).Scan(&pubQty).Error; err != nil {
			return fmt.Errorf("lock public_shares: %w", err)
		}
		if pubQty < int64(c.Amount) {
			return domain.ErrOTCInsufficientCapacity
		}
		// Smanji količinu; briši red ako pada na 0.
		if err := tx.Exec(`
			UPDATE core_banking.public_shares
			SET quantity = quantity - ?
			WHERE user_id = ? AND listing_id = ?
		`, c.Amount, c.SellerID, c.ListingID).Error; err != nil {
			return fmt.Errorf("deduct public_shares: %w", err)
		}
		// Počisti redove sa quantity <= 0.
		return tx.Exec(`
			DELETE FROM core_banking.public_shares
			WHERE user_id = ? AND listing_id = ? AND quantity <= 0
		`, c.SellerID, c.ListingID).Error
	})

	return s.recordStep(ctx, exec, domain.OTCSagaStepReserveSecurities, txErr)
}

// stepTransferFunds prenosi sredstva sa buyerAccount na sellerAccount.
// Koristi isti debit/credit pattern kao PaymentService (audit transakcija).
func (s *otcContractService) stepTransferFunds(ctx context.Context, exec *domain.OTCSagaExecution, c *domain.OTCContract) error {
	now := time.Now().UTC()
	desc := fmt.Sprintf("OTC izvršavanje ugovora #%d — prenos sredstava", c.ID)

	txErr := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Debit kupca: smanji stanje i rezervu (rezerva je bila podignuta u koraku 1).
		if err := tx.Exec(`
			UPDATE core_banking.racun
			SET stanje_racuna        = stanje_racuna        - ?,
			    rezervisana_sredstva = rezervisana_sredstva - ?
			WHERE id = ? AND status = 'AKTIVAN'
		`, exec.BuyerReservedAmount, exec.BuyerReservedAmount, c.BuyerAccountID).Error; err != nil {
			return fmt.Errorf("debit buyer: %w", err)
		}
		// Audit zapis za kupca.
		if err := tx.Exec(`
			INSERT INTO core_banking.transakcija
			    (racun_id, tip_transakcije, iznos, opis, vreme_izvrsavanja, status)
			VALUES (?, 'ISPLATA', ?, ?, ?, 'IZVRSEN')
		`, c.BuyerAccountID, exec.BuyerReservedAmount, desc, now).Error; err != nil {
			return fmt.Errorf("audit buyer: %w", err)
		}
		// Credit prodavca.
		if err := tx.Exec(`
			UPDATE core_banking.racun
			SET stanje_racuna = stanje_racuna + ?
			WHERE id = ? AND status = 'AKTIVAN'
		`, exec.BuyerReservedAmount, c.SellerAccountID).Error; err != nil {
			return fmt.Errorf("credit seller: %w", err)
		}
		// Audit zapis za prodavca.
		return tx.Exec(`
			INSERT INTO core_banking.transakcija
			    (racun_id, tip_transakcije, iznos, opis, vreme_izvrsavanja, status)
			VALUES (?, 'UPLATA', ?, ?, ?, 'IZVRSEN')
		`, c.SellerAccountID, exec.BuyerReservedAmount, desc, now).Error
	})

	return s.recordStep(ctx, exec, domain.OTCSagaStepTransferFunds, txErr)
}

// stepTransferOwnership kreira sintetički DONE BUY nalog za kupca i DONE SELL za prodavca.
// Oba naloga su označena sa otc_saga_execution_id (migr. 000050) što omogućava
// precizno brisanje u compensateTransferOwnership bez vremenskih heuristika.
func (s *otcContractService) stepTransferOwnership(ctx context.Context, exec *domain.OTCSagaExecution, c *domain.OTCContract) error {
	now := time.Now().UTC()

	txErr := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Sintetički BUY nalog za kupca (pokazuje akcije u portfolio-u).
		if err := tx.Exec(`
			INSERT INTO core_banking.orders
			  (user_id, account_id, listing_id, order_type, direction, quantity, contract_size,
			   price_per_unit, status, is_done, remaining_portions,
			   after_hours, all_or_none, margin, last_modified, created_at,
			   otc_saga_execution_id)
			VALUES (?, ?, ?, 'MARKET', 'BUY', ?, 1, ?, 'DONE', TRUE, 0,
			        FALSE, FALSE, FALSE, ?, ?, ?)
		`, c.BuyerID, c.BuyerAccountID, c.ListingID, c.Amount, c.StrikePrice, now, now, exec.ID).Error; err != nil {
			return fmt.Errorf("create synthetic BUY order: %w", err)
		}
		// Sintetički SELL nalog za prodavca (uklanja akcije iz portfolio-a).
		if err := tx.Exec(`
			INSERT INTO core_banking.orders
			  (user_id, account_id, listing_id, order_type, direction, quantity, contract_size,
			   price_per_unit, status, is_done, remaining_portions,
			   after_hours, all_or_none, margin, last_modified, created_at,
			   otc_saga_execution_id)
			VALUES (?, ?, ?, 'MARKET', 'SELL', ?, 1, ?, 'DONE', TRUE, 0,
			        FALSE, FALSE, FALSE, ?, ?, ?)
		`, c.SellerID, c.SellerAccountID, c.ListingID, c.Amount, c.StrikePrice, now, now, exec.ID).Error; err != nil {
			return fmt.Errorf("create synthetic SELL order: %w", err)
		}
		return nil
	})

	return s.recordStep(ctx, exec, domain.OTCSagaStepTransferOwnership, txErr)
}

// markCompleted izvršava double-check konzistentnosti, pa atomično označava
// ugovor kao EXERCISED i SAGU kao COMPLETED.
// Vraća grešku ako double-check ili DB transakcija ne uspeju — pozivalac
// (runSaga) tada pokreće kompenzaciju od koraka TRANSFER_OWNERSHIP unazad.
func (s *otcContractService) markCompleted(ctx context.Context, exec *domain.OTCSagaExecution, c *domain.OTCContract) error {
	// ── Double-check: verifikuj da sintetički BUY nalog iz koraka 4 postoji ──
	// Koristimo otc_saga_execution_id (dodat migracijom 000050) za egzaktno
	// podudaranje — nema vremenskih heuristika, nema lažnih pozitiva.
	var syntheticCount int64
	if err := s.db.WithContext(ctx).Raw(`
		SELECT COUNT(*)
		FROM core_banking.orders
		WHERE otc_saga_execution_id = ?
		  AND user_id    = ?
		  AND listing_id = ?
		  AND direction  = 'BUY'
		  AND status     = 'DONE'
		  AND is_done    = TRUE
	`, exec.ID, c.BuyerID, c.ListingID).Scan(&syntheticCount).Error; err != nil {
		return fmt.Errorf("double-check query: %w", err)
	}
	if syntheticCount == 0 {
		return fmt.Errorf("double-check: sintetički BUY nalog nije pronađen (exec_id=%d, contract_id=%d)", exec.ID, c.ID)
	}

	// ── Finalizuj: atomično označi ugovor i SAGU kao završene ────────────────
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.contractRepo.WithTx(tx).UpdateContractStatus(ctx, c.ID, domain.OTCContractExercised); err != nil {
			return err
		}
		return s.sagaRepo.WithTx(tx).UpdateStep(ctx, exec.ID,
			domain.OTCSagaStepCompleted,
			domain.OTCSagaStatusCompleted,
			"",
		)
	}); err != nil {
		return fmt.Errorf("finalizacija SAGA zapisa: %w", err)
	}

	_ = s.sagaRepo.LogStep(ctx, exec.ID, domain.OTCSagaStepCompleted, domain.OTCSagaStepStatusCompleted, "", 1)
	log.Printf("[SAGA] contract=%d exec=%d: COMPLETED", c.ID, exec.ID)
	return nil
}

// ─── Kompenzacijska logika ────────────────────────────────────────────────────

// compensateFrom pokreće rollback od danog koraka unazad.
// failedStep je POSLEDNJI USPEŠNO ZAVRŠEN korak (od njega idemo unazad).
func (s *otcContractService) compensateFrom(ctx context.Context, exec *domain.OTCSagaExecution, lastSuccessful domain.OTCSagaStep, originalErr error) {
	log.Printf("[SAGA] contract=%d exec=%d: kompenzacija od koraka %s zbog: %v",
		exec.ContractID, exec.ID, lastSuccessful, originalErr)

	_ = s.sagaRepo.UpdateStep(ctx, exec.ID, lastSuccessful, domain.OTCSagaStatusCompensating, originalErr.Error())

	type compensationFn struct {
		step string
		fn   func() error
	}

	// Redosled kompenzacija — od poslednjeg uspešnog ka prvom.
	allSteps := []compensationFn{
		{string(domain.OTCSagaStepTransferOwnership), func() error { return s.compensateTransferOwnership(ctx, exec, exec.ContractID) }},
		{string(domain.OTCSagaStepTransferFunds), func() error { return s.compensateTransferFunds(ctx, exec) }},
		{string(domain.OTCSagaStepReserveSecurities), func() error { return s.compensateReserveSecurities(ctx, exec, exec.ContractID) }},
		{string(domain.OTCSagaStepReserveFunds), func() error { return s.compensateReserveFunds(ctx, exec) }},
	}

	// Pronađi indeks od kojeg trebamo da počnemo.
	startIdx := -1
	for i, st := range allSteps {
		if st.step == string(lastSuccessful) {
			startIdx = i
			break
		}
	}
	if startIdx == -1 {
		// PENDING — ništa nije urađeno, samo označi FAILED.
		_ = s.sagaRepo.UpdateStep(ctx, exec.ID, lastSuccessful, domain.OTCSagaStatusFailed, originalErr.Error())
		return
	}

	overallFailed := false
	for i := startIdx; i < len(allSteps); i++ {
		step := allSteps[i]
		sagaStep := domain.OTCSagaStep(step.step)

		var compErr error
		for attempt := 1; attempt <= maxCompRetries; attempt++ {
			_, retryErr := s.sagaRepo.IncrementRetry(ctx, exec.ID)
			if retryErr != nil {
				log.Printf("[SAGA] exec=%d: ne mogu ažurirati retry_count: %v", exec.ID, retryErr)
			}

			compErr = step.fn()
			if compErr == nil {
				_ = s.sagaRepo.LogStep(ctx, exec.ID, sagaStep, domain.OTCSagaStepStatusCompensated, "", attempt)
				log.Printf("[SAGA] exec=%d: kompenzacija koraka %s uspešna (pokušaj %d)", exec.ID, step.step, attempt)
				break
			}

			_ = s.sagaRepo.LogStep(ctx, exec.ID, sagaStep, domain.OTCSagaStepStatusCompensationFailed, compErr.Error(), attempt)
			log.Printf("[SAGA] exec=%d: kompenzacija koraka %s neuspešna (pokušaj %d/%d): %v",
				exec.ID, step.step, attempt, maxCompRetries, compErr)

			if attempt < maxCompRetries {
				time.Sleep(compRetryDelay)
			}
		}

		if compErr != nil {
			overallFailed = true
			log.Printf("[SAGA] exec=%d: kompenzacija koraka %s iscrpela sve retry pokušaje — manuelna intervencija potrebna", exec.ID, step.step)
		}
	}

	finalStatus := domain.OTCSagaStatusFailed
	if overallFailed {
		finalStatus = domain.OTCSagaStatusCompensationFailed
	}
	_ = s.sagaRepo.UpdateStep(ctx, exec.ID, lastSuccessful, finalStatus, originalErr.Error())
}

// ─── Individualne kompenzacione akcije ───────────────────────────────────────

// compensateReserveFunds oslobađa rezervisana sredstva na buyerAccount.
func (s *otcContractService) compensateReserveFunds(ctx context.Context, exec *domain.OTCSagaExecution) error {
	return s.db.WithContext(ctx).Exec(`
		UPDATE core_banking.racun
		SET rezervisana_sredstva = GREATEST(0, rezervisana_sredstva - ?)
		WHERE id = (
		    SELECT buyer_account_id FROM core_banking.otc_contracts WHERE id = ?
		) AND status = 'AKTIVAN'
	`, exec.BuyerReservedAmount, exec.ContractID).Error
}

// compensateReserveSecurities vraća akcije nazad u public_shares prodavca.
func (s *otcContractService) compensateReserveSecurities(ctx context.Context, _ *domain.OTCSagaExecution, contractID int64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var c struct {
			SellerID  int64 `gorm:"column:seller_id"`
			ListingID int64 `gorm:"column:listing_id"`
			Amount    int32 `gorm:"column:amount"`
		}
		if err := tx.Raw(`
			SELECT seller_id, listing_id, amount FROM core_banking.otc_contracts WHERE id = ?
		`, contractID).Scan(&c).Error; err != nil {
			return err
		}
		// Pokuša UPDATE; ako red ne postoji, INSERT.
		res := tx.Exec(`
			UPDATE core_banking.public_shares
			SET quantity = quantity + ?
			WHERE user_id = ? AND listing_id = ?
		`, c.Amount, c.SellerID, c.ListingID)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return tx.Exec(`
				INSERT INTO core_banking.public_shares (listing_id, user_id, quantity)
				VALUES (?, ?, ?)
			`, c.ListingID, c.SellerID, c.Amount).Error
		}
		return nil
	})
}

// compensateTransferFunds vraća sredstva sa sellerAccount na buyerAccount.
func (s *otcContractService) compensateTransferFunds(ctx context.Context, exec *domain.OTCSagaExecution) error {
	now := time.Now().UTC()
	desc := fmt.Sprintf("SAGA rollback — povrat sredstava za ugovor #%d", exec.ContractID)

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var c struct {
			BuyerAccountID  int64 `gorm:"column:buyer_account_id"`
			SellerAccountID int64 `gorm:"column:seller_account_id"`
		}
		if err := tx.Raw(`
			SELECT buyer_account_id, seller_account_id FROM core_banking.otc_contracts WHERE id = ?
		`, exec.ContractID).Scan(&c).Error; err != nil {
			return err
		}
		// Debit prodavca (vraćamo mu oduzeto).
		if err := tx.Exec(`
			UPDATE core_banking.racun SET stanje_racuna = stanje_racuna - ? WHERE id = ? AND status = 'AKTIVAN'
		`, exec.BuyerReservedAmount, c.SellerAccountID).Error; err != nil {
			return fmt.Errorf("rollback debit seller: %w", err)
		}
		_ = tx.Exec(`
			INSERT INTO core_banking.transakcija (racun_id, tip_transakcije, iznos, opis, vreme_izvrsavanja, status)
			VALUES (?, 'ISPLATA', ?, ?, ?, 'IZVRSEN')
		`, c.SellerAccountID, exec.BuyerReservedAmount, desc, now)
		// Credit kupca (vraćamo mu sredstva).
		if err := tx.Exec(`
			UPDATE core_banking.racun SET stanje_racuna = stanje_racuna + ? WHERE id = ? AND status = 'AKTIVAN'
		`, exec.BuyerReservedAmount, c.BuyerAccountID).Error; err != nil {
			return fmt.Errorf("rollback credit buyer: %w", err)
		}
		_ = tx.Exec(`
			INSERT INTO core_banking.transakcija (racun_id, tip_transakcije, iznos, opis, vreme_izvrsavanja, status)
			VALUES (?, 'UPLATA', ?, ?, ?, 'IZVRSEN')
		`, c.BuyerAccountID, exec.BuyerReservedAmount, desc, now)
		return nil
	})
}

// compensateTransferOwnership briše sintetičke DONE naloge kreirane u koraku 4.
// Koristi otc_saga_execution_id za precizno podudaranje — nema vremenskih
// heuristika i nema rizika od brisanja legitimnih naloga drugog korisnika.
func (s *otcContractService) compensateTransferOwnership(ctx context.Context, exec *domain.OTCSagaExecution, _ int64) error {
	return s.db.WithContext(ctx).Exec(`
		DELETE FROM core_banking.orders
		WHERE otc_saga_execution_id = ?
	`, exec.ID).Error
}

// ─── Pomoćne metode ────────────────────────────────────────────────────────────

// recordStep ažurira current_step u SAGA egzekuciji i beleži step u log.
// Vraća err nepromenjenu (za lančanje u runSaga).
func (s *otcContractService) recordStep(ctx context.Context, exec *domain.OTCSagaExecution, step domain.OTCSagaStep, err error) error {
	if err != nil {
		_ = s.sagaRepo.LogStep(ctx, exec.ID, step, domain.OTCSagaStepStatusFailed, err.Error(), 1)
		_ = s.sagaRepo.UpdateStep(ctx, exec.ID, step, domain.OTCSagaStatusInProgress, err.Error())
		log.Printf("[SAGA] exec=%d korak %s FAILED: %v", exec.ID, step, err)
		return err
	}
	_ = s.sagaRepo.LogStep(ctx, exec.ID, step, domain.OTCSagaStepStatusCompleted, "", 1)
	_ = s.sagaRepo.UpdateStep(ctx, exec.ID, step, domain.OTCSagaStatusInProgress, "")
	// Ažuriraj lokalni exec objekat da compensateFrom zna dokle smo stigli.
	exec.CurrentStep = step
	log.Printf("[SAGA] exec=%d korak %s COMPLETED", exec.ID, step)
	return nil
}

// usdToRSDRate vraća srednji kurs USD→RSD; fallback 1.0.
func (s *otcContractService) usdToRSDRate(ctx context.Context) float64 {
	rates, err := s.exchangeService.GetRates(ctx)
	if err != nil {
		return 1.0
	}
	for _, r := range rates {
		if r.Oznaka == "USD" && r.Srednji > 0 {
			return r.Srednji
		}
	}
	return 1.0
}

// ─── Provjera da implementira interface (compile-time guard) ──────────────────

var _ domain.OTCContractService = (*otcContractService)(nil)

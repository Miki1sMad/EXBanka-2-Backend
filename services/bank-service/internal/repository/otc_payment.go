package repository

// otc_payment.go — auditovana putanja za isplatu OTC premije.
// Koristi PaymentRepository da bi se transfer izvršio kroz iste mehanizme
// (lockovanje računa, knjiženja u core_banking.transakcija, FX kroz trezorske
// račune banke) kao i ostali money-movement tokovi.

import (
	"context"
	"fmt"
	"sort"
	"time"

	"banka-backend/services/bank-service/internal/domain"

	"gorm.io/gorm"
)

// ExecuteOTCPremiumTransfer izvršava transfer premije sa kupčevog na prodavčev
// račun unutar PROSLEĐENE GORM transakcije (`tx`). Mora se zvati iz iste tx u
// kojoj se kreira OTC ugovor i menja status ponude — što garantuje atomičnost.
//
// Logika valuta:
//   - Premium je iznos u valuti listinga (npr. USD).
//   - Ako su buyer/seller računi u istoj valuti kao listing, vrši se direktan
//     interni prenos između računa, sa knjiženjima ISPLATA/UPLATA.
//   - Ako se valute razlikuju, FX se izvršava kroz bankarske trezorske račune
//     (vlasnik_id = 2, kao u VerifyAndExecute), uz spread + proviziju.
//
// Vraća:
//   - domain.ErrOTCInsufficientFunds   ako kupac nema raspoloživo stanje
//   - domain.ErrOTCAccountNotOwned     ako račun ne postoji ili nije AKTIVAN
func (r *paymentRepository) ExecuteOTCPremiumTransfer(
	ctx context.Context,
	tx interface{},
	in domain.OTCPremiumTransferInput,
) error {
	gtx, ok := tx.(*gorm.DB)
	if !ok || gtx == nil {
		return fmt.Errorf("ExecuteOTCPremiumTransfer: nedostaje *gorm.DB tx")
	}
	if in.AmountInListingCurrency <= 0 {
		return nil // ništa za prebaciti
	}
	if in.BuyerAccountID == in.SellerAccountID {
		return domain.ErrOTCInvalidInput
	}

	gtx = gtx.WithContext(ctx)

	// 1) Učitaj buyer i seller račun (i pretvori u skup za lock).
	loadAccount := func(id int64) (*racunForPayment, error) {
		var a racunForPayment
		if err := gtx.Raw(`
			SELECT ra.id, ra.broj_racuna, ra.id_vlasnika, ra.id_valute,
			       ra.stanje_racuna, ra.rezervisana_sredstva,
			       COALESCE(ra.dnevni_limit, 0) AS dnevni_limit,
			       COALESCE(ra.mesecni_limit, 0) AS mesecni_limit,
			       COALESCE(ra.dnevna_potrosnja, 0) AS dnevna_potrosnja,
			       COALESCE(ra.mesecna_potrosnja, 0) AS mesecna_potrosnja,
			       ra.status, v.oznaka AS valuta_oznaka
			FROM core_banking.racun ra
			JOIN core_banking.valuta v ON v.id = ra.id_valute
			WHERE ra.id = ?
		`, id).Scan(&a).Error; err != nil {
			return nil, err
		}
		if a.ID == 0 {
			return nil, domain.ErrOTCAccountNotOwned
		}
		return &a, nil
	}

	buyerAcc, err := loadAccount(in.BuyerAccountID)
	if err != nil {
		return err
	}
	sellerAcc, err := loadAccount(in.SellerAccountID)
	if err != nil {
		return err
	}

	listingCcy := in.ListingCurrency
	if listingCcy == "" {
		listingCcy = "USD"
	}

	// 2) Izračunaj iznose i identifikuj potencijalne FX leg-ove.
	// Buyer plaća u svojoj valuti, ali "obaveza" je u listingCcy:
	//   amountBuyerLeg = convert(listingCcy → buyerCcy, premium)  (klijent plaća)
	// Seller prima u svojoj valuti:
	//   amountSellerLeg = convert(listingCcy → sellerCcy, premium) (klijent dobija)
	amountBuyerLeg, _, _ := convertPaymentAmountWithFee(listingCcy, buyerAcc.ValutaOznaka, in.AmountInListingCurrency)
	amountSellerLeg, _, _ := convertPaymentAmountWithFee(listingCcy, sellerAcc.ValutaOznaka, in.AmountInListingCurrency)

	// 3) Pronađi trezorske račune banke za sve uključene valute.
	currencies := map[string]struct{}{
		buyerAcc.ValutaOznaka:  {},
		sellerAcc.ValutaOznaka: {},
		listingCcy:             {},
	}
	currList := make([]string, 0, len(currencies))
	for c := range currencies {
		currList = append(currList, c)
	}

	type bankRow struct {
		ID           int64  `gorm:"column:id"`
		ValutaOznaka string `gorm:"column:valuta_oznaka"`
	}
	var bankRows []bankRow
	if err := gtx.Raw(`
		SELECT ra.id, v.oznaka AS valuta_oznaka
		FROM core_banking.racun ra
		JOIN core_banking.valuta v ON v.id = ra.id_valute
		WHERE ra.id_vlasnika = 2 AND ra.status = 'AKTIVAN' AND v.oznaka IN ?
	`, currList).Scan(&bankRows).Error; err != nil {
		return fmt.Errorf("lookup trezorskih računa: %w", err)
	}
	treasury := make(map[string]int64, len(bankRows))
	for _, br := range bankRows {
		treasury[br.ValutaOznaka] = br.ID
	}
	// Treba nam trezor za buyer/seller currency kad je FX uključen.
	if buyerAcc.ValutaOznaka != listingCcy && treasury[buyerAcc.ValutaOznaka] == 0 {
		return fmt.Errorf("trezorski račun za %s nedostupan", buyerAcc.ValutaOznaka)
	}
	if sellerAcc.ValutaOznaka != listingCcy && treasury[sellerAcc.ValutaOznaka] == 0 {
		return fmt.Errorf("trezorski račun za %s nedostupan", sellerAcc.ValutaOznaka)
	}
	if (buyerAcc.ValutaOznaka != listingCcy || sellerAcc.ValutaOznaka != listingCcy) &&
		treasury[listingCcy] == 0 {
		return fmt.Errorf("trezorski račun za %s nedostupan", listingCcy)
	}

	// 4) Lock svih uključenih računa u determinističkom redosledu (anti-deadlock).
	idsSet := map[int64]struct{}{
		buyerAcc.ID:  {},
		sellerAcc.ID: {},
	}
	for _, id := range []int64{
		treasury[buyerAcc.ValutaOznaka],
		treasury[sellerAcc.ValutaOznaka],
		treasury[listingCcy],
	} {
		if id != 0 {
			idsSet[id] = struct{}{}
		}
	}
	ids := make([]int64, 0, len(idsSet))
	for id := range idsSet {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	var locked []racunForPayment
	if err := gtx.Raw(`
		SELECT ra.id, ra.broj_racuna, ra.id_vlasnika, ra.id_valute,
		       ra.stanje_racuna, ra.rezervisana_sredstva,
		       COALESCE(ra.dnevni_limit, 0) AS dnevni_limit,
		       COALESCE(ra.mesecni_limit, 0) AS mesecni_limit,
		       COALESCE(ra.dnevna_potrosnja, 0) AS dnevna_potrosnja,
		       COALESCE(ra.mesecna_potrosnja, 0) AS mesecna_potrosnja,
		       ra.status, v.oznaka AS valuta_oznaka
		FROM core_banking.racun ra
		JOIN core_banking.valuta v ON v.id = ra.id_valute
		WHERE ra.id IN ? ORDER BY ra.id FOR UPDATE
	`, ids).Scan(&locked).Error; err != nil {
		return fmt.Errorf("lock racuna: %w", err)
	}

	var buyerLocked *racunForPayment
	for i := range locked {
		if locked[i].ID == buyerAcc.ID {
			buyerLocked = &locked[i]
			break
		}
	}
	if buyerLocked == nil || buyerLocked.Status != "AKTIVAN" {
		return domain.ErrOTCAccountNotOwned
	}

	// 5) Provera raspoloživog stanja kupca u njegovoj valuti.
	raspolozivo := buyerLocked.StanjeRacuna - buyerLocked.RezervovanaSredstva
	if raspolozivo < amountBuyerLeg {
		return domain.ErrOTCInsufficientFunds
	}

	now := time.Now().UTC()
	desc := fmt.Sprintf("OTC premija — ponuda #%d", in.OfferID)

	// Helper za update + audit zapis.
	debit := func(accountID int64, amount float64, opis string) error {
		if err := gtx.Exec(`
			UPDATE core_banking.racun
			SET stanje_racuna = stanje_racuna - ?
			WHERE id = ? AND status = 'AKTIVAN'
		`, amount, accountID).Error; err != nil {
			return fmt.Errorf("debit %d: %w", accountID, err)
		}
		return gtx.Create(&transakcijaModel{
			RacunID:          accountID,
			TipTransakcije:   "ISPLATA",
			Iznos:            amount,
			Opis:             opis,
			VremeIzvrsavanja: now,
			Status:           "IZVRSEN",
		}).Error
	}
	credit := func(accountID int64, amount float64, opis string) error {
		if err := gtx.Exec(`
			UPDATE core_banking.racun
			SET stanje_racuna = stanje_racuna + ?
			WHERE id = ? AND status = 'AKTIVAN'
		`, amount, accountID).Error; err != nil {
			return fmt.Errorf("credit %d: %w", accountID, err)
		}
		return gtx.Create(&transakcijaModel{
			RacunID:          accountID,
			TipTransakcije:   "UPLATA",
			Iznos:            amount,
			Opis:             opis,
			VremeIzvrsavanja: now,
			Status:           "IZVRSEN",
		}).Error
	}

	// 6) Izvršenje legova.
	// (a) Skidanje sa kupca u njegovoj valuti.
	if err := debit(buyerAcc.ID, amountBuyerLeg, desc); err != nil {
		return err
	}

	// (b) Ako je buyer u drugoj valuti od listinga: trezor dobija buyer leg, izdaje listing leg.
	if buyerAcc.ValutaOznaka != listingCcy {
		if err := credit(treasury[buyerAcc.ValutaOznaka], amountBuyerLeg, desc+" — konverzija (buyer leg)"); err != nil {
			return err
		}
		if err := debit(treasury[listingCcy], in.AmountInListingCurrency, desc+" — konverzija (listing leg)"); err != nil {
			return err
		}
	}

	// (c) Ako je seller u drugoj valuti od listinga: trezor prima listing leg, izdaje seller leg.
	if sellerAcc.ValutaOznaka != listingCcy {
		if err := credit(treasury[listingCcy], in.AmountInListingCurrency, desc+" — konverzija (listing in)"); err != nil {
			return err
		}
		if err := debit(treasury[sellerAcc.ValutaOznaka], amountSellerLeg, desc+" — konverzija (seller leg)"); err != nil {
			return err
		}
	}

	// Specijalan slučaj: oba računa u listing valuti — direktan put bez treasury duple konverzije.
	// Već smo skinuli sa kupca premium (amountBuyerLeg == premium), pa samo upišemo seller-u.
	if buyerAcc.ValutaOznaka == listingCcy && sellerAcc.ValutaOznaka == listingCcy {
		if err := credit(sellerAcc.ID, amountSellerLeg, desc); err != nil {
			return err
		}
		return nil
	}

	// (d) Knjiženje na prodavca u njegovoj valuti.
	if err := credit(sellerAcc.ID, amountSellerLeg, desc); err != nil {
		return err
	}
	return nil
}

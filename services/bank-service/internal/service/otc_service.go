package service

// otc_service.go — OTCService (Faza 2): pregovaranje, kontraponude i atomski
// flow prihvatanja koji u jednoj transakciji prebacuje status ponude u
// ACCEPTED, kreira OTC ugovor i izvršava transfer premije kroz PaymentService.

import (
	"context"
	"fmt"
	"time"

	"banka-backend/services/bank-service/internal/domain"

	"gorm.io/gorm"
)

type otcService struct {
	db         *gorm.DB
	repo       domain.OTCRepository
	paymentSvc domain.OTCPaymentPort
}

// NewOTCService konstruše OTCService.
//   - db          — root *gorm.DB; transakcije se otvaraju ovde i prosleđuju
//                   repou (WithTx) i PaymentPortu (ExecuteOTCPremiumTransfer).
//   - repo        — OTC repozitorijum (offers, contracts, capacity check).
//   - paymentSvc  — PaymentService u ulozi OTCPaymentPort (premija).
func NewOTCService(db *gorm.DB, repo domain.OTCRepository, paymentSvc domain.OTCPaymentPort) domain.OTCService {
	return &otcService{db: db, repo: repo, paymentSvc: paymentSvc}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func validateOfferFields(amount int32, price, premium float64, settle time.Time) error {
	if amount <= 0 || price <= 0 || premium < 0 {
		return domain.ErrOTCInvalidInput
	}
	if !settle.IsZero() && settle.Before(time.Now().UTC().Truncate(24*time.Hour)) {
		return domain.ErrOTCInvalidInput
	}
	return nil
}

// validateAccountOwnership proverava da li račun pripada datom korisniku.
// Vraća valutu računa za eventualnu naknadnu validaciju.
func (s *otcService) validateAccountOwnership(ctx context.Context, repo domain.OTCRepository, accountID, expectedOwnerID int64) (string, error) {
	owner, currency, err := repo.GetAccountInfo(ctx, accountID)
	if err != nil {
		return "", err
	}
	if owner != expectedOwnerID {
		return "", domain.ErrOTCAccountNotOwned
	}
	return currency, nil
}

// ─── CreateOffer ──────────────────────────────────────────────────────────────

// CreateOffer — kupac inicira ponudu. Mora birati svoj račun za eventualnu
// isplatu premije. Capacity provera se izvršava u istoj transakciji u kojoj
// se i upisuje nova ponuda (sprečava race kad više kupaca šalje ponude
// istovremeno za istog prodavca).
func (s *otcService) CreateOffer(ctx context.Context, in domain.CreateOTCOfferInput) (*domain.OTCOffer, error) {
	if in.BuyerID == in.SellerID {
		return nil, domain.ErrOTCSelfTrade
	}
	if err := validateOfferFields(in.Amount, in.PricePerStock, in.Premium, in.SettlementDate); err != nil {
		return nil, err
	}

	var created *domain.OTCOffer
	txErr := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := s.repo.WithTx(tx)

		// Provera vlasništva računa kupca (i da postoji).
		if _, err := s.validateAccountOwnership(ctx, repo, in.BuyerAccountID, in.BuyerID); err != nil {
			return err
		}

		// Capacity check: prodavac mora imati dovoljno akcija u javnom režimu
		// (minus aktivne PENDING ponude i VALID ugovori).
		avail, err := repo.AvailablePublicShares(ctx, in.SellerID, in.ListingID, 0)
		if err != nil {
			return err
		}
		if avail < in.Amount {
			return domain.ErrOTCInsufficientCapacity
		}

		offer := domain.OTCOffer{
			ListingID:      in.ListingID,
			SellerID:       in.SellerID,
			BuyerID:        in.BuyerID,
			BuyerAccountID: in.BuyerAccountID,
			Amount:         in.Amount,
			PricePerStock:  in.PricePerStock,
			Premium:        in.Premium,
			SettlementDate: in.SettlementDate,
			Status:         domain.OTCOfferPending,
			ModifiedBy:     in.BuyerID, // kupac je inicijator → modified_by = buyer
			// SellerBankID/BuyerBankID — forward-compat; ostavljeno NULL dok ne
			// uvedemo inter-bank trgovinu.
		}
		out, err := repo.CreateOffer(ctx, offer)
		if err != nil {
			return err
		}
		created = out
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	return created, nil
}

// ─── CounterOffer ─────────────────────────────────────────────────────────────

// CounterOffer — druga strana šalje kontraponudu. Pravila:
//   - caller mora biti učesnik (buyer ili seller).
//   - caller ne sme biti onaj ko je poslednji izmenio ponudu (mora biti "drugi red").
//   - status mora biti PENDING.
//   - prodavac može (i u velikom broju slučajeva mora) postaviti svoj račun.
//   - capacity check se ponavlja sa novim Amount-om.
func (s *otcService) CounterOffer(ctx context.Context, in domain.CounterOTCOfferInput) (*domain.OTCOffer, error) {
	if err := validateOfferFields(in.Amount, in.PricePerStock, in.Premium, in.SettlementDate); err != nil {
		return nil, err
	}

	var updated *domain.OTCOffer
	txErr := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := s.repo.WithTx(tx)

		offer, err := repo.GetOfferByIDForUpdate(ctx, in.OfferID)
		if err != nil {
			return err
		}
		if offer.Status != domain.OTCOfferPending {
			return domain.ErrOTCInvalidStatus
		}
		if in.CallerID != offer.BuyerID && in.CallerID != offer.SellerID {
			return domain.ErrOTCNotParticipant
		}
		// Counter može poslati samo strana kojoj je upućena poslednja verzija
		// (modified_by != caller).
		if offer.ModifiedBy == in.CallerID {
			return domain.ErrOTCNotCounterparty
		}

		// Ako je prodavac taj koji odgovara, sme da postavi ili promeni svoj
		// račun za premiju. Kupac NE sme dirati seller_account_id.
		newSellerAcc := offer.SellerAccountID
		if in.CallerID == offer.SellerID && in.SellerAccountID != nil {
			if _, err := s.validateAccountOwnership(ctx, repo, *in.SellerAccountID, offer.SellerID); err != nil {
				return err
			}
			newSellerAcc = in.SellerAccountID
		}

		// Capacity check sa potencijalno novim Amount-om (isključujući trenutnu
		// ponudu da se ne broji dvaput).
		avail, err := repo.AvailablePublicShares(ctx, offer.SellerID, offer.ListingID, offer.ID)
		if err != nil {
			return err
		}
		if avail < in.Amount {
			return domain.ErrOTCInsufficientCapacity
		}

		offer.Amount = in.Amount
		offer.PricePerStock = in.PricePerStock
		offer.Premium = in.Premium
		offer.SettlementDate = in.SettlementDate
		offer.SellerAccountID = newSellerAcc
		offer.ModifiedBy = in.CallerID

		if err := repo.UpdateOfferOnCounter(ctx, *offer); err != nil {
			return err
		}
		// Ponovo pročitaj iz baze da bi vratili svežu LastModified vrednost.
		updated, err = repo.GetOfferByID(ctx, offer.ID)
		return err
	})
	if txErr != nil {
		return nil, txErr
	}
	return updated, nil
}

// ─── AcceptOffer ──────────────────────────────────────────────────────────────

// AcceptOffer — atomsko prihvatanje. U JEDNOJ transakciji:
//   1) lock ponude (FOR UPDATE) i validacija
//   2) capacity check (još jednom — sprečava dupli-spend između status-prelaza)
//   3) status ponude → ACCEPTED, modified_by = caller
//   4) kreiranje OTCContract (VALID)
//   5) transfer premije kroz PaymentService (auditovano, sa FX po potrebi)
//
// Pravila:
//   - caller mora biti učesnik (buyer ili seller).
//   - caller mora biti "primalac poslednje verzije" — modified_by != caller
//     (sprečava self-accept odmah po slanju).
//   - prodavčev račun za premiju mora biti poznat. Prodavac može da ga prosledi
//     pri samom accept-u (in.SellerAccountID), inače ranije postavljen.
func (s *otcService) AcceptOffer(ctx context.Context, in domain.AcceptOTCOfferInput) (*domain.OTCContract, error) {
	var contract *domain.OTCContract

	txErr := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := s.repo.WithTx(tx)

		offer, err := repo.GetOfferByIDForUpdate(ctx, in.OfferID)
		if err != nil {
			return err
		}
		if offer.Status != domain.OTCOfferPending {
			return domain.ErrOTCInvalidStatus
		}
		if in.CallerID != offer.BuyerID && in.CallerID != offer.SellerID {
			return domain.ErrOTCNotParticipant
		}
		// Strogo: ne smeš prihvatiti svoju poslednju izmenu.
		if offer.ModifiedBy == in.CallerID {
			return domain.ErrOTCSelfAccept
		}

		// Razreši seller_account: ili prosleđen u accept (samo prodavac sme),
		// ili već postavljen ranije.
		sellerAcc := offer.SellerAccountID
		if in.SellerAccountID != nil {
			if in.CallerID != offer.SellerID {
				return domain.ErrOTCNotCounterparty
			}
			if _, err := s.validateAccountOwnership(ctx, repo, *in.SellerAccountID, offer.SellerID); err != nil {
				return err
			}
			sellerAcc = in.SellerAccountID
		}
		if sellerAcc == nil {
			return domain.ErrOTCSellerAccountMissing
		}

		// Capacity check (još jednom; sprečava da se "rupa" iskoristi između
		// counter-a i accept-a).
		avail, err := repo.AvailablePublicShares(ctx, offer.SellerID, offer.ListingID, offer.ID)
		if err != nil {
			return err
		}
		if avail < offer.Amount {
			return domain.ErrOTCInsufficientCapacity
		}

		// Valuta listinga (za FX konverziju premije).
		listingCcy, err := repo.GetListingCurrency(ctx, offer.ListingID)
		if err != nil {
			return err
		}

		// 1) Status → ACCEPTED.
		if err := repo.UpdateOfferStatus(ctx, offer.ID, domain.OTCOfferAccepted, in.CallerID); err != nil {
			return err
		}

		// 2) Kreiraj OTC ugovor.
		c := domain.OTCContract{
			OfferID:         offer.ID,
			ListingID:       offer.ListingID,
			SellerID:        offer.SellerID,
			BuyerID:         offer.BuyerID,
			BuyerAccountID:  offer.BuyerAccountID,
			SellerAccountID: *sellerAcc,
			Amount:          offer.Amount,
			StrikePrice:     offer.PricePerStock,
			Premium:         offer.Premium,
			SettlementDate:  offer.SettlementDate,
			Status:          domain.OTCContractValid,
			SellerBankID:    offer.SellerBankID,
			BuyerBankID:     offer.BuyerBankID,
		}
		out, err := repo.CreateContract(ctx, c)
		if err != nil {
			return err
		}
		contract = out

		// 3) Transfer premije kroz PaymentService (audit + FX) — UNUTAR iste tx.
		if err := s.paymentSvc.ExecuteOTCPremiumTransfer(ctx, tx, domain.OTCPremiumTransferInput{
			OfferID:                 offer.ID,
			BuyerAccountID:          offer.BuyerAccountID,
			SellerAccountID:         *sellerAcc,
			AmountInListingCurrency: offer.Premium,
			ListingCurrency:         listingCcy,
			InitiatedByUserID:       in.CallerID,
		}); err != nil {
			return fmt.Errorf("isplata premije: %w", err)
		}
		return nil
	})

	if txErr != nil {
		return nil, txErr
	}
	return contract, nil
}

// ─── DeclineOffer ─────────────────────────────────────────────────────────────

// DeclineOffer — pokriva i "odbijanje od druge strane" (REJECTED) i
// "povlačenje od strane koja je poslednji put modifikovala ponudu" (DEACTIVATED).
//   - Ako caller == modified_by  → DEACTIVATED (povlačenje sopstvene ponude).
//   - Ako caller != modified_by  → REJECTED (odbijanje druge strane).
func (s *otcService) DeclineOffer(ctx context.Context, offerID, callerID int64) (*domain.OTCOffer, error) {
	var updated *domain.OTCOffer
	txErr := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := s.repo.WithTx(tx)

		offer, err := repo.GetOfferByIDForUpdate(ctx, offerID)
		if err != nil {
			return err
		}
		if offer.Status != domain.OTCOfferPending {
			return domain.ErrOTCInvalidStatus
		}
		if callerID != offer.BuyerID && callerID != offer.SellerID {
			return domain.ErrOTCNotParticipant
		}

		newStatus := domain.OTCOfferRejected
		if offer.ModifiedBy == callerID {
			newStatus = domain.OTCOfferDeactivated
		}

		if err := repo.UpdateOfferStatus(ctx, offer.ID, newStatus, callerID); err != nil {
			return err
		}
		offer.Status = newStatus
		offer.ModifiedBy = callerID
		updated = offer
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	return updated, nil
}

// ─── ListOffers / GetOffer ────────────────────────────────────────────────────

func (s *otcService) ListOffers(ctx context.Context, filter domain.ListOTCOffersFilter) ([]domain.OTCOfferListItem, error) {
	return s.repo.ListOffers(ctx, filter)
}

func (s *otcService) GetOffer(ctx context.Context, offerID, callerID int64) (*domain.OTCOfferListItem, error) {
	return s.repo.GetOfferListItem(ctx, offerID, callerID)
}

// ListMarketplace — sve "javno dostupne" akcije za OTC kupovinu (drugi prodavci).
func (s *otcService) ListMarketplace(ctx context.Context, callerID int64) ([]domain.OTCMarketplaceItem, error) {
	return s.repo.ListMarketplace(ctx, callerID)
}

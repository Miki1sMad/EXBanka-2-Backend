package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"banka-backend/services/bank-service/internal/domain"
	"banka-backend/services/bank-service/internal/service"
	"banka-backend/services/bank-service/mocks"
)

func newKarticaService(repo *mocks.MockKarticaRepository, store *mocks.MockCardRequestStore, notif *mocks.MockNotificationSender) domain.KarticaService {
	return service.NewKarticaService(repo, "test-pepper", store, notif)
}

// ─── GetMojeKartice ───────────────────────────────────────────────────────────

func TestGetMojeKartice_Success(t *testing.T) {
	repo := &mocks.MockKarticaRepository{}
	ctx := context.Background()
	want := []domain.KarticaSaRacunom{{BrojRacuna: "123456"}}
	repo.On("GetKarticeKorisnika", ctx, int64(1)).Return(want, nil)

	svc := newKarticaService(repo, &mocks.MockCardRequestStore{}, &mocks.MockNotificationSender{})
	got, err := svc.GetMojeKartice(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestGetMojeKartice_Error(t *testing.T) {
	repo := &mocks.MockKarticaRepository{}
	ctx := context.Background()
	repo.On("GetKarticeKorisnika", ctx, int64(99)).Return(nil, errors.New("db error"))

	svc := newKarticaService(repo, &mocks.MockCardRequestStore{}, &mocks.MockNotificationSender{})
	_, err := svc.GetMojeKartice(ctx, 99)
	assert.Error(t, err)
}

// ─── GetKarticeZaPortalZaposlenih ─────────────────────────────────────────────

func TestGetKarticeZaPortalZaposlenih_Success(t *testing.T) {
	repo := &mocks.MockKarticaRepository{}
	ctx := context.Background()
	want := []domain.KarticaEmployeeRow{{BrojKartice: "4666661234567890"}}
	repo.On("GetKarticeZaRacunBroj", ctx, "123456789012345678").Return(want, nil)

	svc := newKarticaService(repo, &mocks.MockCardRequestStore{}, &mocks.MockNotificationSender{})
	got, err := svc.GetKarticeZaPortalZaposlenih(ctx, "123456789012345678")
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// ─── BlokirajKarticu ──────────────────────────────────────────────────────────

func TestBlokirajKarticu_NotOwner(t *testing.T) {
	repo := &mocks.MockKarticaRepository{}
	ctx := context.Background()
	repo.On("GetKarticaOwnerInfo", ctx, int64(1)).
		Return(&domain.KarticaOwnerInfo{VlasnikID: 99, Status: "AKTIVNA"}, nil)

	svc := newKarticaService(repo, &mocks.MockCardRequestStore{}, &mocks.MockNotificationSender{})
	err := svc.BlokirajKarticu(ctx, 1, 5) // requesterID=5, owner=99
	assert.ErrorIs(t, err, domain.ErrKarticaNijeTvoja)
}

func TestBlokirajKarticu_AlreadyBlocked(t *testing.T) {
	repo := &mocks.MockKarticaRepository{}
	ctx := context.Background()
	repo.On("GetKarticaOwnerInfo", ctx, int64(1)).
		Return(&domain.KarticaOwnerInfo{VlasnikID: 5, Status: "BLOKIRANA"}, nil)

	svc := newKarticaService(repo, &mocks.MockCardRequestStore{}, &mocks.MockNotificationSender{})
	err := svc.BlokirajKarticu(ctx, 1, 5)
	assert.ErrorIs(t, err, domain.ErrKarticaVecBlokirana)
}

func TestBlokirajKarticu_Success(t *testing.T) {
	repo := &mocks.MockKarticaRepository{}
	ctx := context.Background()
	repo.On("GetKarticaOwnerInfo", ctx, int64(1)).
		Return(&domain.KarticaOwnerInfo{VlasnikID: 5, Status: "AKTIVNA"}, nil)
	repo.On("SetKarticaStatus", ctx, int64(1), "BLOKIRANA").Return(nil)

	svc := newKarticaService(repo, &mocks.MockCardRequestStore{}, &mocks.MockNotificationSender{})
	err := svc.BlokirajKarticu(ctx, 1, 5)
	assert.NoError(t, err)
}

// ─── ChangeEmployeeCardStatus ─────────────────────────────────────────────────

func TestChangeEmployeeCardStatus_CardNotFound(t *testing.T) {
	repo := &mocks.MockKarticaRepository{}
	ctx := context.Background()
	repo.On("GetKarticaZaStatusChange", ctx, "4666661234567890").
		Return((*domain.KarticaZaStatusChange)(nil), errors.New("not found"))

	svc := newKarticaService(repo, &mocks.MockCardRequestStore{}, &mocks.MockNotificationSender{})
	_, err := svc.ChangeEmployeeCardStatus(ctx, "4666661234567890", "BLOKIRANA")
	assert.Error(t, err)
}

func TestChangeEmployeeCardStatus_InvalidTransition_DeaktiviranaToAktivna(t *testing.T) {
	repo := &mocks.MockKarticaRepository{}
	ctx := context.Background()
	repo.On("GetKarticaZaStatusChange", ctx, "4666661234567890").
		Return(&domain.KarticaZaStatusChange{TrenutniStatus: "DEAKTIVIRANA"}, nil)

	svc := newKarticaService(repo, &mocks.MockCardRequestStore{}, &mocks.MockNotificationSender{})
	_, err := svc.ChangeEmployeeCardStatus(ctx, "4666661234567890", "AKTIVNA")
	assert.Error(t, err)
}

// ─── CreateKarticaZaVlasnika ──────────────────────────────────────────────────

func TestCreateKarticaZaVlasnika_InvalidCardType(t *testing.T) {
	repo := &mocks.MockKarticaRepository{}
	ctx := context.Background()
	repo.On("GetRacunInfo", ctx, int64(1)).
		Return(&domain.RacunInfo{VrstaRacuna: "LICNI", ValutaOznaka: "RSD", MesecniLimit: 100000}, nil)

	svc := newKarticaService(repo, &mocks.MockCardRequestStore{}, &mocks.MockNotificationSender{})
	_, err := svc.CreateKarticaZaVlasnika(ctx, 1, "INVALID_TYPE")
	assert.ErrorIs(t, err, domain.ErrNepoznatTipKartice)
}

func TestCreateKarticaZaVlasnika_DinaCardNotRSD(t *testing.T) {
	repo := &mocks.MockKarticaRepository{}
	ctx := context.Background()
	repo.On("GetRacunInfo", ctx, int64(1)).
		Return(&domain.RacunInfo{VrstaRacuna: "DEVIZNI", ValutaOznaka: "EUR", MesecniLimit: 5000}, nil)

	svc := newKarticaService(repo, &mocks.MockCardRequestStore{}, &mocks.MockNotificationSender{})
	_, err := svc.CreateKarticaZaVlasnika(ctx, 1, domain.TipKarticaDinaCard)
	assert.ErrorIs(t, err, domain.ErrDinaCardSamoRSD)
}

func TestCreateKarticaZaVlasnika_LicniLimitExceeded(t *testing.T) {
	repo := &mocks.MockKarticaRepository{}
	ctx := context.Background()
	repo.On("GetRacunInfo", ctx, int64(1)).
		Return(&domain.RacunInfo{VrstaRacuna: "LICNI", ValutaOznaka: "RSD", MesecniLimit: 100000}, nil)
	repo.On("CountKarticeZaRacun", ctx, int64(1)).Return(int64(2), nil) // at limit (max 2)

	svc := newKarticaService(repo, &mocks.MockCardRequestStore{}, &mocks.MockNotificationSender{})
	_, err := svc.CreateKarticaZaVlasnika(ctx, 1, domain.TipKarticaVisa)
	assert.ErrorIs(t, err, domain.ErrKarticaLimitPremasen)
}

func TestCreateKarticaZaVlasnika_PoslovniVlasnikAlreadyHasCard(t *testing.T) {
	repo := &mocks.MockKarticaRepository{}
	ctx := context.Background()
	repo.On("GetRacunInfo", ctx, int64(2)).
		Return(&domain.RacunInfo{VrstaRacuna: "POSLOVNI", ValutaOznaka: "RSD", MesecniLimit: 500000}, nil)
	repo.On("HasVlasnikovaKarticaPostoji", ctx, int64(2)).Return(true, nil)

	svc := newKarticaService(repo, &mocks.MockCardRequestStore{}, &mocks.MockNotificationSender{})
	_, err := svc.CreateKarticaZaVlasnika(ctx, 2, domain.TipKarticaMastercard)
	assert.ErrorIs(t, err, domain.ErrKarticaLimitPremasen)
}

func TestCreateKarticaZaVlasnika_Success(t *testing.T) {
	repo := &mocks.MockKarticaRepository{}
	ctx := context.Background()
	repo.On("GetRacunInfo", ctx, int64(3)).
		Return(&domain.RacunInfo{VrstaRacuna: "LICNI", ValutaOznaka: "RSD", MesecniLimit: 100000}, nil)
	repo.On("CountKarticeZaRacun", ctx, int64(3)).Return(int64(0), nil)
	repo.On("CreateKartica", ctx, mock.Anything).Return(int64(10), nil)

	svc := newKarticaService(repo, &mocks.MockCardRequestStore{}, &mocks.MockNotificationSender{})
	id, err := svc.CreateKarticaZaVlasnika(ctx, 3, domain.TipKarticaVisa)
	require.NoError(t, err)
	assert.Equal(t, int64(10), id)
}

// ─── ChangeEmployeeCardStatus — dopunski testovi ──────────────────────────────

func TestChangeEmployeeCardStatus_AktivaToBlokirana(t *testing.T) {
	repo := &mocks.MockKarticaRepository{}
	ctx := context.Background()
	kartica := &domain.KarticaZaStatusChange{ID: 1, TrenutniStatus: "AKTIVNA"}
	repo.On("GetKarticaZaStatusChange", ctx, "4111111111111111").Return(kartica, nil)
	repo.On("SetKarticaStatus", ctx, int64(1), "BLOKIRANA").Return(nil)

	svc := newKarticaService(repo, &mocks.MockCardRequestStore{}, &mocks.MockNotificationSender{})
	got, err := svc.ChangeEmployeeCardStatus(ctx, "4111111111111111", "BLOKIRANA")
	require.NoError(t, err)
	assert.Equal(t, kartica, got)
}

func TestChangeEmployeeCardStatus_AktivaToDeaktivirana(t *testing.T) {
	repo := &mocks.MockKarticaRepository{}
	ctx := context.Background()
	kartica := &domain.KarticaZaStatusChange{ID: 2, TrenutniStatus: "AKTIVNA"}
	repo.On("GetKarticaZaStatusChange", ctx, "4111111111111112").Return(kartica, nil)
	repo.On("SetKarticaStatus", ctx, int64(2), "DEAKTIVIRANA").Return(nil)

	svc := newKarticaService(repo, &mocks.MockCardRequestStore{}, &mocks.MockNotificationSender{})
	got, err := svc.ChangeEmployeeCardStatus(ctx, "4111111111111112", "DEAKTIVIRANA")
	require.NoError(t, err)
	assert.Equal(t, kartica, got)
}

func TestChangeEmployeeCardStatus_AktivaToAktivna(t *testing.T) {
	repo := &mocks.MockKarticaRepository{}
	ctx := context.Background()
	repo.On("GetKarticaZaStatusChange", ctx, "4111111111111113").
		Return(&domain.KarticaZaStatusChange{ID: 3, TrenutniStatus: "AKTIVNA"}, nil)

	svc := newKarticaService(repo, &mocks.MockCardRequestStore{}, &mocks.MockNotificationSender{})
	_, err := svc.ChangeEmployeeCardStatus(ctx, "4111111111111113", "AKTIVNA")
	assert.ErrorIs(t, err, domain.ErrKarticaVecAktivna)
}

func TestChangeEmployeeCardStatus_BlokiranaToAktivna(t *testing.T) {
	repo := &mocks.MockKarticaRepository{}
	ctx := context.Background()
	kartica := &domain.KarticaZaStatusChange{ID: 4, TrenutniStatus: "BLOKIRANA"}
	repo.On("GetKarticaZaStatusChange", ctx, "4111111111111114").Return(kartica, nil)
	repo.On("SetKarticaStatus", ctx, int64(4), "AKTIVNA").Return(nil)

	svc := newKarticaService(repo, &mocks.MockCardRequestStore{}, &mocks.MockNotificationSender{})
	got, err := svc.ChangeEmployeeCardStatus(ctx, "4111111111111114", "AKTIVNA")
	require.NoError(t, err)
	assert.Equal(t, kartica, got)
}

func TestChangeEmployeeCardStatus_BlokiranaToBlokirana(t *testing.T) {
	repo := &mocks.MockKarticaRepository{}
	ctx := context.Background()
	repo.On("GetKarticaZaStatusChange", ctx, "4111111111111115").
		Return(&domain.KarticaZaStatusChange{ID: 5, TrenutniStatus: "BLOKIRANA"}, nil)

	svc := newKarticaService(repo, &mocks.MockCardRequestStore{}, &mocks.MockNotificationSender{})
	_, err := svc.ChangeEmployeeCardStatus(ctx, "4111111111111115", "BLOKIRANA")
	assert.ErrorIs(t, err, domain.ErrKarticaVecBlokirana)
}

func TestChangeEmployeeCardStatus_BlokiranaToDeaktivirana(t *testing.T) {
	repo := &mocks.MockKarticaRepository{}
	ctx := context.Background()
	kartica := &domain.KarticaZaStatusChange{ID: 6, TrenutniStatus: "BLOKIRANA"}
	repo.On("GetKarticaZaStatusChange", ctx, "4111111111111116").Return(kartica, nil)
	repo.On("SetKarticaStatus", ctx, int64(6), "DEAKTIVIRANA").Return(nil)

	svc := newKarticaService(repo, &mocks.MockCardRequestStore{}, &mocks.MockNotificationSender{})
	got, err := svc.ChangeEmployeeCardStatus(ctx, "4111111111111116", "DEAKTIVIRANA")
	require.NoError(t, err)
	assert.Equal(t, kartica, got)
}

func TestChangeEmployeeCardStatus_DeaktiviranaJeZabranjena(t *testing.T) {
	repo := &mocks.MockKarticaRepository{}
	ctx := context.Background()
	repo.On("GetKarticaZaStatusChange", ctx, "4111111111111117").
		Return(&domain.KarticaZaStatusChange{ID: 7, TrenutniStatus: "DEAKTIVIRANA"}, nil)

	svc := newKarticaService(repo, &mocks.MockCardRequestStore{}, &mocks.MockNotificationSender{})
	_, err := svc.ChangeEmployeeCardStatus(ctx, "4111111111111117", "AKTIVNA")
	assert.ErrorIs(t, err, domain.ErrNedozvoljenaPromenaSatusa)
}

func TestChangeEmployeeCardStatus_UnknownStatus(t *testing.T) {
	repo := &mocks.MockKarticaRepository{}
	ctx := context.Background()
	repo.On("GetKarticaZaStatusChange", ctx, "4111111111111118").
		Return(&domain.KarticaZaStatusChange{ID: 8, TrenutniStatus: "NEPOZNAT"}, nil)

	svc := newKarticaService(repo, &mocks.MockCardRequestStore{}, &mocks.MockNotificationSender{})
	_, err := svc.ChangeEmployeeCardStatus(ctx, "4111111111111118", "AKTIVNA")
	assert.ErrorIs(t, err, domain.ErrNedozvoljenaPromenaSatusa)
}

// ─── RequestKartica ───────────────────────────────────────────────────────────

func TestRequestKartica_AccountNotOwned(t *testing.T) {
	repo := &mocks.MockKarticaRepository{}
	ctx := context.Background()
	repo.On("GetRacunVlasnikInfo", ctx, int64(10)).
		Return(&domain.RacunVlasnikInfo{VlasnikID: 99, Status: "AKTIVAN", VrstaRacuna: "LICNI"}, nil)

	svc := newKarticaService(repo, &mocks.MockCardRequestStore{}, &mocks.MockNotificationSender{})
	err := svc.RequestKartica(ctx, domain.RequestKarticaInput{
		RacunID: 10, VlasnikID: 1, TipKartice: domain.TipKarticaVisa, VlasnikEmail: "test@test.com",
	})
	assert.ErrorIs(t, err, domain.ErrRacunNijeTvoj)
}

func TestRequestKartica_AccountNotActive(t *testing.T) {
	repo := &mocks.MockKarticaRepository{}
	ctx := context.Background()
	repo.On("GetRacunVlasnikInfo", ctx, int64(11)).
		Return(&domain.RacunVlasnikInfo{VlasnikID: 1, Status: "NEAKTIVAN", VrstaRacuna: "LICNI"}, nil)

	svc := newKarticaService(repo, &mocks.MockCardRequestStore{}, &mocks.MockNotificationSender{})
	err := svc.RequestKartica(ctx, domain.RequestKarticaInput{
		RacunID: 11, VlasnikID: 1, TipKartice: domain.TipKarticaVisa, VlasnikEmail: "test@test.com",
	})
	assert.ErrorIs(t, err, domain.ErrRacunNijeAktivan)
}

func TestRequestKartica_LicniWithOvlascenoLice(t *testing.T) {
	repo := &mocks.MockKarticaRepository{}
	ctx := context.Background()
	repo.On("GetRacunVlasnikInfo", ctx, int64(12)).
		Return(&domain.RacunVlasnikInfo{VlasnikID: 1, Status: "AKTIVAN", VrstaRacuna: "LICNI"}, nil)

	svc := newKarticaService(repo, &mocks.MockCardRequestStore{}, &mocks.MockNotificationSender{})
	err := svc.RequestKartica(ctx, domain.RequestKarticaInput{
		RacunID:      12,
		VlasnikID:    1,
		TipKartice:   domain.TipKarticaVisa,
		VlasnikEmail: "test@test.com",
		OvlascenoLice: &domain.OvlascenoLiceInput{
			Ime: "Pera", Prezime: "Peric", EmailAdresa: "pera@test.com",
		},
	})
	assert.ErrorIs(t, err, domain.ErrOvlascenoLiceNijeDozvoljeno)
}

func TestRequestKartica_LimitPremasena(t *testing.T) {
	repo := &mocks.MockKarticaRepository{}
	ctx := context.Background()
	repo.On("GetRacunVlasnikInfo", ctx, int64(13)).
		Return(&domain.RacunVlasnikInfo{VlasnikID: 1, Status: "AKTIVAN", VrstaRacuna: "LICNI", MesecniLimit: 100000}, nil)
	repo.On("CountKarticeZaRacun", ctx, int64(13)).Return(int64(2), nil) // at max

	svc := newKarticaService(repo, &mocks.MockCardRequestStore{}, &mocks.MockNotificationSender{})
	err := svc.RequestKartica(ctx, domain.RequestKarticaInput{
		RacunID: 13, VlasnikID: 1, TipKartice: domain.TipKarticaVisa, VlasnikEmail: "test@test.com",
	})
	assert.ErrorIs(t, err, domain.ErrKarticaLimitPremasen)
}

func TestRequestKartica_Success(t *testing.T) {
	repo := &mocks.MockKarticaRepository{}
	store := &mocks.MockCardRequestStore{}
	notif := &mocks.MockNotificationSender{}
	ctx := context.Background()

	repo.On("GetRacunVlasnikInfo", ctx, int64(14)).
		Return(&domain.RacunVlasnikInfo{VlasnikID: 1, Status: "AKTIVAN", VrstaRacuna: "LICNI", MesecniLimit: 100000}, nil)
	repo.On("CountKarticeZaRacun", ctx, int64(14)).Return(int64(0), nil)
	store.On("SaveCardRequest", ctx, int64(1), mock.Anything, mock.Anything).Return(nil)
	notif.On("SendCardOTP", ctx, "korisnik@test.com", mock.Anything).Return(nil)

	svc := newKarticaService(repo, store, notif)
	err := svc.RequestKartica(ctx, domain.RequestKarticaInput{
		RacunID: 14, VlasnikID: 1, TipKartice: domain.TipKarticaVisa, VlasnikEmail: "korisnik@test.com",
	})
	assert.NoError(t, err)
	store.AssertExpectations(t)
	notif.AssertExpectations(t)
}

func TestRequestKartica_OTPSendFails(t *testing.T) {
	repo := &mocks.MockKarticaRepository{}
	store := &mocks.MockCardRequestStore{}
	notif := &mocks.MockNotificationSender{}
	ctx := context.Background()

	repo.On("GetRacunVlasnikInfo", ctx, int64(15)).
		Return(&domain.RacunVlasnikInfo{VlasnikID: 1, Status: "AKTIVAN", VrstaRacuna: "LICNI", MesecniLimit: 100000}, nil)
	repo.On("CountKarticeZaRacun", ctx, int64(15)).Return(int64(0), nil)
	store.On("SaveCardRequest", ctx, int64(1), mock.Anything, mock.Anything).Return(nil)
	notif.On("SendCardOTP", ctx, "fail@test.com", mock.Anything).Return(errors.New("smtp error"))
	store.On("DeleteCardRequest", ctx, int64(1)).Return(nil)

	svc := newKarticaService(repo, store, notif)
	err := svc.RequestKartica(ctx, domain.RequestKarticaInput{
		RacunID: 15, VlasnikID: 1, TipKartice: domain.TipKarticaVisa, VlasnikEmail: "fail@test.com",
	})
	assert.ErrorIs(t, err, domain.ErrNotificationFailed)
}

// ─── ConfirmKartica ───────────────────────────────────────────────────────────

func TestConfirmKartica_NoActiveRequest(t *testing.T) {
	store := &mocks.MockCardRequestStore{}
	ctx := context.Background()
	store.On("GetCardRequest", ctx, int64(1)).Return((*domain.CardRequestState)(nil), domain.ErrCardRequestNotFound)

	svc := newKarticaService(&mocks.MockKarticaRepository{}, store, &mocks.MockNotificationSender{})
	_, err := svc.ConfirmKartica(ctx, domain.ConfirmKarticaInput{VlasnikID: 1, OTPCode: "123456"})
	assert.ErrorIs(t, err, domain.ErrCardRequestNotFound)
}

func TestConfirmKartica_WrongOTP(t *testing.T) {
	store := &mocks.MockCardRequestStore{}
	ctx := context.Background()
	state := &domain.CardRequestState{AccountID: 20, TipKartice: domain.TipKarticaVisa, OTPCode: "999999", Attempts: 0}
	store.On("GetCardRequest", ctx, int64(1)).Return(state, nil)
	store.On("SaveCardRequest", ctx, int64(1), mock.Anything, mock.Anything).Return(nil)

	svc := newKarticaService(&mocks.MockKarticaRepository{}, store, &mocks.MockNotificationSender{})
	_, err := svc.ConfirmKartica(ctx, domain.ConfirmKarticaInput{VlasnikID: 1, OTPCode: "000000"})
	assert.ErrorIs(t, err, domain.ErrOTPInvalid)
}

func TestConfirmKartica_MaxOTPAttempts(t *testing.T) {
	store := &mocks.MockCardRequestStore{}
	ctx := context.Background()
	// Attempts is already 2, one more wrong → max reached
	state := &domain.CardRequestState{AccountID: 21, TipKartice: domain.TipKarticaVisa, OTPCode: "777777", Attempts: 2}
	store.On("GetCardRequest", ctx, int64(2)).Return(state, nil)
	store.On("DeleteCardRequest", ctx, int64(2)).Return(nil)

	svc := newKarticaService(&mocks.MockKarticaRepository{}, store, &mocks.MockNotificationSender{})
	_, err := svc.ConfirmKartica(ctx, domain.ConfirmKarticaInput{VlasnikID: 2, OTPCode: "000000"})
	assert.ErrorIs(t, err, domain.ErrOTPMaxAttempts)
}

func TestConfirmKartica_SuccessLicni(t *testing.T) {
	repo := &mocks.MockKarticaRepository{}
	store := &mocks.MockCardRequestStore{}
	ctx := context.Background()

	state := &domain.CardRequestState{AccountID: 30, TipKartice: domain.TipKarticaVisa, OTPCode: "123456", Attempts: 0}
	store.On("GetCardRequest", ctx, int64(5)).Return(state, nil)
	repo.On("GetRacunInfo", ctx, int64(30)).
		Return(&domain.RacunInfo{VrstaRacuna: "LICNI", ValutaOznaka: "RSD", MesecniLimit: 100000}, nil)
	repo.On("CountKarticeZaRacun", ctx, int64(30)).Return(int64(0), nil)
	repo.On("CreateKartica", ctx, mock.Anything).Return(int64(55), nil)
	store.On("DeleteCardRequest", ctx, int64(5)).Return(nil)

	svc := newKarticaService(repo, store, &mocks.MockNotificationSender{})
	id, err := svc.ConfirmKartica(ctx, domain.ConfirmKarticaInput{VlasnikID: 5, OTPCode: "123456"})
	require.NoError(t, err)
	assert.Equal(t, int64(55), id)
}

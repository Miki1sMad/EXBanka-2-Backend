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

func newPaymentService(rr *mocks.MockPaymentRecipientRepository, pr *mocks.MockPaymentRepository) domain.PaymentService {
	return service.NewPaymentService(rr, pr)
}

// ─── CreateRecipient ──────────────────────────────────────────────────────────

func TestCreateRecipient_Success(t *testing.T) {
	rr := &mocks.MockPaymentRecipientRepository{}
	pr := &mocks.MockPaymentRepository{}
	ctx := context.Background()

	rr.On("Create", ctx, mock.MatchedBy(func(r *domain.PaymentRecipient) bool {
		return r.VlasnikID == 1 && r.Naziv == "Marta" && r.BrojRacuna == "1234567890"
	})).Return(nil)

	svc := newPaymentService(rr, pr)
	got, err := svc.CreateRecipient(ctx, 1, "Marta", "1234567890")
	require.NoError(t, err)
	assert.Equal(t, "Marta", got.Naziv)
	assert.Equal(t, "1234567890", got.BrojRacuna)
	rr.AssertExpectations(t)
}

func TestCreateRecipient_EmptyNaziv(t *testing.T) {
	svc := newPaymentService(&mocks.MockPaymentRecipientRepository{}, &mocks.MockPaymentRepository{})
	_, err := svc.CreateRecipient(context.Background(), 1, "", "1234567890")
	assert.Error(t, err)
}

func TestCreateRecipient_AccountNumberTooShort(t *testing.T) {
	svc := newPaymentService(&mocks.MockPaymentRecipientRepository{}, &mocks.MockPaymentRepository{})
	_, err := svc.CreateRecipient(context.Background(), 1, "Marta", "123456789") // 9 chars
	assert.Error(t, err)
}

func TestCreateRecipient_AccountNumberTooLong(t *testing.T) {
	svc := newPaymentService(&mocks.MockPaymentRecipientRepository{}, &mocks.MockPaymentRepository{})
	_, err := svc.CreateRecipient(context.Background(), 1, "Marta", "1234567890123456789") // 19 chars
	assert.Error(t, err)
}

func TestCreateRecipient_AccountNumberBoundaryValid(t *testing.T) {
	rr := &mocks.MockPaymentRecipientRepository{}
	pr := &mocks.MockPaymentRepository{}
	rr.On("Create", mock.Anything, mock.Anything).Return(nil)
	svc := newPaymentService(rr, pr)

	// exactly 10 chars — minimum valid
	_, err := svc.CreateRecipient(context.Background(), 1, "Marta", "1234567890")
	assert.NoError(t, err)

	// exactly 18 chars — maximum valid
	_, err = svc.CreateRecipient(context.Background(), 1, "Marta", "123456789012345678")
	assert.NoError(t, err)
}

func TestCreateRecipient_RepoError(t *testing.T) {
	rr := &mocks.MockPaymentRecipientRepository{}
	pr := &mocks.MockPaymentRepository{}
	rr.On("Create", mock.Anything, mock.Anything).Return(errors.New("db error"))
	svc := newPaymentService(rr, pr)
	_, err := svc.CreateRecipient(context.Background(), 1, "Marta", "1234567890")
	assert.Error(t, err)
}

// ─── GetRecipients ────────────────────────────────────────────────────────────

func TestGetRecipients(t *testing.T) {
	rr := &mocks.MockPaymentRecipientRepository{}
	pr := &mocks.MockPaymentRepository{}
	ctx := context.Background()
	want := []domain.PaymentRecipient{{ID: 1, Naziv: "Marta"}}
	rr.On("GetByOwner", ctx, int64(5)).Return(want, nil)

	svc := newPaymentService(rr, pr)
	got, err := svc.GetRecipients(ctx, 5)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// ─── UpdateRecipient ──────────────────────────────────────────────────────────

func TestUpdateRecipient_EmptyNaziv(t *testing.T) {
	svc := newPaymentService(&mocks.MockPaymentRecipientRepository{}, &mocks.MockPaymentRepository{})
	_, err := svc.UpdateRecipient(context.Background(), 1, 1, "", "1234567890")
	assert.Error(t, err)
}

func TestUpdateRecipient_AccountTooShort(t *testing.T) {
	svc := newPaymentService(&mocks.MockPaymentRecipientRepository{}, &mocks.MockPaymentRepository{})
	_, err := svc.UpdateRecipient(context.Background(), 1, 1, "Marta", "123456789")
	assert.Error(t, err)
}

func TestUpdateRecipient_Success(t *testing.T) {
	rr := &mocks.MockPaymentRecipientRepository{}
	pr := &mocks.MockPaymentRepository{}
	ctx := context.Background()
	existing := &domain.PaymentRecipient{ID: 1, VlasnikID: 2, Naziv: "Old", BrojRacuna: "1234567890"}
	rr.On("GetByID", ctx, int64(1), int64(2)).Return(existing, nil)
	rr.On("Update", ctx, mock.MatchedBy(func(r *domain.PaymentRecipient) bool {
		return r.Naziv == "New" && r.BrojRacuna == "1234567890123"
	})).Return(nil)

	svc := newPaymentService(rr, pr)
	got, err := svc.UpdateRecipient(ctx, 1, 2, "New", "1234567890123")
	require.NoError(t, err)
	assert.Equal(t, "New", got.Naziv)
}

// ─── DeleteRecipient ──────────────────────────────────────────────────────────

func TestDeleteRecipient(t *testing.T) {
	rr := &mocks.MockPaymentRecipientRepository{}
	pr := &mocks.MockPaymentRepository{}
	ctx := context.Background()
	rr.On("Delete", ctx, int64(1), int64(2)).Return(nil)

	svc := newPaymentService(rr, pr)
	err := svc.DeleteRecipient(ctx, 1, 2)
	assert.NoError(t, err)
}

// ─── validateSifraPlacanja (tested via CreatePaymentIntent) ──────────────────

func TestCreatePaymentIntent_InvalidSifraPlacanja(t *testing.T) {
	tests := []struct {
		name  string
		sifra string
	}{
		{"empty", ""},
		{"too short", "20"},
		{"too long", "2001"},
		{"non-digit", "2A1"},
		{"starts with 1", "100"},
		{"starts with 3", "300"},
	}
	svc := newPaymentService(&mocks.MockPaymentRecipientRepository{}, &mocks.MockPaymentRepository{})
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := svc.CreatePaymentIntent(context.Background(), domain.CreatePaymentIntentInput{
				SifraPlacanja:      tc.sifra,
				NazivPrimaoca:      "Marta",
				BrojRacunaPrimaoca: "1234567890",
				Iznos:              100,
				IdempotencyKey:     "key-1",
			})
			assert.ErrorIs(t, err, domain.ErrInvalidPaymentCode, "sifra=%q", tc.sifra)
		})
	}
}

func TestCreatePaymentIntent_ValidSifra(t *testing.T) {
	rr := &mocks.MockPaymentRecipientRepository{}
	pr := &mocks.MockPaymentRepository{}
	ctx := context.Background()
	intent := &domain.PaymentIntent{ID: 1}
	pr.On("CreateIntent", ctx, mock.Anything).Return(intent, int64(10), nil)

	svc := newPaymentService(rr, pr)
	got, actionID, err := svc.CreatePaymentIntent(ctx, domain.CreatePaymentIntentInput{
		SifraPlacanja:      "289",
		NazivPrimaoca:      "Marta",
		BrojRacunaPrimaoca: "1234567890",
		Iznos:              500,
		IdempotencyKey:     "key-valid",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), got.ID)
	assert.Equal(t, int64(10), actionID)
}

func TestCreatePaymentIntent_EmptyNazivPrimaoca(t *testing.T) {
	svc := newPaymentService(&mocks.MockPaymentRecipientRepository{}, &mocks.MockPaymentRepository{})
	_, _, err := svc.CreatePaymentIntent(context.Background(), domain.CreatePaymentIntentInput{
		SifraPlacanja:  "289",
		Iznos:          100,
		IdempotencyKey: "key-1",
	})
	assert.Error(t, err)
}

func TestCreatePaymentIntent_EmptyBrojRacuna(t *testing.T) {
	svc := newPaymentService(&mocks.MockPaymentRecipientRepository{}, &mocks.MockPaymentRepository{})
	_, _, err := svc.CreatePaymentIntent(context.Background(), domain.CreatePaymentIntentInput{
		SifraPlacanja:  "289",
		NazivPrimaoca:  "Marta",
		Iznos:          100,
		IdempotencyKey: "key-1",
	})
	assert.Error(t, err)
}

func TestCreatePaymentIntent_ZeroAmount(t *testing.T) {
	svc := newPaymentService(&mocks.MockPaymentRecipientRepository{}, &mocks.MockPaymentRepository{})
	_, _, err := svc.CreatePaymentIntent(context.Background(), domain.CreatePaymentIntentInput{
		SifraPlacanja:      "289",
		NazivPrimaoca:      "Marta",
		BrojRacunaPrimaoca: "1234567890",
		Iznos:              0,
		IdempotencyKey:     "key-1",
	})
	assert.Error(t, err)
}

func TestCreatePaymentIntent_NegativeAmount(t *testing.T) {
	svc := newPaymentService(&mocks.MockPaymentRecipientRepository{}, &mocks.MockPaymentRepository{})
	_, _, err := svc.CreatePaymentIntent(context.Background(), domain.CreatePaymentIntentInput{
		SifraPlacanja:      "289",
		NazivPrimaoca:      "Marta",
		BrojRacunaPrimaoca: "1234567890",
		Iznos:              -100,
		IdempotencyKey:     "key-1",
	})
	assert.Error(t, err)
}

func TestCreatePaymentIntent_EmptyIdempotencyKey(t *testing.T) {
	svc := newPaymentService(&mocks.MockPaymentRecipientRepository{}, &mocks.MockPaymentRepository{})
	_, _, err := svc.CreatePaymentIntent(context.Background(), domain.CreatePaymentIntentInput{
		SifraPlacanja:      "289",
		NazivPrimaoca:      "Marta",
		BrojRacunaPrimaoca: "1234567890",
		Iznos:              100,
	})
	assert.Error(t, err)
}

// ─── CreateTransferIntent ─────────────────────────────────────────────────────

func TestCreateTransferIntent_SameAccount(t *testing.T) {
	svc := newPaymentService(&mocks.MockPaymentRecipientRepository{}, &mocks.MockPaymentRepository{})
	_, _, err := svc.CreateTransferIntent(context.Background(), domain.CreateTransferIntentInput{
		RacunPlatioceID: 1,
		RacunPrimaocaID: 1, // same
		Iznos:           100,
		IdempotencyKey:  "key-1",
	})
	assert.ErrorIs(t, err, domain.ErrSameAccount)
}

func TestCreateTransferIntent_ZeroAmount(t *testing.T) {
	svc := newPaymentService(&mocks.MockPaymentRecipientRepository{}, &mocks.MockPaymentRepository{})
	_, _, err := svc.CreateTransferIntent(context.Background(), domain.CreateTransferIntentInput{
		RacunPlatioceID: 1,
		RacunPrimaocaID: 2,
		Iznos:           0,
		IdempotencyKey:  "key-1",
	})
	assert.Error(t, err)
}

func TestCreateTransferIntent_EmptyIdempotencyKey(t *testing.T) {
	svc := newPaymentService(&mocks.MockPaymentRecipientRepository{}, &mocks.MockPaymentRepository{})
	_, _, err := svc.CreateTransferIntent(context.Background(), domain.CreateTransferIntentInput{
		RacunPlatioceID: 1,
		RacunPrimaocaID: 2,
		Iznos:           100,
	})
	assert.Error(t, err)
}

func TestCreateTransferIntent_Success(t *testing.T) {
	rr := &mocks.MockPaymentRecipientRepository{}
	pr := &mocks.MockPaymentRepository{}
	ctx := context.Background()
	intent := &domain.PaymentIntent{ID: 5}
	pr.On("CreateTransferIntent", ctx, mock.Anything).Return(intent, int64(20), nil)

	svc := newPaymentService(rr, pr)
	got, actionID, err := svc.CreateTransferIntent(ctx, domain.CreateTransferIntentInput{
		RacunPlatioceID: 1,
		RacunPrimaocaID: 2,
		Iznos:           1000,
		IdempotencyKey:  "transfer-key",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(5), got.ID)
	assert.Equal(t, int64(20), actionID)
}

// ─── VerifyAndExecute ─────────────────────────────────────────────────────────

func TestVerifyAndExecute_EmptyCode(t *testing.T) {
	svc := newPaymentService(&mocks.MockPaymentRecipientRepository{}, &mocks.MockPaymentRepository{})
	_, err := svc.VerifyAndExecute(context.Background(), domain.VerifyPaymentInput{IntentID: 1, Code: ""})
	assert.Error(t, err)
}

func TestVerifyAndExecute_Success(t *testing.T) {
	rr := &mocks.MockPaymentRecipientRepository{}
	pr := &mocks.MockPaymentRepository{}
	ctx := context.Background()
	intent := &domain.PaymentIntent{ID: 1}
	input := domain.VerifyPaymentInput{IntentID: 1, Code: "123456", UserID: 2}
	pr.On("VerifyAndExecute", ctx, input).Return(intent, nil)

	svc := newPaymentService(rr, pr)
	got, err := svc.VerifyAndExecute(ctx, input)
	require.NoError(t, err)
	assert.Equal(t, int64(1), got.ID)
}

// ─── GetPaymentHistory / GetPaymentDetail ─────────────────────────────────────

func TestGetPaymentHistory(t *testing.T) {
	rr := &mocks.MockPaymentRecipientRepository{}
	pr := &mocks.MockPaymentRepository{}
	ctx := context.Background()
	filter := domain.PaymentHistoryFilter{}
	want := []domain.PaymentIntent{{ID: 1}, {ID: 2}}
	pr.On("GetHistory", ctx, int64(3), filter).Return(want, nil)

	svc := newPaymentService(rr, pr)
	got, err := svc.GetPaymentHistory(ctx, 3, filter)
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

func TestGetPaymentDetail_Error(t *testing.T) {
	rr := &mocks.MockPaymentRecipientRepository{}
	pr := &mocks.MockPaymentRepository{}
	ctx := context.Background()
	pr.On("GetByID", ctx, int64(1), int64(2)).Return((*domain.PaymentIntent)(nil), errors.New("not found"))

	svc := newPaymentService(rr, pr)
	_, err := svc.GetPaymentDetail(ctx, 1, 2)
	assert.Error(t, err)
}

// ─── ExecuteOTCPremiumTransfer ────────────────────────────────────────────────

func TestExecuteOTCPremiumTransfer_Success(t *testing.T) {
	rr := &mocks.MockPaymentRecipientRepository{}
	pr := &mocks.MockPaymentRepository{}
	ctx := context.Background()
	input := domain.OTCPremiumTransferInput{OfferID: 1, BuyerAccountID: 44, SellerAccountID: 55, AmountInListingCurrency: 100.0, ListingCurrency: "USD", InitiatedByUserID: 1}

	pr.On("ExecuteOTCPremiumTransfer", ctx, nil, input).Return(nil)

	svc := newPaymentService(rr, pr)
	err := svc.ExecuteOTCPremiumTransfer(ctx, nil, input)
	require.NoError(t, err)
	pr.AssertExpectations(t)
}

func TestExecuteOTCPremiumTransfer_Error(t *testing.T) {
	rr := &mocks.MockPaymentRecipientRepository{}
	pr := &mocks.MockPaymentRepository{}
	ctx := context.Background()
	input := domain.OTCPremiumTransferInput{OfferID: 2, BuyerAccountID: 44, SellerAccountID: 55, AmountInListingCurrency: 50.0, ListingCurrency: "RSD", InitiatedByUserID: 2}

	pr.On("ExecuteOTCPremiumTransfer", ctx, nil, input).Return(errors.New("transfer failed"))

	svc := newPaymentService(rr, pr)
	err := svc.ExecuteOTCPremiumTransfer(ctx, nil, input)
	require.Error(t, err)
	pr.AssertExpectations(t)
}

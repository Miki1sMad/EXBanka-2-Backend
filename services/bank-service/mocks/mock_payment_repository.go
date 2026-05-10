package mocks

import (
	"context"

	"github.com/stretchr/testify/mock"

	"banka-backend/services/bank-service/internal/domain"
)

// MockPaymentRecipientRepository is a testify mock for domain.PaymentRecipientRepository.
type MockPaymentRecipientRepository struct {
	mock.Mock
}

func (m *MockPaymentRecipientRepository) Create(ctx context.Context, r *domain.PaymentRecipient) error {
	args := m.Called(ctx, r)
	return args.Error(0)
}

func (m *MockPaymentRecipientRepository) GetByID(ctx context.Context, id, vlasnikID int64) (*domain.PaymentRecipient, error) {
	args := m.Called(ctx, id, vlasnikID)
	v, _ := args.Get(0).(*domain.PaymentRecipient)
	return v, args.Error(1)
}

func (m *MockPaymentRecipientRepository) GetByOwner(ctx context.Context, vlasnikID int64) ([]domain.PaymentRecipient, error) {
	args := m.Called(ctx, vlasnikID)
	v, _ := args.Get(0).([]domain.PaymentRecipient)
	return v, args.Error(1)
}

func (m *MockPaymentRecipientRepository) Update(ctx context.Context, r *domain.PaymentRecipient) error {
	args := m.Called(ctx, r)
	return args.Error(0)
}

func (m *MockPaymentRecipientRepository) Delete(ctx context.Context, id, vlasnikID int64) error {
	args := m.Called(ctx, id, vlasnikID)
	return args.Error(0)
}

func (m *MockPaymentRecipientRepository) ExistsByOwnerAndAccount(ctx context.Context, vlasnikID int64, brojRacuna string) (bool, error) {
	args := m.Called(ctx, vlasnikID, brojRacuna)
	return args.Bool(0), args.Error(1)
}

// MockPaymentRepository is a testify mock for domain.PaymentRepository.
type MockPaymentRepository struct {
	mock.Mock
}

func (m *MockPaymentRepository) CreateIntent(ctx context.Context, input domain.CreatePaymentIntentInput) (*domain.PaymentIntent, int64, error) {
	args := m.Called(ctx, input)
	v, _ := args.Get(0).(*domain.PaymentIntent)
	return v, args.Get(1).(int64), args.Error(2)
}

func (m *MockPaymentRepository) CreateTransferIntent(ctx context.Context, input domain.CreateTransferIntentInput) (*domain.PaymentIntent, int64, error) {
	args := m.Called(ctx, input)
	v, _ := args.Get(0).(*domain.PaymentIntent)
	return v, args.Get(1).(int64), args.Error(2)
}

func (m *MockPaymentRepository) VerifyAndExecute(ctx context.Context, input domain.VerifyPaymentInput) (*domain.PaymentIntent, error) {
	args := m.Called(ctx, input)
	v, _ := args.Get(0).(*domain.PaymentIntent)
	return v, args.Error(1)
}

func (m *MockPaymentRepository) GetByID(ctx context.Context, id, userID int64) (*domain.PaymentIntent, error) {
	args := m.Called(ctx, id, userID)
	v, _ := args.Get(0).(*domain.PaymentIntent)
	return v, args.Error(1)
}

func (m *MockPaymentRepository) GetHistory(ctx context.Context, userID int64, filter domain.PaymentHistoryFilter) ([]domain.PaymentIntent, error) {
	args := m.Called(ctx, userID, filter)
	v, _ := args.Get(0).([]domain.PaymentIntent)
	return v, args.Error(1)
}

func (m *MockPaymentRepository) ExecuteOTCPremiumTransfer(ctx context.Context, tx interface{}, in domain.OTCPremiumTransferInput) error {
	args := m.Called(ctx, tx, in)
	return args.Error(0)
}

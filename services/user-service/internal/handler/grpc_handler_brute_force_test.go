package handler_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	pb "banka-backend/proto/user"
	sqlc "banka-backend/services/user-service/internal/database/sqlc"
	utils "banka-backend/services/user-service/internal/utils"
	"banka-backend/services/user-service/mocks"

	"google.golang.org/grpc/codes"
)

// ─── Brute-force login protection ────────────────────────────────────────────

func TestLogin_AccountCurrentlyLocked(t *testing.T) {
	q := &mocks.MockQuerier{}
	hash := bcryptHash("ValidPass1!")
	q.On("GetUserByEmail", context.Background(), "user@test.com").Return(sqlc.User{
		ID:           1,
		Email:        "user@test.com",
		PasswordHash: hash,
		IsActive:     true,
		AccountLockedUntil: sql.NullTime{
			Valid: true,
			Time:  time.Now().Add(5 * time.Minute),
		},
	}, nil)

	h := newHandler(q, &mocks.MockEmailPublisher{})
	resp, err := h.Login(context.Background(), &pb.LoginRequest{
		Email:    "user@test.com",
		Password: "ValidPass1!",
	})

	assert.Nil(t, resp)
	assert.Equal(t, codes.ResourceExhausted, grpcCode(err))
	q.AssertExpectations(t)
}

func TestLogin_ExpiredLock_ResetsCounterAndProceeds(t *testing.T) {
	q := &mocks.MockQuerier{}
	hash := bcryptHash("ValidPass1!")
	q.On("GetUserByEmail", context.Background(), "user@test.com").Return(sqlc.User{
		ID:           1,
		Email:        "user@test.com",
		PasswordHash: hash,
		IsActive:     true,
		UserType:     "EMPLOYEE",
		AccountLockedUntil: sql.NullTime{
			Valid: true,
			Time:  time.Now().Add(-1 * time.Minute), // expired
		},
	}, nil)
	q.On("ResetLoginAttempts", context.Background(), int64(1)).Return(nil)
	q.On("GetUserPermissions", context.Background(), int64(1)).Return([]string{}, nil)

	h := newHandler(q, &mocks.MockEmailPublisher{})
	resp, err := h.Login(context.Background(), &pb.LoginRequest{
		Email:    "user@test.com",
		Password: "ValidPass1!",
	})

	require.NoError(t, err)
	assert.NotEmpty(t, resp.AccessToken)
	q.AssertExpectations(t)
}

func TestLogin_FifthFailedAttempt_AccountJustLocked_SendsEmail(t *testing.T) {
	q := &mocks.MockQuerier{}
	pub := &mocks.MockEmailPublisher{}
	q.On("GetUserByEmail", context.Background(), "user@test.com").Return(sqlc.User{
		ID:           1,
		Email:        "user@test.com",
		PasswordHash: bcryptHash("CorrectPass1!"),
		IsActive:     true,
	}, nil)
	q.On("RecordFailedLogin", context.Background(), int64(1)).Return(sqlc.RecordFailedLoginRow{
		FailedLoginAttempts: 5,
		AccountLockedUntil: sql.NullTime{
			Valid: true,
			Time:  time.Now().Add(10 * time.Minute),
		},
	}, nil)
	pub.On("Publish", mock.MatchedBy(func(e utils.EmailEvent) bool {
		return e.Type == "ACCOUNT_LOCKED" && e.Email == "user@test.com" && e.Token != ""
	})).Return(nil)

	h := newHandler(q, pub)
	_, err := h.Login(context.Background(), &pb.LoginRequest{
		Email:    "user@test.com",
		Password: "WrongPass!",
	})

	assert.Equal(t, codes.Unauthenticated, grpcCode(err))
	q.AssertExpectations(t)
	pub.AssertExpectations(t)

	h2 := newHandler(q, &mocks.MockEmailPublisher{})
	_ = h2
}

func TestLogin_SuccessWithPendingAttempts_ResetsCounter(t *testing.T) {
	q := &mocks.MockQuerier{}
	hash := bcryptHash("ValidPass1!")
	q.On("GetUserByEmail", context.Background(), "user@test.com").Return(sqlc.User{
		ID:                  1,
		Email:               "user@test.com",
		PasswordHash:        hash,
		IsActive:            true,
		UserType:            "EMPLOYEE",
		FailedLoginAttempts: 3,
	}, nil)
	q.On("ResetLoginAttempts", context.Background(), int64(1)).Return(nil)
	q.On("GetUserPermissions", context.Background(), int64(1)).Return([]string{}, nil)

	h := newHandler(q, &mocks.MockEmailPublisher{})
	resp, err := h.Login(context.Background(), &pb.LoginRequest{
		Email:    "user@test.com",
		Password: "ValidPass1!",
	})

	require.NoError(t, err)
	assert.NotEmpty(t, resp.AccessToken)
	q.AssertExpectations(t)
}

func TestLogin_WrongPassword_NotLockedYet_NoEmail(t *testing.T) {
	q := &mocks.MockQuerier{}
	pub := &mocks.MockEmailPublisher{}
	q.On("GetUserByEmail", context.Background(), "user@test.com").Return(sqlc.User{
		ID:           1,
		Email:        "user@test.com",
		PasswordHash: bcryptHash("CorrectPass1!"),
		IsActive:     true,
	}, nil)
	q.On("RecordFailedLogin", context.Background(), int64(1)).Return(sqlc.RecordFailedLoginRow{
		FailedLoginAttempts: 2,
		AccountLockedUntil:  sql.NullTime{Valid: false},
	}, nil)

	h := newHandler(q, pub)
	_, err := h.Login(context.Background(), &pb.LoginRequest{
		Email:    "user@test.com",
		Password: "WrongPass!",
	})

	assert.Equal(t, codes.Unauthenticated, grpcCode(err))
	pub.AssertNotCalled(t, "Publish", mock.Anything)
	q.AssertExpectations(t)
}

func TestLogin_LockedUntil_NotValid_ProceedsNormally(t *testing.T) {
	q := &mocks.MockQuerier{}
	hash := bcryptHash("ValidPass1!")
	q.On("GetUserByEmail", context.Background(), "user@test.com").Return(sqlc.User{
		ID:                 1,
		Email:              "user@test.com",
		PasswordHash:       hash,
		IsActive:           true,
		UserType:           "EMPLOYEE",
		AccountLockedUntil: sql.NullTime{Valid: false},
	}, nil)
	q.On("GetUserPermissions", context.Background(), int64(1)).Return([]string{}, nil)

	h := newHandler(q, &mocks.MockEmailPublisher{})
	resp, err := h.Login(context.Background(), &pb.LoginRequest{
		Email:    "user@test.com",
		Password: "ValidPass1!",
	})

	require.NoError(t, err)
	assert.NotEmpty(t, resp.AccessToken)
}

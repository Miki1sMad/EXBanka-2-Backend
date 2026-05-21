// Package domain defines business entities and interfaces for notification-service.
// Clean Architecture: innermost layer — no external dependencies.
package domain

import "fmt"

// EmailEvent is the message payload consumed from the email_notifications queue.
// Mirrors the struct published by user-service (JSON tags: type, email, token).
type EmailEvent struct {
	Type  string `json:"type"`  // "ACTIVATION" | "RESET" | "ACTIVATION_SUCCESS" | "PASSWORD_RESET_SUCCESS" | "ACCOUNT_CREATED" | "CARD_OTP" | "CARD_STATUS_CHANGED" | "KREIRANA_KARTICA" | "KREDIT_PODNET" | "KREDIT_RATA_UPOZORENJE" | "ACCOUNT_LOCKED" | "PAYMENT_EXECUTED" | "TRANSFER_EXECUTED" | "LIMIT_CHANGED" | "KREDIT_ODOBREN"
	Email string `json:"email"` // recipient address
	Token string `json:"token"` // JWT for action links; OTP code for CARD_OTP type; card status for CARD_STATUS_CHANGED
}

// NotificationService defines the application use-case contract.
type NotificationService interface {
	SendEmail(event EmailEvent) error
}

// ErrUnknownEventType is returned when SendEmail receives an unrecognized event type.
// Callers (e.g. gRPC handler) can use errors.As to distinguish it from infrastructure errors.
type ErrUnknownEventType struct {
	Type string
}

func (e ErrUnknownEventType) Error() string {
	return fmt.Sprintf("unknown email event type: %s", e.Type)
}

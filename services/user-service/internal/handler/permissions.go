package handler

import "context"

// Permission codes — must stay in sync with the permissions table seed data.
// These values are embedded in JWT access tokens and consumed by downstream
// services (e.g. bank-service actuary_consumer checks for SUPERVISOR / AGENT).
const (
	PermAdmin      = "ADMIN_PERMISSION"
	PermSupervisor = "SUPERVISOR"
	PermAgent      = "AGENT"
)

// BankActuaryClient syncs actuary_info records in bank-service when an
// employee gains or loses SUPERVISOR / AGENT permissions.
// The concrete implementation lives in transport.BankServiceClient.
// Inject nil to disable bank-service sync (e.g. when BANK_SERVICE_ADDR is unset).
type BankActuaryClient interface {
	CreateActuary(ctx context.Context, employeeID int64, actuaryType string, bearerToken string) error
	DeleteActuary(ctx context.Context, employeeID int64, oldActuaryType string, bearerToken string) error
}

// appendIfMissing returns codes with val appended only when val is not already
// present in the slice. Used to build the effective permission list without
// duplicates when auto-deriving permissions from the employee's position.
func appendIfMissing(codes []string, val string) []string {
	for _, c := range codes {
		if c == val {
			return codes
		}
	}
	return append(codes, val)
}

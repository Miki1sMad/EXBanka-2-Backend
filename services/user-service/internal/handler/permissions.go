package handler

// Permission codes — must stay in sync with the permissions table seed data.
// These values are embedded in JWT access tokens and consumed by downstream
// services (e.g. bank-service actuary_consumer checks for SUPERVISOR / AGENT).
const (
	PermAdmin      = "ADMIN_PERMISSION"
	PermSupervisor = "SUPERVISOR"
	PermAgent      = "AGENT"
)

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

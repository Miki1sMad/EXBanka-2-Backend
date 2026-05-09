package handler

// service_token.go — kratkotrajan JWT token koji bank-service koristi za
// inter-service pozive ka user-service gRPC API-ju.
//
// Problem: user-service.GetClientByID zahteva EMPLOYEE ili ADMIN tip,
// a GetEmployeeByID zahteva ADMIN/MANAGE_USERS/SUPERVISOR.
// Kada klijent (CLIENT) poziva OTC endpoint, bank-service prosleđuje
// klijentov token — oba gRPC poziva pada sa PermissionDenied.
//
// Rešenje: bank-service generiše sopstveni servisni token (EMPLOYEE+SUPERVISOR)
// potpisanim istim JWT tajnim ključem koji dele svi servisi. Ovaj token se
// koristi SAMO za interne name-resolution pozive, nikad se ne vraća korisniku.

import (
	auth "banka-backend/shared/auth"
)

// serviceToken generiše kratkotrajan (15 min) EMPLOYEE+SUPERVISOR JWT token
// za inter-service gRPC pozive ka user-service-u.
// Vraća prazan string ako potpisivanje ne uspe.
func serviceToken(jwtSecret string) string {
	token, err := auth.GenerateAccessToken(
		"0",                     // sub=0 označava service account
		"bank-service@internal", // email nije bitan za user-service auth
		"EMPLOYEE",
		[]string{"SUPERVISOR"},
		jwtSecret,
	)
	if err != nil {
		return ""
	}
	return token
}

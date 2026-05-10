// Package transport — HTTP client for calling bank-service internal endpoints.
package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// BankServiceClient sends HTTP requests to bank-service's internal API.
// The caller is responsible for forwarding a valid admin Bearer token.
type BankServiceClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewBankServiceClient constructs a client targeting the given base URL.
// Example: "http://bank-service:8080"
func NewBankServiceClient(baseURL string) *BankServiceClient {
	return &BankServiceClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// CreateActuary calls POST /bank/internal/actuary/ to provision an actuary_info record.
// bearerToken must be a valid admin access token.
func (c *BankServiceClient) CreateActuary(ctx context.Context, employeeID int64, actuaryType string, bearerToken string) error {
	body, _ := json.Marshal(map[string]any{
		"employee_id":  employeeID,
		"actuary_type": actuaryType,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/bank/internal/actuary/", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build create-actuary request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearerToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("create actuary HTTP call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("create actuary: bank-service returned %d", resp.StatusCode)
	}
	return nil
}

// DeleteActuary calls DELETE /bank/internal/actuary/{employeeID} to remove an actuary_info record.
// oldActuaryType ("SUPERVISOR" or "AGENT") is forwarded as a query param so bank-service can
// trigger fund transfer even when the actuary_info row is missing (e.g. seeded users).
func (c *BankServiceClient) DeleteActuary(ctx context.Context, employeeID int64, oldActuaryType string, bearerToken string) error {
	url := fmt.Sprintf("%s/bank/internal/actuary/%d?actuaryType=%s", c.baseURL, employeeID, oldActuaryType)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("build delete-actuary request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearerToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete actuary HTTP call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("delete actuary: bank-service returned %d", resp.StatusCode)
	}
	return nil
}

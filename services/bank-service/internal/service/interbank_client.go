// Package service — interbank_client.go
//
// HTTP klijent za komunikaciju sa drugom bankom po si-tx-proto:
//
//	POST /interbank
//	GET  /public-stock
//	POST /negotiations
//	PUT  /negotiations/{routing}/{id}
//	GET  /negotiations/{routing}/{id}
//	DELETE /negotiations/{routing}/{id}
//	GET  /negotiations/{routing}/{id}/accept
//
// Bez mock-a, bez stub-ova, bez fake success-a. Ako URL druge banke nije
// podešen, klijent vraća domain.ErrInterbankPeerNotConfigured.
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"banka-backend/services/bank-service/internal/domain"
)

// interbankClient implementira domain.InterbankClient.
type interbankClient struct {
	httpClient  *http.Client
	peerBaseURL string
	peerAPIKey  string
	peerRouting int64
	ourRouting  int64
}

// InterbankClientConfig — strukturisani config za konstruktor.
type InterbankClientConfig struct {
	PeerBaseURL        string
	PeerAPIKey         string
	PeerRoutingNumber  int64
	OurRoutingNumber   int64
	HTTPTimeoutSeconds int
}

// NewInterbankClient kreira HTTP klijent za drugu banku.
//
// Ako PeerBaseURL nije postavljen, sve metode klijenta vraćaju
// domain.ErrInterbankPeerNotConfigured. Klijent svakako biva instanciran
// (servis se uvek spreman wire-uje), ali stvarni pozivi nisu mogući.
func NewInterbankClient(cfg InterbankClientConfig) domain.InterbankClient {
	timeout := time.Duration(cfg.HTTPTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &interbankClient{
		httpClient:  &http.Client{Timeout: timeout},
		peerBaseURL: strings.TrimRight(cfg.PeerBaseURL, "/"),
		peerAPIKey:  cfg.PeerAPIKey,
		peerRouting: cfg.PeerRoutingNumber,
		ourRouting:  cfg.OurRoutingNumber,
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func (c *interbankClient) ensureConfigured() error {
	if c.peerBaseURL == "" {
		return domain.ErrInterbankPeerNotConfigured
	}
	return nil
}

func (c *interbankClient) doRequest(ctx context.Context, method, path string, body interface{}) (*http.Response, []byte, error) {
	if err := c.ensureConfigured(); err != nil {
		return nil, nil, err
	}
	endpoint := c.peerBaseURL + path

	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, nil, fmt.Errorf("interbank: marshal body: %w", err)
		}
		rdr = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, rdr)
	if err != nil {
		return nil, nil, fmt.Errorf("interbank: kreiranje zahteva: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "application/json")
	if c.peerAPIKey != "" {
		req.Header.Set("X-Api-Key", c.peerAPIKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("interbank: HTTP poziv: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return resp, respBody, nil
}

// ─── POST /interbank ─────────────────────────────────────────────────────────

func (c *interbankClient) SendMessage(ctx context.Context, msg domain.InterbankMessage) (int, []byte, error) {
	resp, body, err := c.doRequest(ctx, http.MethodPost, "/interbank", msg)
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, body, nil
}

// ─── GET /public-stock ───────────────────────────────────────────────────────

func (c *interbankClient) GetPublicStock(ctx context.Context) ([]domain.PublicStock, error) {
	resp, body, err := c.doRequest(ctx, http.MethodGet, "/public-stock", nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("interbank GET /public-stock: status %d, body %s", resp.StatusCode, string(body))
	}
	var out []domain.PublicStock
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("interbank: parse public-stock: %w", err)
	}
	return out, nil
}

// ─── POST /negotiations ──────────────────────────────────────────────────────

func (c *interbankClient) CreateNegotiation(ctx context.Context, offer domain.OtcOffer) (*domain.ForeignBankId, error) {
	resp, body, err := c.doRequest(ctx, http.MethodPost, "/negotiations", offer)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("interbank POST /negotiations: status %d, body %s", resp.StatusCode, string(body))
	}
	var id domain.ForeignBankId
	if err := json.Unmarshal(body, &id); err != nil {
		return nil, fmt.Errorf("interbank: parse negotiation id: %w", err)
	}
	return &id, nil
}

// ─── PUT /negotiations/{routing}/{id} ────────────────────────────────────────

func (c *interbankClient) CounterNegotiation(ctx context.Context, id domain.ForeignBankId, offer domain.OtcOffer) error {
	path := fmt.Sprintf("/negotiations/%s/%s",
		strconv.FormatInt(id.RoutingNumber, 10),
		url.PathEscape(id.ID),
	)
	resp, body, err := c.doRequest(ctx, http.MethodPut, path, offer)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusConflict {
		return domain.ErrInterbankConflict
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("interbank PUT /negotiations: status %d, body %s", resp.StatusCode, string(body))
	}
	return nil
}

// ─── GET /negotiations/{routing}/{id} ────────────────────────────────────────

func (c *interbankClient) GetNegotiation(ctx context.Context, id domain.ForeignBankId) (*domain.OtcNegotiation, error) {
	path := fmt.Sprintf("/negotiations/%s/%s",
		strconv.FormatInt(id.RoutingNumber, 10),
		url.PathEscape(id.ID),
	)
	resp, body, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, domain.ErrInterbankNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("interbank GET /negotiations: status %d, body %s", resp.StatusCode, string(body))
	}
	var n domain.OtcNegotiation
	if err := json.Unmarshal(body, &n); err != nil {
		return nil, fmt.Errorf("interbank: parse negotiation: %w", err)
	}
	return &n, nil
}

// ─── DELETE /negotiations/{routing}/{id} ─────────────────────────────────────

func (c *interbankClient) CancelNegotiation(ctx context.Context, id domain.ForeignBankId) error {
	path := fmt.Sprintf("/negotiations/%s/%s",
		strconv.FormatInt(id.RoutingNumber, 10),
		url.PathEscape(id.ID),
	)
	resp, body, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotFound {
		return domain.ErrInterbankNotFound
	}
	if resp.StatusCode == http.StatusConflict {
		return domain.ErrInterbankConflict
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("interbank DELETE /negotiations: status %d, body %s", resp.StatusCode, string(body))
	}
	return nil
}

// ─── GET /negotiations/{routing}/{id}/accept ─────────────────────────────────

func (c *interbankClient) AcceptNegotiation(ctx context.Context, id domain.ForeignBankId) error {
	path := fmt.Sprintf("/negotiations/%s/%s/accept",
		strconv.FormatInt(id.RoutingNumber, 10),
		url.PathEscape(id.ID),
	)
	resp, body, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotFound {
		return domain.ErrInterbankNotFound
	}
	if resp.StatusCode == http.StatusConflict {
		return domain.ErrInterbankConflict
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("interbank GET /negotiations/accept: status %d, body %s", resp.StatusCode, string(body))
	}
	return nil
}

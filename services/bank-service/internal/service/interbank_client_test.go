package service_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"banka-backend/services/bank-service/internal/domain"
	"banka-backend/services/bank-service/internal/service"
)

func newTestClient(t *testing.T, serverURL string) domain.InterbankClient {
	t.Helper()
	return service.NewInterbankClient(service.InterbankClientConfig{
		PeerBaseURL:        serverURL,
		PeerAPIKey:         "test-key",
		PeerRoutingNumber:  222,
		OurRoutingNumber:   ourRouting,
		HTTPTimeoutSeconds: 5,
	})
}

// ─── ErrInterbankPeerNotConfigured ────────────────────────────────────────────

func TestInterbankClient_NotConfigured_SendMessage(t *testing.T) {
	c := service.NewInterbankClient(service.InterbankClientConfig{})
	ctx := context.Background()
	_, _, err := c.SendMessage(ctx, domain.InterbankMessage{})
	require.ErrorIs(t, err, domain.ErrInterbankPeerNotConfigured)
}

func TestInterbankClient_NotConfigured_GetPublicStock(t *testing.T) {
	c := service.NewInterbankClient(service.InterbankClientConfig{})
	_, err := c.GetPublicStock(context.Background())
	require.ErrorIs(t, err, domain.ErrInterbankPeerNotConfigured)
}

func TestInterbankClient_NotConfigured_CreateNegotiation(t *testing.T) {
	c := service.NewInterbankClient(service.InterbankClientConfig{})
	_, err := c.CreateNegotiation(context.Background(), domain.OtcOffer{})
	require.ErrorIs(t, err, domain.ErrInterbankPeerNotConfigured)
}

func TestInterbankClient_NotConfigured_CounterNegotiation(t *testing.T) {
	c := service.NewInterbankClient(service.InterbankClientConfig{})
	err := c.CounterNegotiation(context.Background(), domain.ForeignBankId{}, domain.OtcOffer{})
	require.ErrorIs(t, err, domain.ErrInterbankPeerNotConfigured)
}

func TestInterbankClient_NotConfigured_GetNegotiation(t *testing.T) {
	c := service.NewInterbankClient(service.InterbankClientConfig{})
	_, err := c.GetNegotiation(context.Background(), domain.ForeignBankId{})
	require.ErrorIs(t, err, domain.ErrInterbankPeerNotConfigured)
}

func TestInterbankClient_NotConfigured_CancelNegotiation(t *testing.T) {
	c := service.NewInterbankClient(service.InterbankClientConfig{})
	err := c.CancelNegotiation(context.Background(), domain.ForeignBankId{})
	require.ErrorIs(t, err, domain.ErrInterbankPeerNotConfigured)
}

func TestInterbankClient_NotConfigured_AcceptNegotiation(t *testing.T) {
	c := service.NewInterbankClient(service.InterbankClientConfig{})
	err := c.AcceptNegotiation(context.Background(), domain.ForeignBankId{})
	require.ErrorIs(t, err, domain.ErrInterbankPeerNotConfigured)
}

// ─── SendMessage ─────────────────────────────────────────────────────────────

func TestInterbankClient_SendMessage_200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/interbank", r.URL.Path)
		assert.Equal(t, "test-key", r.Header.Get("X-Api-Key"))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"vote":"YES"}`))
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	raw, _ := json.Marshal(map[string]string{})
	msg := domain.InterbankMessage{
		IdempotenceKey: domain.IdempotenceKey{RoutingNumber: ourRouting, LocallyGeneratedKey: "k1"},
		MessageType:    domain.MessageNewTx,
		Message:        json.RawMessage(raw),
	}

	status, body, err := c.SendMessage(context.Background(), msg)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.Contains(t, string(body), "YES")
}

func TestInterbankClient_SendMessage_500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	raw, _ := json.Marshal(map[string]string{})
	msg := domain.InterbankMessage{Message: json.RawMessage(raw)}

	status, _, err := c.SendMessage(context.Background(), msg)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, status)
}

// ─── GetPublicStock ───────────────────────────────────────────────────────────

func TestInterbankClient_GetPublicStock_OK(t *testing.T) {
	stocks := []domain.PublicStock{
		{Stock: domain.StockDescription{Ticker: "AAPL"}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/public-stock", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(stocks)
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	result, err := c.GetPublicStock(context.Background())
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "AAPL", result[0].Stock.Ticker)
}

func TestInterbankClient_GetPublicStock_Non200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	_, err := c.GetPublicStock(context.Background())
	require.Error(t, err)
}

// ─── CreateNegotiation ────────────────────────────────────────────────────────

func TestInterbankClient_CreateNegotiation_OK(t *testing.T) {
	id := domain.ForeignBankId{RoutingNumber: 222, ID: "neg1"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/negotiations", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(id)
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	got, err := c.CreateNegotiation(context.Background(), domain.OtcOffer{
		Stock:          domain.StockDescription{Ticker: "AAPL"},
		SettlementDate: time.Now().Add(24 * time.Hour).Format(time.RFC3339),
	})
	require.NoError(t, err)
	assert.Equal(t, "neg1", got.ID)
}

func TestInterbankClient_CreateNegotiation_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`bad request`))
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	_, err := c.CreateNegotiation(context.Background(), domain.OtcOffer{})
	require.Error(t, err)
}

// ─── CounterNegotiation ───────────────────────────────────────────────────────

func TestInterbankClient_CounterNegotiation_OK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	id := domain.ForeignBankId{RoutingNumber: 222, ID: "neg1"}
	err := c.CounterNegotiation(context.Background(), id, domain.OtcOffer{})
	require.NoError(t, err)
}

func TestInterbankClient_CounterNegotiation_Conflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	id := domain.ForeignBankId{RoutingNumber: 222, ID: "neg1"}
	err := c.CounterNegotiation(context.Background(), id, domain.OtcOffer{})
	require.ErrorIs(t, err, domain.ErrInterbankConflict)
}

// ─── GetNegotiation ───────────────────────────────────────────────────────────

func TestInterbankClient_GetNegotiation_OK(t *testing.T) {
	neg := domain.OtcNegotiation{
		OtcOffer: domain.OtcOffer{
			Stock:          domain.StockDescription{Ticker: "GOOG"},
			PricePerUnit:   domain.MonetaryValue{Currency: "USD", Amount: decimal.NewFromFloat(100)},
			SettlementDate: time.Now().Add(48 * time.Hour).Format(time.RFC3339),
		},
		IsOngoing: true,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(neg)
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	id := domain.ForeignBankId{RoutingNumber: 222, ID: "neg1"}
	got, err := c.GetNegotiation(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, "GOOG", got.Stock.Ticker)
}

func TestInterbankClient_GetNegotiation_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	id := domain.ForeignBankId{RoutingNumber: 222, ID: "x"}
	_, err := c.GetNegotiation(context.Background(), id)
	require.ErrorIs(t, err, domain.ErrInterbankNotFound)
}

// ─── CancelNegotiation ────────────────────────────────────────────────────────

func TestInterbankClient_CancelNegotiation_OK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	id := domain.ForeignBankId{RoutingNumber: 222, ID: "neg1"}
	err := c.CancelNegotiation(context.Background(), id)
	require.NoError(t, err)
}

func TestInterbankClient_CancelNegotiation_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	id := domain.ForeignBankId{RoutingNumber: 222, ID: "x"}
	err := c.CancelNegotiation(context.Background(), id)
	require.ErrorIs(t, err, domain.ErrInterbankNotFound)
}

func TestInterbankClient_CancelNegotiation_Conflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	id := domain.ForeignBankId{RoutingNumber: 222, ID: "x"}
	err := c.CancelNegotiation(context.Background(), id)
	require.ErrorIs(t, err, domain.ErrInterbankConflict)
}

// ─── AcceptNegotiation ────────────────────────────────────────────────────────

func TestInterbankClient_AcceptNegotiation_OK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/accept")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	id := domain.ForeignBankId{RoutingNumber: 222, ID: "neg1"}
	err := c.AcceptNegotiation(context.Background(), id)
	require.NoError(t, err)
}

func TestInterbankClient_AcceptNegotiation_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	id := domain.ForeignBankId{RoutingNumber: 222, ID: "x"}
	err := c.AcceptNegotiation(context.Background(), id)
	require.ErrorIs(t, err, domain.ErrInterbankNotFound)
}

func TestInterbankClient_AcceptNegotiation_Conflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	id := domain.ForeignBankId{RoutingNumber: 222, ID: "x"}
	err := c.AcceptNegotiation(context.Background(), id)
	require.ErrorIs(t, err, domain.ErrInterbankConflict)
}

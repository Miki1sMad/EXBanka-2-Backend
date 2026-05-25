package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"banka-backend/services/bank-service/internal/domain"
	auth "banka-backend/shared/auth"
)

// WatchlistHandler serves REST endpoints for watchlist management.
//
//	GET    /bank/watchlists              — list user's watchlists
//	POST   /bank/watchlists              — create watchlist
//	GET    /bank/watchlists/{id}         — get watchlist with items
//	DELETE /bank/watchlists/{id}         — delete watchlist
//	POST   /bank/watchlists/{id}/items   — add item
//	DELETE /bank/watchlists/{id}/items/{listingId} — remove item
type WatchlistHandler struct {
	repo      domain.WatchlistRepository
	jwtSecret string
}

func NewWatchlistHandler(repo domain.WatchlistRepository, jwtSecret string) *WatchlistHandler {
	return &WatchlistHandler{repo: repo, jwtSecret: jwtSecret}
}

func (h *WatchlistHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	claims, err := h.authenticate(r)
	if err != nil {
		watchlistWriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		watchlistWriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "bad token"})
		return
	}

	// Strip prefix; remaining path is "" or "/{id}" or "/{id}/items" or "/{id}/items/{listingId}"
	path := strings.TrimPrefix(r.URL.Path, "/bank/watchlists")
	path = strings.TrimSuffix(path, "/")

	if path == "" {
		switch r.Method {
		case http.MethodGet:
			h.listWatchlists(w, userID)
		case http.MethodPost:
			h.createWatchlist(w, r, userID)
		default:
			watchlistWriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return
	}

	// parts[0] = id, parts[1] = "items" (optional), parts[2] = listingId (optional)
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")

	watchlistID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		watchlistWriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid watchlist id"})
		return
	}

	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			h.getWatchlist(w, watchlistID, userID)
		case http.MethodDelete:
			h.deleteWatchlist(w, watchlistID, userID)
		default:
			watchlistWriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return
	}

	if len(parts) >= 2 && parts[1] == "items" {
		if len(parts) == 2 {
			if r.Method == http.MethodPost {
				h.addItem(w, r, watchlistID, userID)
				return
			}
			watchlistWriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		if len(parts) == 3 {
			listingID, err := strconv.ParseInt(parts[2], 10, 64)
			if err != nil {
				watchlistWriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid listing id"})
				return
			}
			if r.Method == http.MethodDelete {
				h.removeItem(w, watchlistID, listingID, userID)
				return
			}
			watchlistWriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
	}

	watchlistWriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}

func (h *WatchlistHandler) authenticate(r *http.Request) (*auth.AccessClaims, error) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return nil, fmt.Errorf("no token")
	}
	return auth.VerifyToken(strings.TrimPrefix(authHeader, "Bearer "), h.jwtSecret)
}

func (h *WatchlistHandler) listWatchlists(w http.ResponseWriter, userID int64) {
	lists, err := h.repo.List(userID)
	if err != nil {
		watchlistWriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	watchlistWriteJSON(w, http.StatusOK, map[string]interface{}{"watchlists": lists})
}

func (h *WatchlistHandler) createWatchlist(w http.ResponseWriter, r *http.Request, userID int64) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		watchlistWriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name je obavezan"})
		return
	}
	wl, err := h.repo.Create(userID, body.Name)
	if err != nil {
		watchlistWriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	watchlistWriteJSON(w, http.StatusCreated, map[string]interface{}{"watchlist": wl})
}

func (h *WatchlistHandler) getWatchlist(w http.ResponseWriter, id, userID int64) {
	detail, err := h.repo.GetWithItems(id, userID)
	if err != nil {
		if errors.Is(err, domain.ErrWatchlistNotFound) {
			watchlistWriteJSON(w, http.StatusNotFound, map[string]string{"error": "watchlist nije pronađen"})
			return
		}
		watchlistWriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	watchlistWriteJSON(w, http.StatusOK, map[string]interface{}{"watchlist": detail})
}

func (h *WatchlistHandler) deleteWatchlist(w http.ResponseWriter, id, userID int64) {
	if err := h.repo.Delete(id, userID); err != nil {
		if errors.Is(err, domain.ErrWatchlistNotFound) {
			watchlistWriteJSON(w, http.StatusNotFound, map[string]string{"error": "watchlist nije pronađen"})
			return
		}
		watchlistWriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *WatchlistHandler) addItem(w http.ResponseWriter, r *http.Request, watchlistID, userID int64) {
	var body struct {
		ListingID int64 `json:"listingId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ListingID == 0 {
		watchlistWriteJSON(w, http.StatusBadRequest, map[string]string{"error": "listingId je obavezan"})
		return
	}
	if err := h.repo.AddItem(watchlistID, body.ListingID, userID); err != nil {
		if errors.Is(err, domain.ErrWatchlistNotFound) {
			watchlistWriteJSON(w, http.StatusNotFound, map[string]string{"error": "watchlist nije pronađen"})
			return
		}
		if errors.Is(err, domain.ErrWatchlistItemExists) {
			watchlistWriteJSON(w, http.StatusConflict, map[string]string{"error": "hartija već postoji u watchlisti"})
			return
		}
		watchlistWriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *WatchlistHandler) removeItem(w http.ResponseWriter, watchlistID, listingID, userID int64) {
	if err := h.repo.RemoveItem(watchlistID, listingID, userID); err != nil {
		if errors.Is(err, domain.ErrWatchlistNotFound) {
			watchlistWriteJSON(w, http.StatusNotFound, map[string]string{"error": "watchlist nije pronađen"})
			return
		}
		watchlistWriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func watchlistWriteJSON(w http.ResponseWriter, status int, v interface{}) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

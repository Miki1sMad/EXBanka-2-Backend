package handler

// devices_handler.go — registracija FCM tokena (mobilni uređaji) i in-app
// notifications inbox.
//
// Endpoint-i (svi traže Bearer JWT, ulogovan klijent):
//   POST   /bank/user/devices                       — registruje/upserts FCM token
//   DELETE /bank/user/devices                       — uklanja token (body: { fcm_token })
//   GET    /bank/user/notifications?unreadOnly=...  — lista in-app notifikacija
//   POST   /bank/user/notifications/{id}/read       — označava jednu kao pročitanu
//   POST   /bank/user/notifications/read-all        — sve pročitane

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	auth "banka-backend/shared/auth"
	"gorm.io/gorm"
)

// DevicesHandler serves /bank/user/devices and /bank/user/notifications.
type DevicesHandler struct {
	db        *gorm.DB
	jwtSecret string
}

// NewDevicesHandler kreira novi handler.
func NewDevicesHandler(db *gorm.DB, jwtSecret string) *DevicesHandler {
	return &DevicesHandler{db: db, jwtSecret: jwtSecret}
}

func (h *DevicesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	userID, err := h.authClient(r)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	path := strings.TrimRight(r.URL.Path, "/")

	switch {
	case path == "/bank/user/devices" && r.Method == http.MethodPost:
		h.registerDevice(w, r, userID)
	case path == "/bank/user/devices" && r.Method == http.MethodDelete:
		h.unregisterDevice(w, r, userID)
	case path == "/bank/user/notifications" && r.Method == http.MethodGet:
		h.listNotifications(w, r, userID)
	case path == "/bank/user/notifications/read-all" && r.Method == http.MethodPost:
		h.markAllRead(w, r, userID)
	case strings.HasPrefix(path, "/bank/user/notifications/") && strings.HasSuffix(path, "/read") && r.Method == http.MethodPost:
		h.markOneRead(w, r, userID, path)
	default:
		writeJSONError(w, http.StatusNotFound, "not found")
	}
}

func (h *DevicesHandler) authClient(r *http.Request) (int64, error) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return 0, errUnauthorized
	}
	claims, err := auth.VerifyToken(strings.TrimPrefix(authHeader, "Bearer "), h.jwtSecret)
	if err != nil {
		return 0, err
	}
	id, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// ─── POST /bank/user/devices ─────────────────────────────────────────────────

type registerDeviceRequest struct {
	FCMToken string `json:"fcm_token"`
	DeviceID string `json:"device_id,omitempty"`
	Platform string `json:"platform,omitempty"` // ANDROID | IOS
}

type registerDeviceResponse struct {
	ID         int64  `json:"id"`
	DeviceID   string `json:"device_id"`
	Platform   string `json:"platform"`
	LastSeenAt string `json:"last_seen_at"`
}

func (h *DevicesHandler) registerDevice(w http.ResponseWriter, r *http.Request, userID int64) {
	var req registerDeviceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "neispravno telo zahteva")
		return
	}
	req.FCMToken = strings.TrimSpace(req.FCMToken)
	if req.FCMToken == "" {
		writeJSONError(w, http.StatusBadRequest, "fcm_token je obavezan")
		return
	}
	platform := strings.ToUpper(strings.TrimSpace(req.Platform))
	if platform == "" {
		platform = "ANDROID"
	}

	type row struct {
		ID         int64     `gorm:"column:id"`
		DeviceID   string    `gorm:"column:device_id"`
		Platform   string    `gorm:"column:platform"`
		LastSeenAt time.Time `gorm:"column:last_seen_at"`
	}

	var out row
	if err := h.db.WithContext(r.Context()).Raw(`
		INSERT INTO core_banking.mobile_devices
			(user_id, fcm_token, device_id, platform, last_seen_at)
		VALUES (?, ?, ?, ?, NOW())
		ON CONFLICT (fcm_token) DO UPDATE SET
			user_id      = EXCLUDED.user_id,
			device_id    = EXCLUDED.device_id,
			platform     = EXCLUDED.platform,
			last_seen_at = NOW()
		RETURNING id, device_id, platform, last_seen_at
	`, userID, req.FCMToken, req.DeviceID, platform).Scan(&out).Error; err != nil {
		writeJSONError(w, http.StatusInternalServerError, "greška pri registraciji uređaja")
		return
	}

	devicesWriteJSON(w, http.StatusOK, registerDeviceResponse{
		ID:         out.ID,
		DeviceID:   out.DeviceID,
		Platform:   out.Platform,
		LastSeenAt: out.LastSeenAt.UTC().Format(time.RFC3339),
	})
}

// ─── DELETE /bank/user/devices ───────────────────────────────────────────────

type unregisterDeviceRequest struct {
	FCMToken string `json:"fcm_token"`
}

func (h *DevicesHandler) unregisterDevice(w http.ResponseWriter, r *http.Request, userID int64) {
	var req unregisterDeviceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "neispravno telo zahteva")
		return
	}
	req.FCMToken = strings.TrimSpace(req.FCMToken)
	if req.FCMToken == "" {
		writeJSONError(w, http.StatusBadRequest, "fcm_token je obavezan")
		return
	}

	if err := h.db.WithContext(r.Context()).Exec(`
		DELETE FROM core_banking.mobile_devices
		WHERE user_id = ? AND fcm_token = ?
	`, userID, req.FCMToken).Error; err != nil {
		writeJSONError(w, http.StatusInternalServerError, "greška pri odjavi uređaja")
		return
	}
	devicesWriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ─── GET /bank/user/notifications ────────────────────────────────────────────

type notificationItem struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Data      string `json:"data"` // raw JSON string; klijent ga može parsirati
	ReadAt    string `json:"read_at,omitempty"`
	CreatedAt string `json:"created_at"`
}

type notificationsResponse struct {
	Notifications []notificationItem `json:"notifications"`
	UnreadCount   int64              `json:"unreadCount"`
}

func (h *DevicesHandler) listNotifications(w http.ResponseWriter, r *http.Request, userID int64) {
	unreadOnly := r.URL.Query().Get("unreadOnly") == "true"
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			if n > 200 {
				n = 200
			}
			limit = n
		}
	}

	sql := `
		SELECT id, type, title, body, data_json, read_at, created_at
		FROM core_banking.push_notifications
		WHERE user_id = ?
	`
	args := []interface{}{userID}
	if unreadOnly {
		sql += " AND read_at IS NULL"
	}
	sql += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)

	type row struct {
		ID        int64      `gorm:"column:id"`
		Type      string     `gorm:"column:type"`
		Title     string     `gorm:"column:title"`
		Body      string     `gorm:"column:body"`
		DataJSON  string     `gorm:"column:data_json"`
		ReadAt    *time.Time `gorm:"column:read_at"`
		CreatedAt time.Time  `gorm:"column:created_at"`
	}
	var rows []row
	if err := h.db.WithContext(r.Context()).Raw(sql, args...).Scan(&rows).Error; err != nil {
		writeJSONError(w, http.StatusInternalServerError, "greška pri dohvatu notifikacija")
		return
	}

	items := make([]notificationItem, 0, len(rows))
	for _, x := range rows {
		item := notificationItem{
			ID:        x.ID,
			Type:      x.Type,
			Title:     x.Title,
			Body:      x.Body,
			Data:      x.DataJSON,
			CreatedAt: x.CreatedAt.UTC().Format(time.RFC3339),
		}
		if x.ReadAt != nil {
			item.ReadAt = x.ReadAt.UTC().Format(time.RFC3339)
		}
		items = append(items, item)
	}

	var unread int64
	h.db.WithContext(r.Context()).Raw(`
		SELECT COUNT(*) FROM core_banking.push_notifications
		WHERE user_id = ? AND read_at IS NULL
	`, userID).Scan(&unread)

	devicesWriteJSON(w, http.StatusOK, notificationsResponse{
		Notifications: items,
		UnreadCount:   unread,
	})
}

// ─── POST /bank/user/notifications/{id}/read ─────────────────────────────────

func (h *DevicesHandler) markOneRead(w http.ResponseWriter, r *http.Request, userID int64, path string) {
	// path: /bank/user/notifications/{id}/read
	rest := strings.TrimPrefix(path, "/bank/user/notifications/")
	rest = strings.TrimSuffix(rest, "/read")
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "neispravan ID")
		return
	}

	if err := h.db.WithContext(r.Context()).Exec(`
		UPDATE core_banking.push_notifications
		SET read_at = NOW()
		WHERE id = ? AND user_id = ? AND read_at IS NULL
	`, id, userID).Error; err != nil {
		writeJSONError(w, http.StatusInternalServerError, "greška pri označavanju kao pročitano")
		return
	}
	devicesWriteJSON(w, http.StatusOK, map[string]string{"status": "read"})
}

// ─── POST /bank/user/notifications/read-all ──────────────────────────────────

func (h *DevicesHandler) markAllRead(w http.ResponseWriter, r *http.Request, userID int64) {
	if err := h.db.WithContext(r.Context()).Exec(`
		UPDATE core_banking.push_notifications
		SET read_at = NOW()
		WHERE user_id = ? AND read_at IS NULL
	`, userID).Error; err != nil {
		writeJSONError(w, http.StatusInternalServerError, "greška pri označavanju svih kao pročitanih")
		return
	}
	devicesWriteJSON(w, http.StatusOK, map[string]string{"status": "all-read"})
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func devicesWriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// errUnauthorized signals missing/invalid bearer token.
var errUnauthorized = errUnauthorizedConst{}

type errUnauthorizedConst struct{}

func (errUnauthorizedConst) Error() string { return "unauthorized" }

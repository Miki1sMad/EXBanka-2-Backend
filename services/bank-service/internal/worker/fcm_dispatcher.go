package worker

// fcm_dispatcher.go — slanje push notifikacija preko Firebase Cloud Messaging-a.
//
// SKELETON: zahteva FCM_SERVER_KEY env var (legacy server key) ili
// Service Account JSON (FCM v1 API). Ako kredencijal nije postavljen,
// dispatcher samo loguje payload i ne radi HTTP poziv — koristi se kao
// no-op za development.
//
// Pre nego što ovo radi za pravu push notifikaciju, treba:
//   1) Konfigurisati Firebase projekat
//   2) Postaviti FCM_SERVER_KEY env var (ili migrirati na FCM v1 sa Service Account-om)
//   3) Mobilna aplikacija mora da registruje stvaran FCM token kroz POST /user/devices.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"gorm.io/gorm"
)

// FCMDispatcher orkestrira:
//   - upis notifikacije u core_banking.push_notifications (in-app inbox)
//   - slanje FCM poruke svim aktivnim uređajima korisnika (ako je server key postavljen)
type FCMDispatcher struct {
	db         *gorm.DB
	serverKey  string // FCM legacy server key; "" → dispatcher je no-op za push slanje (inbox upis i dalje radi)
	httpClient *http.Client
}

// NewFCMDispatcher kreira novi dispatcher. Bez serverKey-a postaje no-op za FCM HTTP poziv.
func NewFCMDispatcher(db *gorm.DB, serverKey string) *FCMDispatcher {
	return &FCMDispatcher{
		db:        db,
		serverKey: serverKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Notify upisuje notifikaciju u inbox i šalje push svim uređajima korisnika.
// Vraća inbox ID. Fire-and-forget za FCM HTTP — greške se loguju.
func (d *FCMDispatcher) Notify(ctx context.Context, userID int64, notificationType, title, body string, data map[string]string) (int64, error) {
	dataJSON := "{}"
	if data != nil {
		if b, err := json.Marshal(data); err == nil {
			dataJSON = string(b)
		}
	}

	var inboxID int64
	if err := d.db.WithContext(ctx).Raw(`
		INSERT INTO core_banking.push_notifications
			(user_id, type, title, body, data_json)
		VALUES (?, ?, ?, ?, ?)
		RETURNING id
	`, userID, notificationType, title, body, dataJSON).Scan(&inboxID).Error; err != nil {
		log.Printf("[fcm] inbox insert failed for user_id=%d: %v", userID, err)
		return 0, err
	}

	// Pošalji FCM push svim uređajima (fire-and-forget). Inbox upis je već uspeo.
	go d.dispatchToDevices(context.Background(), userID, notificationType, title, body, data)

	return inboxID, nil
}

func (d *FCMDispatcher) dispatchToDevices(ctx context.Context, userID int64, notificationType, title, body string, data map[string]string) {
	if d.serverKey == "" {
		log.Printf("[fcm] FCM_SERVER_KEY not set — push send skipped (inbox only) user_id=%d type=%s", userID, notificationType)
		return
	}

	type device struct {
		Token string `gorm:"column:fcm_token"`
	}
	var devices []device
	if err := d.db.WithContext(ctx).Raw(`
		SELECT fcm_token FROM core_banking.mobile_devices WHERE user_id = ?
	`, userID).Scan(&devices).Error; err != nil {
		log.Printf("[fcm] device lookup failed user_id=%d: %v", userID, err)
		return
	}
	if len(devices) == 0 {
		return
	}

	for _, dev := range devices {
		d.sendOne(dev.Token, notificationType, title, body, data)
	}
}

// sendOne šalje jednu poruku preko legacy FCM HTTP API-ja.
// TODO: migrirati na FCM v1 API kad bude Service Account JSON dostupan.
func (d *FCMDispatcher) sendOne(token, notificationType, title, body string, data map[string]string) {
	payload := map[string]interface{}{
		"to":           token,
		"notification": map[string]string{"title": title, "body": body},
		"data": func() map[string]string {
			m := map[string]string{"type": notificationType}
			for k, v := range data {
				m[k] = v
			}
			return m
		}(),
		"priority": "high",
	}

	buf, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[fcm] marshal failed: %v", err)
		return
	}

	req, err := http.NewRequest("POST", "https://fcm.googleapis.com/fcm/send", bytes.NewReader(buf))
	if err != nil {
		log.Printf("[fcm] request build failed: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("key=%s", d.serverKey))

	resp, err := d.httpClient.Do(req)
	if err != nil {
		log.Printf("[fcm] send failed: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		shortToken := token
		if len(shortToken) > 8 {
			shortToken = shortToken[:8] + "…"
		}
		log.Printf("[fcm] non-2xx status %d for token=%s", resp.StatusCode, shortToken)
		return
	}
}

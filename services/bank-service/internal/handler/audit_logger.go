package handler

import (
	"log"

	"banka-backend/services/bank-service/internal/domain"
)

// SystemAuditLogger wraps an AuditLogRepository for fire-and-forget logging.
type SystemAuditLogger struct {
	repo domain.AuditLogRepository
}

func NewSystemAuditLogger(repo domain.AuditLogRepository) *SystemAuditLogger {
	return &SystemAuditLogger{repo: repo}
}

// Log fires an audit entry asynchronously. Never blocks or panics.
func (l *SystemAuditLogger) Log(action domain.AuditAction, actorID, targetID *int64, details map[string]interface{}) {
	entry := domain.AuditLogEntry{
		Action:   action,
		ActorID:  actorID,
		TargetID: targetID,
		Details:  details,
	}
	go func() {
		if err := l.repo.Create(entry); err != nil {
			log.Printf("[audit] greška pri upisu audit loga: %v", err)
		}
	}()
}

// ptr64 is a helper to get pointer to int64.
func ptr64(v int64) *int64 { return &v }

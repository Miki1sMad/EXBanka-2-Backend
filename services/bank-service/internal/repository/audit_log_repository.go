package repository

import (
	"encoding/json"
	"time"

	"banka-backend/services/bank-service/internal/domain"

	"gorm.io/gorm"
)

type auditLogModel struct {
	ID        int64     `gorm:"column:id;primaryKey"`
	Action    string    `gorm:"column:action"`
	ActorID   *int64    `gorm:"column:actor_id"`
	TargetID  *int64    `gorm:"column:target_id"`
	Details   string    `gorm:"column:details"` // JSON string
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (auditLogModel) TableName() string { return "core_banking.system_audit_log" }

type auditLogRepository struct {
	db *gorm.DB
}

func NewAuditLogRepository(db *gorm.DB) domain.AuditLogRepository {
	return &auditLogRepository{db: db}
}

func (r *auditLogRepository) Create(entry domain.AuditLogEntry) error {
	raw, err := json.Marshal(entry.Details)
	if err != nil {
		raw = []byte("{}")
	}
	m := auditLogModel{
		Action:   string(entry.Action),
		ActorID:  entry.ActorID,
		TargetID: entry.TargetID,
		Details:  string(raw),
	}
	return r.db.Create(&m).Error
}

func (r *auditLogRepository) List(filter domain.AuditLogFilter) ([]domain.AuditLogEntry, int64, error) {
	q := r.db.Model(&auditLogModel{})

	if filter.Action != "" {
		q = q.Where("action = ?", filter.Action)
	}
	if filter.ActorID != nil {
		q = q.Where("actor_id = ?", *filter.ActorID)
	}
	if filter.DateFrom != nil {
		q = q.Where("created_at >= ?", *filter.DateFrom)
	}
	if filter.DateTo != nil {
		q = q.Where("created_at <= ?", *filter.DateTo)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize < 1 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize

	var rows []auditLogModel
	if err := q.Order("created_at DESC").Limit(pageSize).Offset(offset).Find(&rows).Error; err != nil {
		return nil, 0, err
	}

	entries := make([]domain.AuditLogEntry, 0, len(rows))
	for _, m := range rows {
		var details map[string]interface{}
		_ = json.Unmarshal([]byte(m.Details), &details)
		if details == nil {
			details = map[string]interface{}{}
		}
		entries = append(entries, domain.AuditLogEntry{
			ID:        m.ID,
			Action:    domain.AuditAction(m.Action),
			ActorID:   m.ActorID,
			TargetID:  m.TargetID,
			Details:   details,
			CreatedAt: m.CreatedAt,
		})
	}
	return entries, total, nil
}

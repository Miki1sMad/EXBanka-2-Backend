package domain

import "time"

type AuditAction string

const (
	AuditOrderApproved     AuditAction = "ORDER_APPROVED"
	AuditOrderDeclined     AuditAction = "ORDER_DECLINED"
	AuditOrderCanceled     AuditAction = "ORDER_CANCELED"
	AuditAgentLimitSet     AuditAction = "AGENT_LIMIT_SET"
	AuditAgentLimitReset   AuditAction = "AGENT_LIMIT_RESET"
	AuditActuaryCreated    AuditAction = "ACTUARY_CREATED"
	AuditActuaryDeleted    AuditAction = "ACTUARY_DELETED"
	AuditTaxCalculated     AuditAction = "TAX_CALCULATED"
	AuditPermissionChanged AuditAction = "PERMISSION_CHANGED"
)

type AuditLogEntry struct {
	ID        int64
	Action    AuditAction
	ActorID   *int64
	TargetID  *int64
	Details   map[string]interface{}
	CreatedAt time.Time
}

type AuditLogFilter struct {
	Action   string
	ActorID  *int64
	DateFrom *time.Time
	DateTo   *time.Time
	Page     int
	PageSize int
}

type AuditLogRepository interface {
	Create(entry AuditLogEntry) error
	List(filter AuditLogFilter) ([]AuditLogEntry, int64, error)
}

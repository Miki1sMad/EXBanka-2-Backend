package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"banka-backend/services/bank-service/internal/domain"
	"banka-backend/services/bank-service/internal/transport"
	auth "banka-backend/shared/auth"

	"google.golang.org/grpc/metadata"
)

// AuditLogHandler serves audit log endpoints.
//
//	GET  /bank/audit-log         — list entries (ADMIN or SUPERVISOR only)
//	POST /bank/internal/audit    — accept external audit entries (service-to-service)
type AuditLogHandler struct {
	repo       domain.AuditLogRepository
	userClient *transport.UserServiceClient
	jwtSecret  string
}

func NewAuditLogHandler(repo domain.AuditLogRepository, jwtSecret string, userClient *transport.UserServiceClient) *AuditLogHandler {
	return &AuditLogHandler{repo: repo, userClient: userClient, jwtSecret: jwtSecret}
}

func (h *AuditLogHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/bank/audit-log":
		h.listEntries(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/bank/internal/audit":
		h.createEntry(w, r)
	default:
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	}
}

// listEntries handles GET /bank/audit-log.
func (h *AuditLogHandler) listEntries(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.requireAdminOrSupervisor(w, r)
	if !ok {
		return
	}
	_ = claims

	q := r.URL.Query()

	filter := domain.AuditLogFilter{
		Action:   strings.TrimSpace(q.Get("action")),
		Page:     1,
		PageSize: 50,
	}

	if v := strings.TrimSpace(q.Get("actor_id")); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err == nil {
			filter.ActorID = &id
		}
	}

	if v := strings.TrimSpace(q.Get("from")); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err == nil {
			filter.DateFrom = &t
		}
	}

	if v := strings.TrimSpace(q.Get("to")); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err == nil {
			// Include the full end day.
			end := t.Add(24*time.Hour - time.Nanosecond)
			filter.DateTo = &end
		}
	}

	if v := strings.TrimSpace(q.Get("page")); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			filter.Page = p
		}
	}

	if v := strings.TrimSpace(q.Get("page_size")); v != "" {
		if ps, err := strconv.Atoi(v); err == nil && ps > 0 {
			if ps > 200 {
				ps = 200
			}
			filter.PageSize = ps
		}
	}

	entries, total, err := h.repo.List(filter)
	if err != nil {
		auditWriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list audit log"})
		return
	}

	// ── Resolve user names ────────────────────────────────────────────────────
	type userInfo struct {
		Name  string
		Email string
	}
	resolved := make(map[int64]userInfo)

	if h.userClient != nil {
		// Collect unique IDs
		idSet := make(map[int64]struct{})
		for _, e := range entries {
			if e.ActorID != nil {
				idSet[*e.ActorID] = struct{}{}
			}
			if e.TargetID != nil {
				idSet[*e.TargetID] = struct{}{}
			}
		}

		if len(idSet) > 0 {
			authHeader := r.Header.Get("Authorization")

			// Batch-fetch all employees/admins in one call
			empCtx, empCancel := context.WithTimeout(r.Context(), 5*time.Second)
			if authHeader != "" {
				empCtx = metadata.NewOutgoingContext(empCtx, metadata.Pairs("authorization", authHeader))
			}
			employeeMap, _ := h.userClient.GetAllEmployeesAsMap(empCtx)
			empCancel()

			for id := range idSet {
				if info, ok := employeeMap[id]; ok {
					resolved[id] = userInfo{
						Name:  info.FirstName + " " + info.LastName,
						Email: info.Email,
					}
					continue
				}
				// Not an employee — try client
				clCtx, clCancel := context.WithTimeout(r.Context(), 3*time.Second)
				if authHeader != "" {
					clCtx = metadata.NewOutgoingContext(clCtx, metadata.Pairs("authorization", authHeader))
				}
				if info, err := h.userClient.GetClientInfo(clCtx, id); err == nil {
					resolved[id] = userInfo{
						Name:  info.FirstName + " " + info.LastName,
						Email: info.Email,
					}
				}
				clCancel()
			}
		}
	}

	type entryJSON struct {
		ID         int64                  `json:"id"`
		Action     string                 `json:"action"`
		ActorID    *int64                 `json:"actorId,omitempty"`
		ActorName  string                 `json:"actorName,omitempty"`
		ActorEmail string                 `json:"actorEmail,omitempty"`
		TargetID   *int64                 `json:"targetId,omitempty"`
		TargetName string                 `json:"targetName,omitempty"`
		Details    map[string]interface{} `json:"details"`
		CreatedAt  time.Time              `json:"createdAt"`
	}

	out := make([]entryJSON, 0, len(entries))
	for _, e := range entries {
		item := entryJSON{
			ID:        e.ID,
			Action:    string(e.Action),
			ActorID:   e.ActorID,
			TargetID:  e.TargetID,
			Details:   e.Details,
			CreatedAt: e.CreatedAt,
		}
		if e.ActorID != nil {
			if info, ok := resolved[*e.ActorID]; ok {
				item.ActorName = info.Name
				item.ActorEmail = info.Email
			}
		}
		if e.TargetID != nil {
			if info, ok := resolved[*e.TargetID]; ok {
				item.TargetName = info.Name
			}
		}
		out = append(out, item)
	}

	auditWriteJSON(w, http.StatusOK, map[string]interface{}{
		"entries":  out,
		"total":    total,
		"page":     filter.Page,
		"pageSize": filter.PageSize,
	})
}

// createEntry handles POST /bank/internal/audit.
func (h *AuditLogHandler) createEntry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action   string                 `json:"action"`
		ActorID  *int64                 `json:"actor_id"`
		TargetID *int64                 `json:"target_id"`
		Details  map[string]interface{} `json:"details"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if req.Action == "" {
		http.Error(w, `{"error":"action is required"}`, http.StatusBadRequest)
		return
	}
	if req.Details == nil {
		req.Details = map[string]interface{}{}
	}

	entry := domain.AuditLogEntry{
		Action:   domain.AuditAction(req.Action),
		ActorID:  req.ActorID,
		TargetID: req.TargetID,
		Details:  req.Details,
	}
	if err := h.repo.Create(entry); err != nil {
		http.Error(w, `{"error":"failed to create audit entry"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (h *AuditLogHandler) requireAdminOrSupervisor(w http.ResponseWriter, r *http.Request) (*auth.AccessClaims, bool) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return nil, false
	}
	claims, err := auth.VerifyToken(strings.TrimPrefix(authHeader, "Bearer "), h.jwtSecret)
	if err != nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return nil, false
	}
	if claims.UserType == "ADMIN" {
		return claims, true
	}
	for _, p := range claims.Permissions {
		if p == "SUPERVISOR" {
			return claims, true
		}
	}
	http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
	return nil, false
}

func auditWriteJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

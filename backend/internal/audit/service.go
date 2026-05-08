package audit

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/cbdc-simulator/backend/pkg/database"
)

// Service writes audit log entries to the database.
// It is designed to never panic or return errors to the caller —
// if audit logging fails, we log the failure but do NOT abort the
// business operation. A failed audit write is serious but must not
// block a legitimate payment.
type Service struct {
	db *database.Pool
}

// NewService creates a new audit Service.
func NewService(db *database.Pool) *Service {
	return &Service{db: db}
}

// Log writes an audit entry. It is safe to call concurrently.
// The operation being audited should NOT be aborted if this returns an error —
// log the error and proceed.
func (s *Service) Log(ctx context.Context, p LogParams) {
	// nil db means we're running in a test context with NewServiceWithLogger —
	// skip the DB write silently so unit tests don't panic.
	if s.db == nil {
		return
	}
	metaJSON, err := json.Marshal(p.Metadata)
	if err != nil {
		// Metadata serialisation failure should not block the audit write.
		// Use an empty object instead.
		metaJSON = []byte("{}")
	}

	// Convert *uuid.UUID to string for the INET/UUID DB columns.
	// pgx handles nil pointers as SQL NULL automatically.
	var actorIDStr *string
	if p.ActorID != nil {
		s := p.ActorID.String()
		actorIDStr = &s
	}

	var resourceIDStr *string
	if p.ResourceID != nil {
		s := p.ResourceID.String()
		resourceIDStr = &s
	}

	// Truncate user agent to 500 chars max (matches DB column length).
	if len(p.UserAgent) > 500 {
		p.UserAgent = p.UserAgent[:500]
	}

	// ip_address is an inet column — cast text input explicitly so pgx doesn't
	// try to infer the type and fail on IPv4-mapped or unusual address formats.
	var ipParam any
	if p.IPAddress != "" {
		ipParam = p.IPAddress // PostgreSQL will cast text → inet via $6::inet in the query
	}

	_, err = s.db.Exec(ctx, `
		INSERT INTO audit_logs
			(actor_id, actor_role, action, resource_type, resource_id,
			 ip_address, user_agent, request_id, metadata, success, error_code)
		VALUES
			($1, $2, $3, $4, $5, $6::inet, $7, $8, $9, $10, $11)
	`,
		actorIDStr,
		nullIfEmpty(p.ActorRole),
		p.Action,
		nullIfEmpty(p.ResourceType),
		resourceIDStr,
		ipParam,
		nullIfEmpty(p.UserAgent),
		nullIfEmpty(p.RequestID),
		metaJSON,
		p.Success,
		nullIfEmpty(p.ErrorCode),
	)
	if err != nil {
		// IMPORTANT: do not propagate this error to the caller.
		// Audit logging failure is a serious operational issue but must not
		// cause payments or auth operations to fail.
		slog.Error("audit log write failed",
			"action", p.Action,
			"actor_id", p.ActorID,
			"error", err,
		)
	}
}

// nullIfEmpty returns nil if the string is empty, otherwise returns a pointer to it.
// pgx uses nil to represent SQL NULL for pointer types.
func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

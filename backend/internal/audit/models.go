// Package audit provides an append-only audit log service.
// Every sensitive action (login, payment, freeze, CBDC issuance) writes an entry
// BEFORE it executes — so even failed attempts leave a trace.
package audit

import (
	"time"

	"github.com/google/uuid"
)

// Action constants define every auditable event in the system.
// Using constants (not free-form strings) ensures audit log entries are
// consistent and searchable — misspelled action names would be impossible to query.
const (
	ActionUserRegister       = "user.register"
	ActionUserLogin          = "user.login"
	ActionUserLoginFailed    = "user.login.failed"
	ActionUserLogout         = "user.logout"
	ActionUserTokenRefresh   = "user.token.refresh"
	ActionUserLocked         = "user.locked"
	ActionWalletCreate       = "wallet.create"
	ActionWalletFreeze       = "wallet.freeze"
	ActionWalletUnfreeze     = "wallet.unfreeze"
	ActionPaymentSend        = "payment.send"
	ActionPaymentRefund      = "payment.refund"
	ActionMerchantPayRequest = "merchant.payment_request.create"
	ActionMerchantPayPaid    = "merchant.payment_request.paid"
	ActionCBDCIssue          = "cbdc.issue"
	ActionAdminUserView      = "admin.user.view"
)

// Entry represents a single audit log record.
// Fields map directly to the audit_logs table columns.
type Entry struct {
	ID           int64          // BIGSERIAL — sequential, detects gaps (tampering indicator)
	ActorID      *uuid.UUID     // nil for unauthenticated or system actions
	ActorRole    string
	Action       string
	ResourceType string
	ResourceID   *uuid.UUID
	IPAddress    string
	UserAgent    string
	RequestID    string
	Metadata     map[string]any // flexible JSON for action-specific details
	Success      bool
	ErrorCode    string
	CreatedAt    time.Time
}

// LogParams contains all inputs needed to write a single audit entry.
// Using a struct instead of many function args prevents argument order mistakes.
type LogParams struct {
	ActorID      *uuid.UUID
	ActorRole    string
	Action       string
	ResourceType string
	ResourceID   *uuid.UUID
	IPAddress    string
	UserAgent    string
	RequestID    string
	Metadata     map[string]any
	Success      bool
	ErrorCode    string
}

// Package admin handles CBDC issuance and other admin-only operations.
// All endpoints require role=admin in the JWT. Every action is audit-logged.
package admin

import (
	"errors"
	"strings"
	"time"
)

// Domain errors.
var (
	ErrAmountTooLarge    = errors.New("amount exceeds maximum issuance limit of DD$1,000,000 per action")
	ErrAmountZero        = errors.New("amount must be greater than zero")
	ErrReasonTooShort    = errors.New("reason must be at least 10 characters")
	ErrMissingIdempotencyKey = errors.New("X-Idempotency-Key header is required for issuance")
)

const (
	// maxIssuanceCents is DD$1,000,000 — matches the DB CHECK constraint.
	// Prevents a single fat-finger from issuing an economy-breaking amount.
	maxIssuanceCents = 100_000_000
)

// IssueRequest is the JSON body for POST /api/v1/admin/issue-cbdc.
type IssueRequest struct {
	WalletID    string `json:"wallet_id"`
	AmountCents int64  `json:"amount_cents"`
	Reason      string `json:"reason"`
}

// Validate checks all IssueRequest fields before any DB access.
func (r *IssueRequest) Validate() error {
	if r.AmountCents <= 0 {
		return ErrAmountZero
	}
	if r.AmountCents > maxIssuanceCents {
		return ErrAmountTooLarge
	}
	r.Reason = strings.TrimSpace(r.Reason)
	if len(r.Reason) < 10 {
		return ErrReasonTooShort
	}
	return nil
}

// IssuanceResponse is returned on successful issuance.
type IssuanceResponse struct {
	Issuance       IssuanceDetail `json:"issuance"`
	NewBalanceCents int64         `json:"new_balance_cents"`
	NewBalanceDisplay string      `json:"new_balance_display"`
}

// IssuanceDetail contains the created issuance record details.
type IssuanceDetail struct {
	ID            string    `json:"id"`
	WalletID      string    `json:"wallet_id"`
	AmountCents   int64     `json:"amount_cents"`
	AmountDisplay string    `json:"amount_display"`
	Reason        string    `json:"reason"`
	TransactionID string    `json:"transaction_id"`
	CreatedAt     time.Time `json:"created_at"`
}

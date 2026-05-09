// Package payment handles P2P transfers between wallets.
// All value movement goes through the ledger engine — this package only
// coordinates validation, idempotency, signing, and the HTTP layer.
package payment

import (
	"errors"
	"strings"
	"time"
)

// Domain errors.
var (
	ErrSelfPayment        = errors.New("cannot send payment to your own wallet")
	ErrAmountZero         = errors.New("amount must be greater than zero")
	ErrReferenceTooLong   = errors.New("reference must be 256 characters or fewer")
	ErrTransactionNotFound = errors.New("transaction not found")
	ErrAccessDenied       = errors.New("access denied: transaction does not involve your wallet")
	ErrMissingIdempotencyKey = errors.New("X-Idempotency-Key header is required")
)

// SendRequest is the JSON body for POST /api/v1/payments/send.
type SendRequest struct {
	ToWalletID  string `json:"to_wallet_id"`
	AmountCents int64  `json:"amount_cents"`
	Reference   string `json:"reference"` // optional, max 256 chars
}

// Validate checks fields before touching the DB.
func (r *SendRequest) Validate() error {
	r.Reference = strings.TrimSpace(r.Reference)
	if r.AmountCents <= 0 {
		return ErrAmountZero
	}
	if len(r.Reference) > 256 {
		return ErrReferenceTooLong
	}
	return nil
}

// SendResponse is returned on a successful payment.
type SendResponse struct {
	Transaction       TransactionDetail `json:"transaction"`
	NewBalanceCents   int64             `json:"new_balance_cents"`
	NewBalanceDisplay string            `json:"new_balance_display"`
}

// TransactionDetail is the full transaction view returned on send and GET by ID.
type TransactionDetail struct {
	ID               string  `json:"id"`
	Type             string  `json:"type"`
	Status           string  `json:"status"`
	Direction        string  `json:"direction"`          // "DEBIT" or "CREDIT" relative to requester
	SenderWalletID   *string `json:"sender_wallet_id"`
	ReceiverWalletID *string `json:"receiver_wallet_id"`
	CounterpartyName *string `json:"counterparty_name"`  // other party's display name
	AmountCents      int64   `json:"amount_cents"`
	AmountDisplay    string  `json:"amount_display"`
	FeeCents         int64   `json:"fee_cents"`
	Reference        *string `json:"reference"`
	Signature        string  `json:"signature"`
	CreatedAt        string  `json:"created_at"`
	SettledAt        *string `json:"settled_at"`
}

// PaymentListResponse wraps paginated payment history.
type PaymentListResponse struct {
	Transactions []TransactionDetail `json:"transactions"`
	Pagination   Pagination          `json:"pagination"`
}

// Pagination metadata.
type Pagination struct {
	Page  int `json:"page"`
	Limit int `json:"limit"`
	Total int `json:"total"`
	Pages int `json:"pages"`
}

// ListParams holds validated query params for payment history.
type ListParams struct {
	Page   int
	Limit  int
	Type   string
	Status string
	From   *time.Time
	To     *time.Time
}

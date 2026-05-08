// Package wallet handles wallet HTTP endpoints and read queries.
// Write operations (balance changes) go through internal/ledger — the wallet
// package never modifies balances directly.
package wallet

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Domain errors.
var (
	ErrWalletNotFound  = errors.New("wallet not found")
	ErrAccessDenied    = errors.New("access denied: wallet belongs to another user")
	ErrAlreadyExists   = errors.New("wallet already exists for this currency")
)

// Wallet is the full wallet domain object (includes freeze metadata).
type Wallet struct {
	ID           uuid.UUID
	UserID       uuid.UUID
	Currency     string
	Balance      int64
	IsFrozen     bool
	FrozenReason *string
	FrozenBy     *uuid.UUID
	FrozenAt     *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// WalletResponse is the public API representation of a wallet.
type WalletResponse struct {
	ID             string `json:"id"`
	UserID         string `json:"user_id"`
	Currency       string `json:"currency"`
	BalanceCents   int64  `json:"balance_cents"`
	BalanceDisplay string `json:"balance_display"`
	IsFrozen       bool   `json:"is_frozen"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

// BalanceResponse is the lightweight balance-only response.
type BalanceResponse struct {
	WalletID       string `json:"wallet_id"`
	BalanceCents   int64  `json:"balance_cents"`
	BalanceDisplay string `json:"balance_display"`
	IsFrozen       bool   `json:"is_frozen"`
	AsOf           string `json:"as_of"`
}

// TransactionRow is a single transaction in the history list.
// "direction" is computed from the query: DEBIT if this wallet sent, CREDIT if received.
type TransactionRow struct {
	ID                 string  `json:"id"`
	Type               string  `json:"type"`
	Status             string  `json:"status"`
	Direction          string  `json:"direction"` // "DEBIT" or "CREDIT"
	CounterpartyName   *string `json:"counterparty_name"` // nil for system issuances
	CounterpartyWallet *string `json:"counterparty_wallet_id,omitempty"`
	AmountCents        int64   `json:"amount_cents"`
	AmountDisplay      string  `json:"amount_display"`
	Reference          *string `json:"reference"`
	CreatedAt          string  `json:"created_at"`
	SettledAt          *string `json:"settled_at"`
}

// TransactionListResponse wraps paginated transaction history.
type TransactionListResponse struct {
	Transactions []TransactionRow `json:"transactions"`
	Pagination   Pagination       `json:"pagination"`
}

// Pagination metadata returned with every list endpoint.
type Pagination struct {
	Page  int `json:"page"`
	Limit int `json:"limit"`
	Total int `json:"total"`
	Pages int `json:"pages"`
}

// ListParams holds validated query parameters for transaction history.
type ListParams struct {
	Page   int
	Limit  int
	Type   string // optional filter
	Status string // optional filter
	From   *time.Time
	To     *time.Time
}

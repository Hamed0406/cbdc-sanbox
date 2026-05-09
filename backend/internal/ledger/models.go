// Package ledger implements the double-entry bookkeeping engine.
// Every value transfer creates exactly two ledger_entries (DEBIT + CREDIT),
// ensuring the books always balance regardless of failures.
package ledger

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Domain errors — typed so callers can use errors.Is rather than string matching.
var (
	// ErrInsufficientFunds is returned when sender balance < amount.
	ErrInsufficientFunds = errors.New("insufficient funds")

	// ErrWalletFrozen blocks all sends and receives on a frozen wallet.
	ErrWalletFrozen = errors.New("wallet is frozen")

	// ErrWalletNotFound means the wallet UUID doesn't exist or is deleted.
	ErrWalletNotFound = errors.New("wallet not found")

	// ErrSelfTransfer prevents a wallet from sending to itself.
	ErrSelfTransfer = errors.New("cannot transfer to the same wallet")

	// ErrAmountZero rejects zero or negative amounts before touching the DB.
	ErrAmountZero = errors.New("amount must be greater than zero")

	// ErrIdempotentReplay signals that this idempotency key was already processed.
	// The caller should return the cached result, not create a new transaction.
	ErrIdempotentReplay = errors.New("idempotency key already used")
)

// TransactionType matches the transaction_type enum in PostgreSQL.
type TransactionType string

const (
	TypeTransfer  TransactionType = "TRANSFER"
	TypeIssuance  TransactionType = "ISSUANCE"
	TypePayment   TransactionType = "PAYMENT" // wallet-to-merchant via QR payment request
	TypeRefund    TransactionType = "REFUND"
	TypeFee       TransactionType = "FEE"
)

// TransactionStatus matches the transaction_status enum in PostgreSQL.
type TransactionStatus string

const (
	StatusPending   TransactionStatus = "PENDING"
	StatusConfirmed TransactionStatus = "CONFIRMED"
	StatusSettled   TransactionStatus = "SETTLED"
	StatusFailed    TransactionStatus = "FAILED"
	StatusRefunded  TransactionStatus = "REFUNDED"
)

// EntryType matches the ledger_entry_type enum in PostgreSQL.
type EntryType string

const (
	EntryDebit  EntryType = "DEBIT"
	EntryCredit EntryType = "CREDIT"
)

// Transaction maps to a row in the transactions table.
type Transaction struct {
	ID                  uuid.UUID
	IdempotencyKey      *string
	Type                TransactionType
	Status              TransactionStatus
	SenderWalletID      *uuid.UUID
	ReceiverWalletID    *uuid.UUID
	AmountCents         int64
	FeeCents            int64
	Reference           *string
	Signature           string
	FailureReason       *string
	ParentTransactionID *uuid.UUID
	CreatedAt           time.Time
	ConfirmedAt         *time.Time
	SettledAt           *time.Time
}

// LedgerEntry maps to a row in the ledger_entries table.
// balance_after is the wallet balance immediately after this entry was posted —
// this gives us a full auditable history without replaying all entries.
type LedgerEntry struct {
	ID            uuid.UUID
	WalletID      uuid.UUID
	TransactionID uuid.UUID
	EntryType     EntryType
	AmountCents   int64
	BalanceAfter  int64
	CreatedAt     time.Time
}

// Wallet is the minimal wallet state the ledger engine needs during a transfer.
// The full Wallet with freeze metadata lives in the wallet package.
type Wallet struct {
	ID       uuid.UUID
	UserID   uuid.UUID
	Balance  int64
	IsFrozen bool
	Currency string
}

// TransferParams holds all inputs for a ledger transfer operation.
type TransferParams struct {
	SenderWalletID   uuid.UUID
	ReceiverWalletID uuid.UUID
	AmountCents      int64
	Reference        string
	IdempotencyKey   string          // empty = no idempotency guard
	Signature        string          // HMAC-SHA256 of the transfer payload
	TxnType          TransactionType // TRANSFER by default; REFUND for reversals
}

// TransferResult is returned after a successful atomic transfer.
type TransferResult struct {
	Transaction     *Transaction
	SenderBalance   int64 // wallet balance after the debit
	ReceiverBalance int64 // wallet balance after the credit
}

// IssueParams holds all inputs for a CBDC issuance (central bank credit, no sender).
type IssueParams struct {
	AdminID        uuid.UUID
	WalletID       uuid.UUID
	AmountCents    int64
	Reason         string
	IdempotencyKey string // required — prevents accidental double-mint
	Signature      string // HMAC of the issuance payload
	IPAddress      string // admin's IP for audit
}

// IssueResult is returned after a successful issuance.
type IssueResult struct {
	IssuanceID  uuid.UUID
	Transaction *Transaction
	NewBalance  int64 // wallet balance after the credit
}

// CreateTransactionParams groups the fields for a new transaction row.
type CreateTransactionParams struct {
	IdempotencyKey      *string
	Type                TransactionType
	SenderWalletID      *uuid.UUID
	ReceiverWalletID    *uuid.UUID
	AmountCents         int64
	Reference           *string
	Signature           string
	ParentTransactionID *uuid.UUID
}

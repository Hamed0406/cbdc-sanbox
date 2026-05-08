package ledger

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cbdc-simulator/backend/pkg/database"
)

// Repository handles all ledger-related database operations.
type Repository struct {
	db *database.Pool
}

// NewRepository creates a new ledger Repository.
func NewRepository(db *database.Pool) *Repository {
	return &Repository{db: db}
}

// BeginTx starts a new database transaction.
// The caller must defer tx.Rollback(ctx) and call tx.Commit(ctx) on success.
func (r *Repository) BeginTx(ctx context.Context) (pgx.Tx, error) {
	return r.db.Begin(ctx)
}

// GetWalletForUpdate fetches a wallet row and acquires a row-level lock.
//
// WHY SELECT FOR UPDATE?
// Without locking, two concurrent transfers from the same wallet could both
// read balance=100, both see 100>=50 so both proceed, and end up at balance=50
// instead of 0 — a classic lost-update / race condition.
// The FOR UPDATE lock prevents any other transaction from reading or writing
// this wallet row until the current transaction commits or rolls back.
func (r *Repository) GetWalletForUpdate(ctx context.Context, tx pgx.Tx, walletID uuid.UUID) (*Wallet, error) {
	w := &Wallet{}
	err := tx.QueryRow(ctx, `
		SELECT id, user_id, balance, is_frozen, currency
		FROM wallets
		WHERE id = $1 AND deleted_at IS NULL
		FOR UPDATE
	`, walletID).Scan(&w.ID, &w.UserID, &w.Balance, &w.IsFrozen, &w.Currency)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrWalletNotFound
		}
		return nil, fmt.Errorf("lock wallet %s: %w", walletID, err)
	}
	return w, nil
}

// CreateTransaction inserts a new transaction row with PENDING status.
// All financial fields are set here; status advances to SETTLED later in the same DB tx.
func (r *Repository) CreateTransaction(ctx context.Context, tx pgx.Tx, p CreateTransactionParams) (*Transaction, error) {
	t := &Transaction{}
	err := tx.QueryRow(ctx, `
		INSERT INTO transactions
			(idempotency_key, type, status, sender_wallet_id, receiver_wallet_id,
			 amount_cents, reference, signature, parent_transaction_id)
		VALUES ($1, $2, 'PENDING', $3, $4, $5, $6, $7, $8)
		RETURNING id, idempotency_key, type, status, sender_wallet_id, receiver_wallet_id,
		          amount_cents, fee_cents, reference, signature, failure_reason,
		          parent_transaction_id, created_at, confirmed_at, settled_at
	`,
		p.IdempotencyKey,
		string(p.Type),
		p.SenderWalletID,
		p.ReceiverWalletID,
		p.AmountCents,
		p.Reference,
		p.Signature,
		p.ParentTransactionID,
	).Scan(
		&t.ID, &t.IdempotencyKey, &t.Type, &t.Status,
		&t.SenderWalletID, &t.ReceiverWalletID,
		&t.AmountCents, &t.FeeCents, &t.Reference, &t.Signature,
		&t.FailureReason, &t.ParentTransactionID,
		&t.CreatedAt, &t.ConfirmedAt, &t.SettledAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create transaction: %w", err)
	}
	return t, nil
}

// CreateLedgerEntry inserts a single DEBIT or CREDIT entry.
// balance_after must be computed by the caller from the post-update wallet balance.
func (r *Repository) CreateLedgerEntry(ctx context.Context, tx pgx.Tx, walletID, txnID uuid.UUID, entryType EntryType, amountCents, balanceAfter int64) (*LedgerEntry, error) {
	e := &LedgerEntry{}
	err := tx.QueryRow(ctx, `
		INSERT INTO ledger_entries (wallet_id, transaction_id, entry_type, amount_cents, balance_after)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, wallet_id, transaction_id, entry_type, amount_cents, balance_after, created_at
	`, walletID, txnID, string(entryType), amountCents, balanceAfter).Scan(
		&e.ID, &e.WalletID, &e.TransactionID, &e.EntryType,
		&e.AmountCents, &e.BalanceAfter, &e.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create ledger entry (%s): %w", entryType, err)
	}
	return e, nil
}

// UpdateWalletBalance applies a balance delta (positive = credit, negative = debit).
// Returns the new balance. The DB constraint wallets_balance_non_negative will
// reject any delta that would push balance below zero, so we get a DB-level
// safety net in addition to our own balance check.
func (r *Repository) UpdateWalletBalance(ctx context.Context, tx pgx.Tx, walletID uuid.UUID, delta int64) (int64, error) {
	var newBalance int64
	err := tx.QueryRow(ctx, `
		UPDATE wallets
		SET balance = balance + $2, updated_at = NOW()
		WHERE id = $1
		RETURNING balance
	`, walletID, delta).Scan(&newBalance)
	if err != nil {
		return 0, fmt.Errorf("update balance wallet %s delta %d: %w", walletID, delta, err)
	}
	return newBalance, nil
}

// SettleTransaction advances a transaction's status to SETTLED.
// PENDING → SETTLED happens atomically in the same DB transaction as the balance updates,
// so we never have a SETTLED transaction with un-updated balances (or vice versa).
func (r *Repository) SettleTransaction(ctx context.Context, tx pgx.Tx, txnID uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		UPDATE transactions
		SET status = 'SETTLED', confirmed_at = NOW(), settled_at = NOW()
		WHERE id = $1
	`, txnID)
	if err != nil {
		return fmt.Errorf("settle transaction %s: %w", txnID, err)
	}
	return nil
}

// GetTransactionByIdempotencyKey checks whether a transaction with this key
// already exists for the given sender wallet.
// Returns the existing transaction if found, or pgx.ErrNoRows if not.
func (r *Repository) GetTransactionByIdempotencyKey(ctx context.Context, senderWalletID uuid.UUID, key string) (*Transaction, error) {
	t := &Transaction{}
	err := r.db.QueryRow(ctx, `
		SELECT id, idempotency_key, type, status, sender_wallet_id, receiver_wallet_id,
		       amount_cents, fee_cents, reference, signature, failure_reason,
		       parent_transaction_id, created_at, confirmed_at, settled_at
		FROM transactions
		WHERE sender_wallet_id = $1 AND idempotency_key = $2
	`, senderWalletID, key).Scan(
		&t.ID, &t.IdempotencyKey, &t.Type, &t.Status,
		&t.SenderWalletID, &t.ReceiverWalletID,
		&t.AmountCents, &t.FeeCents, &t.Reference, &t.Signature,
		&t.FailureReason, &t.ParentTransactionID,
		&t.CreatedAt, &t.ConfirmedAt, &t.SettledAt,
	)
	if err != nil {
		return nil, err // caller checks pgx.ErrNoRows
	}
	return t, nil
}

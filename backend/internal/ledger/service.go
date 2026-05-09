package ledger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ledgerRepository defines all DB operations the Service needs.
// InsertIssuanceRecord is included here so Issue() is fully atomic.
// Using an interface (not *Repository) lets unit tests inject a mock without a real DB.
type ledgerRepository interface {
	BeginTx(ctx context.Context) (pgx.Tx, error)
	GetWalletForUpdate(ctx context.Context, tx pgx.Tx, walletID uuid.UUID) (*Wallet, error)
	CreateTransaction(ctx context.Context, tx pgx.Tx, p CreateTransactionParams) (*Transaction, error)
	CreateLedgerEntry(ctx context.Context, tx pgx.Tx, walletID, txnID uuid.UUID, entryType EntryType, amountCents, balanceAfter int64) (*LedgerEntry, error)
	UpdateWalletBalance(ctx context.Context, tx pgx.Tx, walletID uuid.UUID, delta int64) (int64, error)
	SettleTransaction(ctx context.Context, tx pgx.Tx, txnID uuid.UUID) error
	GetTransactionByIdempotencyKey(ctx context.Context, senderWalletID uuid.UUID, key string) (*Transaction, error)
	InsertIssuanceRecord(ctx context.Context, tx pgx.Tx, adminID, walletID, txnID uuid.UUID, amountCents int64, reason, ipAddress string) (uuid.UUID, error)
}

// Service is the double-entry bookkeeping engine.
// All money movement goes through Transfer — nothing writes to wallets or
// ledger_entries directly. This guarantees every credit has a matching debit.
type Service struct {
	repo ledgerRepository
}

// NewService creates a new ledger Service.
func NewService(repo ledgerRepository) *Service {
	return &Service{repo: repo}
}

// Transfer executes an atomic double-entry transfer between two wallets.
//
// Algorithm:
//  1. Validate inputs before touching the database
//  2. Idempotency check — return cached result if key already used
//  3. Begin PostgreSQL transaction (all-or-nothing)
//  4. Lock both wallet rows with SELECT FOR UPDATE (deadlock-safe order)
//  5. Validate business rules on locked rows (frozen, balance)
//  6. Insert PENDING transaction record
//  7. Debit sender → update balance, insert DEBIT ledger entry
//  8. Credit receiver → update balance, insert CREDIT ledger entry
//  9. Advance transaction status to SETTLED
// 10. Commit — failure at any step rolls back everything
func (s *Service) Transfer(ctx context.Context, p TransferParams) (*TransferResult, error) {
	// ── Input validation (no DB access) ──────────────────────────────────────
	if p.AmountCents <= 0 {
		return nil, ErrAmountZero
	}
	if p.SenderWalletID == p.ReceiverWalletID {
		return nil, ErrSelfTransfer
	}
	if p.TxnType == "" {
		p.TxnType = TypeTransfer
	}

	// ── Idempotency check ────────────────────────────────────────────────────
	// Check BEFORE acquiring any locks to avoid holding locks during a Redis/DB read.
	// If the same key was used before, return the cached transaction immediately.
	if p.IdempotencyKey != "" {
		existing, err := s.repo.GetTransactionByIdempotencyKey(ctx, p.SenderWalletID, p.IdempotencyKey)
		if err == nil {
			// Key already used — return the original result (idempotent replay)
			return &TransferResult{Transaction: existing}, ErrIdempotentReplay
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("idempotency check: %w", err)
		}
	}

	// ── Database transaction ─────────────────────────────────────────────────
	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		// Rollback is a no-op if tx was already committed.
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			slog.Error("ledger tx rollback failed", "error", rbErr)
		}
	}()

	// ── Lock wallets in consistent UUID order (deadlock prevention) ───────────
	// If Alice→Bob and Bob→Alice run concurrently, both would try to lock the
	// same two rows. Without ordering, they could deadlock waiting for each other.
	// Locking the wallet with the lexicographically smaller UUID first ensures
	// both goroutines acquire locks in the same order → no deadlock.
	first, second := orderedLockIDs(p.SenderWalletID, p.ReceiverWalletID)

	firstWallet, err := s.repo.GetWalletForUpdate(ctx, tx, first)
	if err != nil {
		return nil, err
	}
	secondWallet, err := s.repo.GetWalletForUpdate(ctx, tx, second)
	if err != nil {
		return nil, err
	}

	// Map back to sender/receiver regardless of lock order
	var senderWallet, receiverWallet *Wallet
	if firstWallet.ID == p.SenderWalletID {
		senderWallet, receiverWallet = firstWallet, secondWallet
	} else {
		senderWallet, receiverWallet = secondWallet, firstWallet
	}

	// ── Business rule validation (on locked rows) ────────────────────────────
	if senderWallet.IsFrozen {
		return nil, ErrWalletFrozen
	}
	if receiverWallet.IsFrozen {
		return nil, ErrWalletFrozen
	}
	if senderWallet.Balance < p.AmountCents {
		return nil, ErrInsufficientFunds
	}

	// ── Create transaction record (PENDING) ───────────────────────────────────
	var idempotencyKeyPtr *string
	if p.IdempotencyKey != "" {
		idempotencyKeyPtr = &p.IdempotencyKey
	}
	var referencePtr *string
	if p.Reference != "" {
		referencePtr = &p.Reference
	}

	txn, err := s.repo.CreateTransaction(ctx, tx, CreateTransactionParams{
		IdempotencyKey:   idempotencyKeyPtr,
		Type:             p.TxnType,
		SenderWalletID:   &p.SenderWalletID,
		ReceiverWalletID: &p.ReceiverWalletID,
		AmountCents:      p.AmountCents,
		Reference:        referencePtr,
		Signature:        p.Signature,
	})
	if err != nil {
		return nil, err
	}

	// ── Debit sender ──────────────────────────────────────────────────────────
	senderNewBalance, err := s.repo.UpdateWalletBalance(ctx, tx, p.SenderWalletID, -p.AmountCents)
	if err != nil {
		return nil, fmt.Errorf("debit sender: %w", err)
	}
	if _, err := s.repo.CreateLedgerEntry(ctx, tx, p.SenderWalletID, txn.ID, EntryDebit, p.AmountCents, senderNewBalance); err != nil {
		return nil, fmt.Errorf("sender ledger entry: %w", err)
	}

	// ── Credit receiver ───────────────────────────────────────────────────────
	receiverNewBalance, err := s.repo.UpdateWalletBalance(ctx, tx, p.ReceiverWalletID, p.AmountCents)
	if err != nil {
		return nil, fmt.Errorf("credit receiver: %w", err)
	}
	if _, err := s.repo.CreateLedgerEntry(ctx, tx, p.ReceiverWalletID, txn.ID, EntryCredit, p.AmountCents, receiverNewBalance); err != nil {
		return nil, fmt.Errorf("receiver ledger entry: %w", err)
	}

	// ── Settle transaction ────────────────────────────────────────────────────
	if err := s.repo.SettleTransaction(ctx, tx, txn.ID); err != nil {
		return nil, err
	}

	// ── Commit ────────────────────────────────────────────────────────────────
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transfer: %w", err)
	}

	txn.Status = StatusSettled

	return &TransferResult{
		Transaction:     txn,
		SenderBalance:   senderNewBalance,
		ReceiverBalance: receiverNewBalance,
	}, nil
}

// Issue mints new DD$ into a wallet with no sender (central bank issuance).
// The entire operation — transaction record, ledger entry, and cbdc_issuance row —
// commits or rolls back atomically. No partial issuances are possible.
func (s *Service) Issue(ctx context.Context, p IssueParams) (*IssueResult, error) {
	if p.AmountCents <= 0 {
		return nil, ErrAmountZero
	}

	// Begin DB transaction
	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin issuance tx: %w", err)
	}
	defer func() {
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			slog.Error("issuance tx rollback failed", "error", rbErr)
		}
	}()

	// Lock the receiver wallet
	receiverWallet, err := s.repo.GetWalletForUpdate(ctx, tx, p.WalletID)
	if err != nil {
		return nil, err
	}
	if receiverWallet.IsFrozen {
		return nil, ErrWalletFrozen
	}

	// Create ISSUANCE transaction — SenderWalletID is nil (central bank has no wallet)
	txn, err := s.repo.CreateTransaction(ctx, tx, CreateTransactionParams{
		Type:             TypeIssuance,
		ReceiverWalletID: &p.WalletID,
		AmountCents:      p.AmountCents,
		Signature:        p.Signature,
	})
	if err != nil {
		return nil, err
	}

	// Credit the wallet
	newBalance, err := s.repo.UpdateWalletBalance(ctx, tx, p.WalletID, p.AmountCents)
	if err != nil {
		return nil, fmt.Errorf("credit wallet: %w", err)
	}
	if _, err := s.repo.CreateLedgerEntry(ctx, tx, p.WalletID, txn.ID, EntryCredit, p.AmountCents, newBalance); err != nil {
		return nil, fmt.Errorf("issuance ledger entry: %w", err)
	}

	// Settle the transaction
	if err := s.repo.SettleTransaction(ctx, tx, txn.ID); err != nil {
		return nil, err
	}

	// Record in cbdc_issuance within the same DB transaction for full atomicity
	issuanceID, err := s.repo.InsertIssuanceRecord(ctx, tx, p.AdminID, p.WalletID, txn.ID, p.AmountCents, p.Reason, p.IPAddress)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit issuance: %w", err)
	}

	txn.Status = StatusSettled

	return &IssueResult{
		IssuanceID:  issuanceID,
		Transaction: txn,
		NewBalance:  newBalance,
	}, nil
}

// Ledger is the interface exposed to other packages (wallet, payment, admin).
// Using an interface here lets callers be tested without a real DB.
type Ledger interface {
	Transfer(ctx context.Context, p TransferParams) (*TransferResult, error)
	Issue(ctx context.Context, p IssueParams) (*IssueResult, error)
}

// orderedLockIDs returns two UUIDs in consistent lexicographic order.
// Always locking the smaller UUID first prevents deadlocks when two goroutines
// transfer between the same pair of wallets in opposite directions.
func orderedLockIDs(a, b uuid.UUID) (uuid.UUID, uuid.UUID) {
	aStr := a.String()
	bStr := b.String()
	if aStr < bStr {
		return a, b
	}
	return b, a
}

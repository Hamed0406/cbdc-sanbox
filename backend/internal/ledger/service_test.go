package ledger_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cbdc-simulator/backend/internal/ledger"
)

// ── Mock repository ───────────────────────────────────────────────────────────

// mockRepo is an in-memory stub that controls what the ledger service sees.
// The failing* flags let each test inject the exact failure it wants to exercise.
type mockRepo struct {
	sender   *ledger.Wallet
	receiver *ledger.Wallet
	failTx   bool // BeginTx returns an error
	failLock bool // GetWalletForUpdate returns ErrWalletNotFound
}

func (m *mockRepo) BeginTx(_ context.Context) (pgx.Tx, error) {
	if m.failTx {
		return nil, errors.New("db unavailable")
	}
	return &fakeTx{}, nil
}

func (m *mockRepo) GetWalletForUpdate(_ context.Context, _ pgx.Tx, id uuid.UUID) (*ledger.Wallet, error) {
	if m.failLock {
		return nil, ledger.ErrWalletNotFound
	}
	if m.sender != nil && m.sender.ID == id {
		return m.sender, nil
	}
	if m.receiver != nil && m.receiver.ID == id {
		return m.receiver, nil
	}
	return nil, ledger.ErrWalletNotFound
}

func (m *mockRepo) CreateTransaction(_ context.Context, _ pgx.Tx, p ledger.CreateTransactionParams) (*ledger.Transaction, error) {
	txn := &ledger.Transaction{
		ID:               uuid.New(),
		Type:             p.Type,
		Status:           ledger.StatusPending,
		SenderWalletID:   p.SenderWalletID,
		ReceiverWalletID: p.ReceiverWalletID,
		AmountCents:      p.AmountCents,
		Signature:        p.Signature,
	}
	return txn, nil
}

func (m *mockRepo) CreateLedgerEntry(_ context.Context, _ pgx.Tx, walletID, txnID uuid.UUID, et ledger.EntryType, amount, balAfter int64) (*ledger.LedgerEntry, error) {
	return &ledger.LedgerEntry{
		ID: uuid.New(), WalletID: walletID, TransactionID: txnID,
		EntryType: et, AmountCents: amount, BalanceAfter: balAfter,
	}, nil
}

func (m *mockRepo) UpdateWalletBalance(_ context.Context, _ pgx.Tx, id uuid.UUID, delta int64) (int64, error) {
	if m.sender != nil && m.sender.ID == id {
		m.sender.Balance += delta
		return m.sender.Balance, nil
	}
	if m.receiver != nil && m.receiver.ID == id {
		m.receiver.Balance += delta
		return m.receiver.Balance, nil
	}
	return 0, errors.New("wallet not found in mock")
}

func (m *mockRepo) SettleTransaction(_ context.Context, _ pgx.Tx, _ uuid.UUID) error {
	return nil
}

func (m *mockRepo) GetTransactionByIdempotencyKey(_ context.Context, _ uuid.UUID, _ string) (*ledger.Transaction, error) {
	return nil, pgx.ErrNoRows // no existing transaction by default
}

// fakeTx satisfies pgx.Tx with no-op implementations.
// Commit is what matters for the Transfer success path — everything else is unused.
type fakeTx struct{}

func (f *fakeTx) Begin(_ context.Context) (pgx.Tx, error)  { return f, nil }
func (f *fakeTx) Commit(_ context.Context) error            { return nil }
func (f *fakeTx) Rollback(_ context.Context) error          { return pgx.ErrTxClosed } // simulate already-closed on defer
func (f *fakeTx) CopyFrom(_ context.Context, _ pgx.Identifier, _ []string, _ pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (f *fakeTx) SendBatch(_ context.Context, _ *pgx.Batch) pgx.BatchResults { return nil }
func (f *fakeTx) LargeObjects() pgx.LargeObjects                              { return pgx.LargeObjects{} }
func (f *fakeTx) Prepare(_ context.Context, _, _ string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (f *fakeTx) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (f *fakeTx) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) { return nil, nil }
func (f *fakeTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row        { return nil }
func (f *fakeTx) Conn() *pgx.Conn                                                { return nil }

// ── Helpers ───────────────────────────────────────────────────────────────────

func newWallet(balance int64, frozen bool) *ledger.Wallet {
	return &ledger.Wallet{
		ID:       uuid.New(),
		UserID:   uuid.New(),
		Balance:  balance,
		IsFrozen: frozen,
		Currency: "DD$",
	}
}

// ── Validation tests (no DB needed) ──────────────────────────────────────────

func TestTransfer_ZeroAmountRejected(t *testing.T) {
	svc := ledger.NewService(&mockRepo{})
	_, err := svc.Transfer(context.Background(), ledger.TransferParams{
		SenderWalletID:   uuid.New(),
		ReceiverWalletID: uuid.New(),
		AmountCents:      0,
	})
	if !errors.Is(err, ledger.ErrAmountZero) {
		t.Errorf("expected ErrAmountZero, got %v", err)
	}
}

func TestTransfer_NegativeAmountRejected(t *testing.T) {
	svc := ledger.NewService(&mockRepo{})
	_, err := svc.Transfer(context.Background(), ledger.TransferParams{
		SenderWalletID:   uuid.New(),
		ReceiverWalletID: uuid.New(),
		AmountCents:      -500,
	})
	if !errors.Is(err, ledger.ErrAmountZero) {
		t.Errorf("expected ErrAmountZero, got %v", err)
	}
}

func TestTransfer_SelfTransferRejected(t *testing.T) {
	svc := ledger.NewService(&mockRepo{})
	id := uuid.New()
	_, err := svc.Transfer(context.Background(), ledger.TransferParams{
		SenderWalletID:   id,
		ReceiverWalletID: id,
		AmountCents:      100,
	})
	if !errors.Is(err, ledger.ErrSelfTransfer) {
		t.Errorf("expected ErrSelfTransfer, got %v", err)
	}
}

// ── Business rule tests (mock repo controls wallet state) ────────────────────

func TestTransfer_InsufficientFunds(t *testing.T) {
	sender := newWallet(500, false)   // DD$5.00
	receiver := newWallet(0, false)
	repo := &mockRepo{sender: sender, receiver: receiver}
	svc := ledger.NewService(repo)

	_, err := svc.Transfer(context.Background(), ledger.TransferParams{
		SenderWalletID:   sender.ID,
		ReceiverWalletID: receiver.ID,
		AmountCents:      1000, // DD$10.00 > DD$5.00
	})
	if !errors.Is(err, ledger.ErrInsufficientFunds) {
		t.Errorf("expected ErrInsufficientFunds, got %v", err)
	}
}

func TestTransfer_FrozenSenderRejected(t *testing.T) {
	sender := newWallet(10000, true) // frozen
	receiver := newWallet(0, false)
	repo := &mockRepo{sender: sender, receiver: receiver}
	svc := ledger.NewService(repo)

	_, err := svc.Transfer(context.Background(), ledger.TransferParams{
		SenderWalletID:   sender.ID,
		ReceiverWalletID: receiver.ID,
		AmountCents:      100,
	})
	if !errors.Is(err, ledger.ErrWalletFrozen) {
		t.Errorf("expected ErrWalletFrozen, got %v", err)
	}
}

func TestTransfer_FrozenReceiverRejected(t *testing.T) {
	sender := newWallet(10000, false)
	receiver := newWallet(0, true) // frozen
	repo := &mockRepo{sender: sender, receiver: receiver}
	svc := ledger.NewService(repo)

	_, err := svc.Transfer(context.Background(), ledger.TransferParams{
		SenderWalletID:   sender.ID,
		ReceiverWalletID: receiver.ID,
		AmountCents:      100,
	})
	if !errors.Is(err, ledger.ErrWalletFrozen) {
		t.Errorf("expected ErrWalletFrozen, got %v", err)
	}
}

func TestTransfer_SuccessUpdatesBalances(t *testing.T) {
	sender := newWallet(10000, false)  // DD$100.00
	receiver := newWallet(5000, false) // DD$50.00
	repo := &mockRepo{sender: sender, receiver: receiver}
	svc := ledger.NewService(repo)

	result, err := svc.Transfer(context.Background(), ledger.TransferParams{
		SenderWalletID:   sender.ID,
		ReceiverWalletID: receiver.ID,
		AmountCents:      2500, // DD$25.00
		Reference:        "Lunch split",
		Signature:        "test-sig",
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if result.SenderBalance != 7500 {
		t.Errorf("sender balance: want 7500, got %d", result.SenderBalance)
	}
	if result.ReceiverBalance != 7500 {
		t.Errorf("receiver balance: want 7500, got %d", result.ReceiverBalance)
	}
	if result.Transaction.Status != ledger.StatusSettled {
		t.Errorf("transaction should be SETTLED, got %s", result.Transaction.Status)
	}
}

func TestTransfer_ExactBalanceSucceeds(t *testing.T) {
	sender := newWallet(2500, false)  // exactly DD$25.00
	receiver := newWallet(0, false)
	repo := &mockRepo{sender: sender, receiver: receiver}
	svc := ledger.NewService(repo)

	result, err := svc.Transfer(context.Background(), ledger.TransferParams{
		SenderWalletID:   sender.ID,
		ReceiverWalletID: receiver.ID,
		AmountCents:      2500, // spend entire balance
		Signature:        "sig",
	})
	if err != nil {
		t.Fatalf("transfer of exact balance should succeed: %v", err)
	}
	if result.SenderBalance != 0 {
		t.Errorf("sender should have 0 balance, got %d", result.SenderBalance)
	}
}

func TestTransfer_IdempotentReplayReturnsCachedTransaction(t *testing.T) {
	existingTxn := &ledger.Transaction{
		ID:          uuid.New(),
		Type:        ledger.TypeTransfer,
		Status:      ledger.StatusSettled,
		AmountCents: 1000,
	}

	repo := &mockRepo{}
	// Override GetTransactionByIdempotencyKey to return an existing transaction
	repoWithExisting := &mockRepoWithIdempotency{mockRepo: repo, existing: existingTxn}
	svc := ledger.NewService(repoWithExisting)

	_, err := svc.Transfer(context.Background(), ledger.TransferParams{
		SenderWalletID:   uuid.New(),
		ReceiverWalletID: uuid.New(),
		AmountCents:      1000,
		IdempotencyKey:   "key-abc-123",
	})
	if !errors.Is(err, ledger.ErrIdempotentReplay) {
		t.Errorf("expected ErrIdempotentReplay, got %v", err)
	}
}

// mockRepoWithIdempotency extends mockRepo to return a pre-existing transaction.
type mockRepoWithIdempotency struct {
	*mockRepo
	existing *ledger.Transaction
}

func (m *mockRepoWithIdempotency) GetTransactionByIdempotencyKey(_ context.Context, _ uuid.UUID, _ string) (*ledger.Transaction, error) {
	return m.existing, nil
}

package payment_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/cbdc-simulator/backend/internal/ledger"
	"github.com/cbdc-simulator/backend/internal/payment"
)

// ── Mock repository ───────────────────────────────────────────────────────────

type mockRepo struct {
	detail *payment.TransactionDetail
	list   []payment.TransactionDetail
	total  int
	err    error
}

func (m *mockRepo) GetByID(_ context.Context, _, _ uuid.UUID) (*payment.TransactionDetail, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.detail, nil
}

func (m *mockRepo) List(_ context.Context, _ uuid.UUID, _ payment.ListParams) ([]payment.TransactionDetail, int, error) {
	if m.err != nil {
		return nil, 0, m.err
	}
	return m.list, m.total, nil
}

// ── Mock ledger ───────────────────────────────────────────────────────────────

type mockLedger struct {
	result *ledger.TransferResult
	err    error
}

func (m *mockLedger) Transfer(_ context.Context, _ ledger.TransferParams) (*ledger.TransferResult, error) {
	return m.result, m.err
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func newSvc(repo *mockRepo, l *mockLedger) *payment.Service {
	return payment.NewService(repo, l, nil, nil, "test-key")
}

func validSendReq(toWalletID string) payment.SendRequest {
	return payment.SendRequest{
		ToWalletID:  toWalletID,
		AmountCents: 1000,
		Reference:   "test",
	}
}

func newTransferResult(senderBalance, receiverBalance int64) *ledger.TransferResult {
	txnID := uuid.New()
	senderID := uuid.New()
	receiverID := uuid.New()
	return &ledger.TransferResult{
		Transaction: &ledger.Transaction{
			ID:               txnID,
			Type:             ledger.TypeTransfer,
			Status:           ledger.StatusSettled,
			SenderWalletID:   &senderID,
			ReceiverWalletID: &receiverID,
			AmountCents:      1000,
			Signature:        "sig",
		},
		SenderBalance:   senderBalance,
		ReceiverBalance: receiverBalance,
	}
}

// ── Send tests ────────────────────────────────────────────────────────────────

func TestSend_MissingIdempotencyKey(t *testing.T) {
	svc := newSvc(&mockRepo{}, &mockLedger{})
	_, err := svc.Send(context.Background(),
		validSendReq(uuid.New().String()),
		uuid.New(), uuid.New(),
		"", // missing key
		"127.0.0.1",
	)
	if !errors.Is(err, payment.ErrMissingIdempotencyKey) {
		t.Errorf("expected ErrMissingIdempotencyKey, got %v", err)
	}
}

func TestSend_ZeroAmount(t *testing.T) {
	svc := newSvc(&mockRepo{}, &mockLedger{})
	req := payment.SendRequest{ToWalletID: uuid.New().String(), AmountCents: 0}
	_, err := svc.Send(context.Background(), req, uuid.New(), uuid.New(), "key-1", "127.0.0.1")
	if !errors.Is(err, payment.ErrAmountZero) {
		t.Errorf("expected ErrAmountZero, got %v", err)
	}
}

func TestSend_ReferenceTooLong(t *testing.T) {
	svc := newSvc(&mockRepo{}, &mockLedger{})
	ref := make([]byte, 257)
	for i := range ref {
		ref[i] = 'x'
	}
	req := payment.SendRequest{ToWalletID: uuid.New().String(), AmountCents: 100, Reference: string(ref)}
	_, err := svc.Send(context.Background(), req, uuid.New(), uuid.New(), "key-1", "127.0.0.1")
	if !errors.Is(err, payment.ErrReferenceTooLong) {
		t.Errorf("expected ErrReferenceTooLong, got %v", err)
	}
}

func TestSend_SelfPayment(t *testing.T) {
	svc := newSvc(&mockRepo{}, &mockLedger{})
	walletID := uuid.New()
	req := validSendReq(walletID.String())
	_, err := svc.Send(context.Background(), req, walletID, uuid.New(), "key-1", "127.0.0.1")
	if !errors.Is(err, payment.ErrSelfPayment) {
		t.Errorf("expected ErrSelfPayment, got %v", err)
	}
}

func TestSend_InsufficientFunds(t *testing.T) {
	l := &mockLedger{err: ledger.ErrInsufficientFunds}
	svc := newSvc(&mockRepo{}, l)
	_, err := svc.Send(context.Background(),
		validSendReq(uuid.New().String()),
		uuid.New(), uuid.New(), "key-1", "127.0.0.1",
	)
	if !errors.Is(err, ledger.ErrInsufficientFunds) {
		t.Errorf("expected ErrInsufficientFunds, got %v", err)
	}
}

func TestSend_WalletFrozen(t *testing.T) {
	l := &mockLedger{err: ledger.ErrWalletFrozen}
	svc := newSvc(&mockRepo{}, l)
	_, err := svc.Send(context.Background(),
		validSendReq(uuid.New().String()),
		uuid.New(), uuid.New(), "key-1", "127.0.0.1",
	)
	if !errors.Is(err, ledger.ErrWalletFrozen) {
		t.Errorf("expected ErrWalletFrozen, got %v", err)
	}
}

func TestSend_Success(t *testing.T) {
	transferResult := newTransferResult(8000, 2000)
	senderID := *transferResult.Transaction.SenderWalletID

	detail := &payment.TransactionDetail{
		ID:          transferResult.Transaction.ID.String(),
		Type:        "TRANSFER",
		Status:      "SETTLED",
		Direction:   "DEBIT",
		AmountCents: 1000,
	}
	repo := &mockRepo{detail: detail}
	l := &mockLedger{result: transferResult}
	svc := newSvc(repo, l)

	resp, err := svc.Send(context.Background(),
		validSendReq(uuid.New().String()),
		senderID, uuid.New(), "key-1", "127.0.0.1",
	)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if resp.NewBalanceCents != 8000 {
		t.Errorf("expected sender balance 8000, got %d", resp.NewBalanceCents)
	}
	if resp.Transaction.Status != "SETTLED" {
		t.Errorf("expected SETTLED, got %s", resp.Transaction.Status)
	}
}

func TestSend_LedgerIdempotentReplayFallsBackToDBDetail(t *testing.T) {
	transferResult := newTransferResult(5000, 5000)
	senderID := *transferResult.Transaction.SenderWalletID

	detail := &payment.TransactionDetail{
		ID:          transferResult.Transaction.ID.String(),
		AmountCents: 1000,
		Direction:   "DEBIT",
		Status:      "SETTLED",
	}
	repo := &mockRepo{detail: detail}
	l := &mockLedger{result: transferResult, err: ledger.ErrIdempotentReplay}
	svc := newSvc(repo, l)

	resp, err := svc.Send(context.Background(),
		validSendReq(uuid.New().String()),
		senderID, uuid.New(), "key-dup", "127.0.0.1",
	)
	if err != nil {
		t.Fatalf("idempotent replay should not error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response, got nil")
	}
}

// ── GetTransaction tests ──────────────────────────────────────────────────────

func TestGetTransaction_NotFound(t *testing.T) {
	repo := &mockRepo{err: payment.ErrTransactionNotFound}
	svc := newSvc(repo, &mockLedger{})
	_, err := svc.GetTransaction(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, payment.ErrTransactionNotFound) {
		t.Errorf("expected ErrTransactionNotFound, got %v", err)
	}
}

func TestGetTransaction_Success(t *testing.T) {
	txnID := uuid.New()
	d := &payment.TransactionDetail{ID: txnID.String(), AmountCents: 500, Status: "SETTLED"}
	repo := &mockRepo{detail: d}
	svc := newSvc(repo, &mockLedger{})

	got, err := svc.GetTransaction(context.Background(), txnID, uuid.New())
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if got.ID != txnID.String() {
		t.Errorf("expected txn ID %s, got %s", txnID, got.ID)
	}
}

// ── ListTransactions tests ────────────────────────────────────────────────────

func TestListTransactions_ReturnsEmptySlice(t *testing.T) {
	repo := &mockRepo{list: []payment.TransactionDetail{}, total: 0}
	svc := newSvc(repo, &mockLedger{})

	result, err := svc.ListTransactions(context.Background(), uuid.New(), payment.ListParams{Page: 1, Limit: 20})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if len(result.Transactions) != 0 {
		t.Errorf("expected empty list, got %d items", len(result.Transactions))
	}
	if result.Pagination.Total != 0 {
		t.Errorf("expected total 0, got %d", result.Pagination.Total)
	}
}

func TestListTransactions_PaginationCalculation(t *testing.T) {
	txns := make([]payment.TransactionDetail, 5)
	repo := &mockRepo{list: txns, total: 25}
	svc := newSvc(repo, &mockLedger{})

	result, err := svc.ListTransactions(context.Background(), uuid.New(), payment.ListParams{Page: 2, Limit: 10})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if result.Pagination.Pages != 3 {
		t.Errorf("expected 3 pages, got %d", result.Pagination.Pages)
	}
	if result.Pagination.Page != 2 {
		t.Errorf("expected page 2, got %d", result.Pagination.Page)
	}
}

// ensure mockRepo satisfies the internal repository interface (compile-time check)
var _ interface {
	GetByID(ctx context.Context, txnID, walletID uuid.UUID) (*payment.TransactionDetail, error)
	List(ctx context.Context, walletID uuid.UUID, p payment.ListParams) ([]payment.TransactionDetail, int, error)
} = (*mockRepo)(nil)

// ensure mockLedger satisfies the interface (compile-time check)
var _ interface {
	Transfer(ctx context.Context, p ledger.TransferParams) (*ledger.TransferResult, error)
} = (*mockLedger)(nil)


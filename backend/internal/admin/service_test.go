package admin_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/cbdc-simulator/backend/internal/admin"
	"github.com/cbdc-simulator/backend/internal/audit"
	"github.com/cbdc-simulator/backend/internal/ledger"
)

// ── Mock ledger ───────────────────────────────────────────────────────────────

type mockLedger struct {
	result *ledger.IssueResult
	err    error
}

func (m *mockLedger) Issue(_ context.Context, _ ledger.IssueParams) (*ledger.IssueResult, error) {
	return m.result, m.err
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func newService(l *mockLedger) *admin.Service {
	// nil idempotency store and audit service — safe because unit tests
	// don't reach the Redis/DB calls when validation fails early.
	auditSvc := audit.NewServiceWithLogger(nil) // test-safe: nil db, no-op audit
	return admin.NewService(l, nil, auditSvc, "test-signing-key")
}

func successResult() *ledger.IssueResult {
	txn := &ledger.Transaction{
		ID:          uuid.New(),
		Type:        ledger.TypeIssuance,
		Status:      ledger.StatusSettled,
		AmountCents: 100_00,
	}
	return &ledger.IssueResult{
		IssuanceID:  uuid.New(),
		Transaction: txn,
		NewBalance:  100_00,
	}
}

// ── Validation tests ──────────────────────────────────────────────────────────

func TestIssueCBDC_ZeroAmountRejected(t *testing.T) {
	svc := newService(&mockLedger{})
	_, err := svc.IssueCBDC(context.Background(), admin.IssueRequest{
		WalletID:    uuid.New().String(),
		AmountCents: 0,
		Reason:      "Test issuance reason",
	}, uuid.New(), "key-1", "127.0.0.1")
	if !errors.Is(err, admin.ErrAmountZero) {
		t.Errorf("expected ErrAmountZero, got %v", err)
	}
}

func TestIssueCBDC_NegativeAmountRejected(t *testing.T) {
	svc := newService(&mockLedger{})
	_, err := svc.IssueCBDC(context.Background(), admin.IssueRequest{
		WalletID:    uuid.New().String(),
		AmountCents: -500,
		Reason:      "Test issuance reason",
	}, uuid.New(), "key-1", "127.0.0.1")
	if !errors.Is(err, admin.ErrAmountZero) {
		t.Errorf("expected ErrAmountZero, got %v", err)
	}
}

func TestIssueCBDC_ExceedsMaxRejected(t *testing.T) {
	svc := newService(&mockLedger{})
	_, err := svc.IssueCBDC(context.Background(), admin.IssueRequest{
		WalletID:    uuid.New().String(),
		AmountCents: 100_000_001, // over DD$1M
		Reason:      "Test issuance reason",
	}, uuid.New(), "key-1", "127.0.0.1")
	if !errors.Is(err, admin.ErrAmountTooLarge) {
		t.Errorf("expected ErrAmountTooLarge, got %v", err)
	}
}

func TestIssueCBDC_ShortReasonRejected(t *testing.T) {
	svc := newService(&mockLedger{})
	_, err := svc.IssueCBDC(context.Background(), admin.IssueRequest{
		WalletID:    uuid.New().String(),
		AmountCents: 1000,
		Reason:      "short", // < 10 chars
	}, uuid.New(), "key-1", "127.0.0.1")
	if !errors.Is(err, admin.ErrReasonTooShort) {
		t.Errorf("expected ErrReasonTooShort, got %v", err)
	}
}

func TestIssueCBDC_MissingIdempotencyKeyRejected(t *testing.T) {
	svc := newService(&mockLedger{})
	_, err := svc.IssueCBDC(context.Background(), admin.IssueRequest{
		WalletID:    uuid.New().String(),
		AmountCents: 1000,
		Reason:      "Valid reason for issuance",
	}, uuid.New(), "", "127.0.0.1") // empty key
	if !errors.Is(err, admin.ErrMissingIdempotencyKey) {
		t.Errorf("expected ErrMissingIdempotencyKey, got %v", err)
	}
}

func TestIssueCBDC_InvalidWalletIDRejected(t *testing.T) {
	svc := newService(&mockLedger{})
	_, err := svc.IssueCBDC(context.Background(), admin.IssueRequest{
		WalletID:    "not-a-uuid",
		AmountCents: 1000,
		Reason:      "Valid reason for issuance",
	}, uuid.New(), "key-1", "127.0.0.1")
	if err == nil {
		t.Error("expected error for invalid wallet UUID, got nil")
	}
}

func TestIssueCBDC_WalletFrozenPropagated(t *testing.T) {
	svc := newService(&mockLedger{err: ledger.ErrWalletFrozen})
	_, err := svc.IssueCBDC(context.Background(), admin.IssueRequest{
		WalletID:    uuid.New().String(),
		AmountCents: 5000,
		Reason:      "Valid reason for issuance",
	}, uuid.New(), "key-1", "127.0.0.1")
	if !errors.Is(err, ledger.ErrWalletFrozen) {
		t.Errorf("expected ErrWalletFrozen, got %v", err)
	}
}

func TestIssueCBDC_SuccessReturnsCorrectBalances(t *testing.T) {
	result := successResult()
	result.NewBalance = 500_00 // DD$500.00 after issuance
	svc := newService(&mockLedger{result: result})

	resp, err := svc.IssueCBDC(context.Background(), admin.IssueRequest{
		WalletID:    uuid.New().String(),
		AmountCents: 500_00,
		Reason:      "Initial demo funding for test wallet",
	}, uuid.New(), "key-1", "127.0.0.1")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if resp.NewBalanceCents != 500_00 {
		t.Errorf("expected new balance 50000, got %d", resp.NewBalanceCents)
	}
	if resp.NewBalanceDisplay != "DD$ 500.00" {
		t.Errorf("expected 'DD$ 500.00', got %q", resp.NewBalanceDisplay)
	}
	if resp.Issuance.AmountDisplay != "DD$ 500.00" {
		t.Errorf("expected issuance display 'DD$ 500.00', got %q", resp.Issuance.AmountDisplay)
	}
}

func TestIssueCBDC_MaxAmountAllowed(t *testing.T) {
	result := successResult()
	result.Transaction.AmountCents = 100_000_000
	result.NewBalance = 100_000_000
	svc := newService(&mockLedger{result: result})

	_, err := svc.IssueCBDC(context.Background(), admin.IssueRequest{
		WalletID:    uuid.New().String(),
		AmountCents: 100_000_000, // exactly DD$1M — should pass
		Reason:      "Maximum allowed issuance amount test",
	}, uuid.New(), "key-1", "127.0.0.1")
	if err != nil {
		t.Errorf("exact max amount should succeed, got %v", err)
	}
}

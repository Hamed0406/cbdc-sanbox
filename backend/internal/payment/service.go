package payment

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/cbdc-simulator/backend/internal/audit"
	"github.com/cbdc-simulator/backend/internal/ledger"
	ws "github.com/cbdc-simulator/backend/internal/websocket"
	"github.com/cbdc-simulator/backend/pkg/crypto"
	"github.com/cbdc-simulator/backend/pkg/currency"
	"github.com/cbdc-simulator/backend/pkg/idempotency"
)

// repository is the read-only DB interface the service needs.
type repository interface {
	GetByID(ctx context.Context, txnID, walletID uuid.UUID) (*TransactionDetail, error)
	List(ctx context.Context, walletID uuid.UUID, p ListParams) ([]TransactionDetail, int, error)
}

// ledgerTransferer is the narrow interface into the ledger engine.
type ledgerTransferer interface {
	Transfer(ctx context.Context, p ledger.TransferParams) (*ledger.TransferResult, error)
}

// Service orchestrates P2P payment flows.
type Service struct {
	repo       repository
	ledger     ledgerTransferer
	idempotent *idempotency.Store
	audit      *audit.Service
	publisher  ws.Publisher // nil-safe; nil in unit tests that don't need WebSocket events
	signingKey string
}

// NewService creates a new payment Service.
func NewService(repo repository, l ledgerTransferer, idempotent *idempotency.Store, auditSvc *audit.Service, pub ws.Publisher, signingKey string) *Service {
	return &Service{
		repo:       repo,
		ledger:     l,
		idempotent: idempotent,
		audit:      auditSvc,
		publisher:  pub,
		signingKey: signingKey,
	}
}

// Send executes a P2P payment from senderWalletID to req.ToWalletID.
//
// Flow:
//  1. Validate inputs (amount, reference length, self-payment check)
//  2. Check idempotency key in Redis — return cached response on replay
//  3. Sign the payment payload with HMAC-SHA256 for tamper-evidence
//  4. Execute atomic double-entry transfer via ledger engine
//  5. Fetch full transaction detail (counterparty name) from DB
//  6. Cache response in Redis for future idempotency replays
//  7. Write audit log entry
func (s *Service) Send(ctx context.Context, req SendRequest, senderWalletID, userID uuid.UUID, idempotencyKey, ip string) (*SendResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if idempotencyKey == "" {
		return nil, ErrMissingIdempotencyKey
	}

	toWalletID, err := uuid.Parse(req.ToWalletID)
	if err != nil {
		return nil, fmt.Errorf("invalid to_wallet_id: %w", err)
	}
	if senderWalletID == toWalletID {
		return nil, ErrSelfPayment
	}

	// Idempotency check (Redis) — fast path avoids DB entirely on replay
	if s.idempotent != nil {
		cached, hit, err := s.idempotent.Check(ctx, senderWalletID.String(), idempotencyKey)
		if err == nil && hit {
			var resp SendResponse
			if err := cached.UnmarshalBody(&resp); err == nil {
				return &resp, nil
			}
		}
	}

	// Sign the payment payload — binds sender + receiver + amount together.
	// Stored in the transaction row; tampering any field invalidates the sig.
	sig := crypto.SignTransaction(
		s.signingKey,
		"payment",
		senderWalletID.String(),
		toWalletID.String(),
		req.AmountCents,
		time.Now().Unix(),
	)

	result, transferErr := s.ledger.Transfer(ctx, ledger.TransferParams{
		SenderWalletID:   senderWalletID,
		ReceiverWalletID: toWalletID,
		AmountCents:      req.AmountCents,
		Reference:        req.Reference,
		IdempotencyKey:   idempotencyKey,
		Signature:        sig,
	})
	if transferErr != nil && !errors.Is(transferErr, ledger.ErrIdempotentReplay) {
		return nil, transferErr
	}

	// Fetch full detail including counterparty name from DB
	txnDetail, fetchErr := s.repo.GetByID(ctx, result.Transaction.ID, senderWalletID)
	if fetchErr != nil {
		// Fall back to building detail from the ledger result without counterparty name
		txnDetail = detailFromLedger(result.Transaction, senderWalletID)
	}

	resp := &SendResponse{
		Transaction:       *txnDetail,
		NewBalanceCents:   result.SenderBalance,
		NewBalanceDisplay: currency.Format(result.SenderBalance, "DD$"),
	}

	// Cache for idempotency replay (only on first execution, not re-replays)
	if s.idempotent != nil && !errors.Is(transferErr, ledger.ErrIdempotentReplay) {
		_ = s.idempotent.Store(ctx, senderWalletID.String(), idempotencyKey, 201, resp)
	}

	// Audit log — fire-and-forget; never fail the payment because of a logging error.
	// s.audit is nil in unit tests that don't wire a real audit service.
	txnID := result.Transaction.ID
	if s.audit != nil {
		s.audit.Log(ctx, audit.LogParams{
			ActorID:      &userID,
			ActorRole:    "user",
			Action:       audit.ActionPaymentSend,
			ResourceType: "transaction",
			ResourceID:   &txnID,
			IPAddress:    ip,
			Metadata: map[string]any{
				"amount_cents":       req.AmountCents,
				"sender_wallet_id":   senderWalletID.String(),
				"receiver_wallet_id": toWalletID.String(),
				"reference":          req.Reference,
			},
			Success: true,
		})
	}

	// WebSocket live events — push DEBIT to sender, CREDIT to receiver.
	// Fire-and-forget: never fail a confirmed payment because a notification failed.
	// s.publisher is nil in unit tests and when no clients are connected.
	if s.publisher != nil && !errors.Is(transferErr, ledger.ErrIdempotentReplay) {
		ref := txnDetail.Reference
		cp := txnDetail.CounterpartyName
		now := time.Now()

		_ = s.publisher.Publish(ctx, senderWalletID, ws.Event{
			Type:      ws.TypePaymentSent,
			WalletID:  senderWalletID.String(),
			Timestamp: now,
			Payload: ws.PaymentEventPayload{
				TransactionID:     txnID.String(),
				Direction:         "DEBIT",
				AmountCents:       req.AmountCents,
				AmountDisplay:     currency.Format(req.AmountCents, "DD$"),
				CounterpartyName:  cp,
				Reference:         ref,
				NewBalanceCents:   result.SenderBalance,
				NewBalanceDisplay: currency.Format(result.SenderBalance, "DD$"),
			},
		})
		_ = s.publisher.Publish(ctx, toWalletID, ws.Event{
			Type:      ws.TypePaymentReceived,
			WalletID:  toWalletID.String(),
			Timestamp: now,
			Payload: ws.PaymentEventPayload{
				TransactionID:     txnID.String(),
				Direction:         "CREDIT",
				AmountCents:       req.AmountCents,
				AmountDisplay:     currency.Format(req.AmountCents, "DD$"),
				CounterpartyName:  cp,
				Reference:         ref,
				NewBalanceCents:   result.ReceiverBalance,
				NewBalanceDisplay: currency.Format(result.ReceiverBalance, "DD$"),
			},
		})
	}

	return resp, nil
}

// GetTransaction fetches a single transaction, verifying walletID is a party.
func (s *Service) GetTransaction(ctx context.Context, txnID, walletID uuid.UUID) (*TransactionDetail, error) {
	d, err := s.repo.GetByID(ctx, txnID, walletID)
	if err != nil {
		return nil, err
	}
	return d, nil
}

// ListTransactions returns paginated payment history for a wallet.
func (s *Service) ListTransactions(ctx context.Context, walletID uuid.UUID, p ListParams) (*PaymentListResponse, error) {
	txns, total, err := s.repo.List(ctx, walletID, p)
	if err != nil {
		return nil, fmt.Errorf("list transactions: %w", err)
	}

	pages := int(math.Ceil(float64(total) / float64(p.Limit)))
	return &PaymentListResponse{
		Transactions: txns,
		Pagination: Pagination{
			Page:  p.Page,
			Limit: p.Limit,
			Total: total,
			Pages: pages,
		},
	}, nil
}

// detailFromLedger builds a TransactionDetail from a ledger result when the
// DB fetch for counterparty name fails. Direction is always DEBIT for the sender.
func detailFromLedger(t *ledger.Transaction, senderWalletID uuid.UUID) *TransactionDetail {
	d := &TransactionDetail{
		ID:            t.ID.String(),
		Type:          string(t.Type),
		Status:        string(t.Status),
		Direction:     "DEBIT",
		AmountCents:   t.AmountCents,
		AmountDisplay: currency.Format(t.AmountCents, "DD$"),
		FeeCents:      t.FeeCents,
		Signature:     t.Signature,
		CreatedAt:     t.CreatedAt.UTC().Format(time.RFC3339),
	}
	if t.SenderWalletID != nil {
		s := t.SenderWalletID.String()
		d.SenderWalletID = &s
	}
	if t.ReceiverWalletID != nil {
		s := t.ReceiverWalletID.String()
		d.ReceiverWalletID = &s
	}
	d.Reference = t.Reference
	if t.SettledAt != nil {
		s := t.SettledAt.UTC().Format(time.RFC3339)
		d.SettledAt = &s
	}
	return d
}

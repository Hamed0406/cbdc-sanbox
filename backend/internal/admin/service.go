package admin

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cbdc-simulator/backend/internal/audit"
	"github.com/cbdc-simulator/backend/internal/ledger"
	ws "github.com/cbdc-simulator/backend/internal/websocket"
	"github.com/cbdc-simulator/backend/pkg/currency"
	"github.com/cbdc-simulator/backend/pkg/crypto"
	"github.com/cbdc-simulator/backend/pkg/idempotency"
)

// ledgerIssuer is the subset of ledger.Ledger the admin service needs.
// Keeping it narrow makes the mock in tests simpler.
type ledgerIssuer interface {
	Issue(ctx context.Context, p ledger.IssueParams) (*ledger.IssueResult, error)
}

// Service contains all admin business logic.
type Service struct {
	ledger     ledgerIssuer
	idempotent *idempotency.Store
	audit      *audit.Service
	publisher  ws.Publisher // nil-safe; nil in unit tests
	signingKey string       // HMAC key for signing issuance payloads
}

// NewService creates a new admin Service.
func NewService(l ledgerIssuer, idempotent *idempotency.Store, auditSvc *audit.Service, pub ws.Publisher, signingKey string) *Service {
	return &Service{
		ledger:     l,
		idempotent: idempotent,
		audit:      auditSvc,
		publisher:  pub,
		signingKey: signingKey,
	}
}

// IssueCBDC mints new DD$ into the target wallet.
//
// Flow:
//  1. Validate request fields
//  2. Check idempotency key (Redis) — return cached result on replay
//  3. Sign the issuance payload for tamper-evidence
//  4. Call ledger.Issue (atomic: transaction + ledger entry + cbdc_issuance row)
//  5. Cache the response in Redis for idempotency replay
//  6. Audit log the issuance
func (s *Service) IssueCBDC(ctx context.Context, req IssueRequest, adminID uuid.UUID, idempotencyKey, ip string) (*IssuanceResponse, error) {
	// ── Validate inputs ───────────────────────────────────────────────────────
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if idempotencyKey == "" {
		return nil, ErrMissingIdempotencyKey
	}

	walletID, err := uuid.Parse(req.WalletID)
	if err != nil {
		return nil, fmt.Errorf("invalid wallet_id: %w", err)
	}

	// ── Idempotency check (Redis) ─────────────────────────────────────────────
	// Key scope: adminID ensures one admin can't replay another admin's key.
	// The ledger also records the transaction, but Redis lets us return the
	// exact same HTTP response without hitting the DB again.
	// Guard: idempotent may be nil in unit tests that skip Redis.
	if s.idempotent != nil {
		cached, hit, err := s.idempotent.Check(ctx, adminID.String(), idempotencyKey)
		if err == nil && hit {
			var resp IssuanceResponse
			if err := cached.UnmarshalBody(&resp); err == nil {
				return &resp, nil
			}
		}
	}

	// ── Sign the issuance payload ─────────────────────────────────────────────
	// Signature binds adminID + walletID + amount together.
	// Any tampering of these fields in the DB will make the signature invalid.
	sig := crypto.SignTransaction(
		s.signingKey,
		"issuance",
		adminID.String(),
		walletID.String(),
		req.AmountCents,
		0, // timestamp not included here; created_at in DB serves that role
	)

	// ── Execute issuance ──────────────────────────────────────────────────────
	result, err := s.ledger.Issue(ctx, ledger.IssueParams{
		AdminID:     adminID,
		WalletID:    walletID,
		AmountCents: req.AmountCents,
		Reason:      req.Reason,
		Signature:   sig,
		IPAddress:   ip,
	})
	if err != nil {
		return nil, err
	}

	// ── Build response ────────────────────────────────────────────────────────
	resp := &IssuanceResponse{
		Issuance: IssuanceDetail{
			ID:            result.IssuanceID.String(),
			WalletID:      walletID.String(),
			AmountCents:   req.AmountCents,
			AmountDisplay: currency.Format(req.AmountCents, "DD$"),
			Reason:        req.Reason,
			TransactionID: result.Transaction.ID.String(),
			CreatedAt:     result.Transaction.CreatedAt,
		},
		NewBalanceCents:   result.NewBalance,
		NewBalanceDisplay: currency.Format(result.NewBalance, "DD$"),
	}

	// ── Cache for idempotency replay ──────────────────────────────────────────
	if s.idempotent != nil {
		_ = s.idempotent.Store(ctx, adminID.String(), idempotencyKey, 201, resp)
	}

	// ── Audit log ─────────────────────────────────────────────────────────────
	txnID := result.Transaction.ID
	s.audit.Log(ctx, audit.LogParams{
		ActorID:      &adminID,
		ActorRole:    "admin",
		Action:       audit.ActionAdminIssueCBDC,
		ResourceType: "wallet",
		ResourceID:   &walletID,
		IPAddress:    ip,
		Metadata: map[string]any{
			"amount_cents":   req.AmountCents,
			"reason":         req.Reason,
			"transaction_id": txnID.String(),
		},
		Success: true,
	})

	// WebSocket live event — notify the wallet owner that CBDC was issued.
	// Fire-and-forget; a failed notification never rolls back a confirmed issuance.
	if s.publisher != nil {
		_ = s.publisher.Publish(ctx, walletID, ws.Event{
			Type:      ws.TypeIssuanceReceived,
			WalletID:  walletID.String(),
			Timestamp: time.Now(),
			Payload: ws.IssuanceEventPayload{
				TransactionID:     txnID.String(),
				AmountCents:       req.AmountCents,
				AmountDisplay:     currency.Format(req.AmountCents, "DD$"),
				Reason:            req.Reason,
				NewBalanceCents:   result.NewBalance,
				NewBalanceDisplay: currency.Format(result.NewBalance, "DD$"),
			},
		})
	}

	return resp, nil
}

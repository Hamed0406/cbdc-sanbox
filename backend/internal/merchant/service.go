package merchant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/cbdc-simulator/backend/internal/ledger"
	ws "github.com/cbdc-simulator/backend/internal/websocket"
	"github.com/cbdc-simulator/backend/pkg/crypto"
	"github.com/cbdc-simulator/backend/pkg/currency"
	"github.com/cbdc-simulator/backend/pkg/idempotency"
	"github.com/cbdc-simulator/backend/pkg/qrcode"
)

// repository is the narrow interface the service needs from the repository.
type repository interface {
	FindByUserID(ctx context.Context, userID uuid.UUID) (*Merchant, error)
	FindByID(ctx context.Context, merchantID uuid.UUID) (*Merchant, error)
	Create(ctx context.Context, p CreateMerchantParams) (*Merchant, error)
	CreatePaymentRequest(ctx context.Context, p CreatePaymentRequestParams) (*PaymentRequest, error)
	FindPaymentRequestByID(ctx context.Context, id uuid.UUID) (*PaymentRequest, error)
	MarkPaid(ctx context.Context, id, paidByWalletID, transactionID uuid.UUID) error
	CancelPaymentRequest(ctx context.Context, id, merchantID uuid.UUID) error
	ListByMerchant(ctx context.Context, merchantID uuid.UUID, p ListParams) ([]PaymentRequest, int, error)
}

// ledgerTransferer executes the actual value movement.
type ledgerTransferer interface {
	Transfer(ctx context.Context, p ledger.TransferParams) (*ledger.TransferResult, error)
}

// WalletIDLookup finds a wallet UUID by owner user ID.
// In main.go this is satisfied by a closure over wallet.Repository.
type WalletIDLookup func(ctx context.Context, userID uuid.UUID) (uuid.UUID, error)

// Service handles merchant registration and QR payment flows.
type Service struct {
	repo       repository
	wallets    WalletIDLookup
	ledger     ledgerTransferer
	idempotent *idempotency.Store
	publisher  ws.Publisher
	signingKey string
	httpClient *http.Client
}

// NewService creates a new merchant Service.
// pub may be nil in unit tests.
func NewService(repo repository, wallets WalletIDLookup, l ledgerTransferer, idempotent *idempotency.Store, pub ws.Publisher, signingKey string) *Service {
	return &Service{
		repo:       repo,
		wallets:    wallets,
		ledger:     l,
		idempotent: idempotent,
		publisher:  pub,
		signingKey: signingKey,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Register creates a merchant profile for the given user.
// The user must have role='merchant' — enforced by RequireMerchant middleware upstream.
func (s *Service) Register(ctx context.Context, userID uuid.UUID, req *RegisterRequest) (*MerchantProfile, string, error) {
	// Check if profile already exists
	if _, err := s.repo.FindByUserID(ctx, userID); err == nil {
		return nil, "", ErrMerchantAlreadyExists
	}

	// Generate API key: 32 random bytes → 64 char hex string
	rawKey, err := crypto.GenerateSecureToken()
	if err != nil {
		return nil, "", fmt.Errorf("generate api key: %w", err)
	}
	keyHash := crypto.HashToken(rawKey)
	keyPrefix := rawKey[:8]

	var bType *string
	if req.BusinessType != "" {
		bType = &req.BusinessType
	}
	var webhook *string
	if req.WebhookURL != "" {
		webhook = &req.WebhookURL
	}

	m, err := s.repo.Create(ctx, CreateMerchantParams{
		UserID:       userID,
		BusinessName: req.BusinessName,
		BusinessType: bType,
		WebhookURL:   webhook,
		APIKeyHash:   keyHash,
		APIKeyPrefix: keyPrefix,
	})
	if err != nil {
		return nil, "", err
	}

	return toProfile(m), rawKey, nil
}

// GetProfile returns the merchant profile for the authenticated user.
func (s *Service) GetProfile(ctx context.Context, userID uuid.UUID) (*MerchantProfile, error) {
	m, err := s.repo.FindByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return toProfile(m), nil
}

// CreatePaymentRequest generates a new QR payment request for the merchant.
func (s *Service) CreatePaymentRequest(ctx context.Context, userID uuid.UUID, input *CreatePaymentRequestInput) (*PaymentRequestDetail, error) {
	m, err := s.repo.FindByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !m.IsActive {
		return nil, ErrMerchantInactive
	}

	expiresAt := time.Now().UTC().Add(time.Duration(input.ExpirySecs) * time.Second)

	var ref *string
	if input.Reference != "" {
		r := input.Reference
		ref = &r
	}
	var desc *string
	if input.Description != "" {
		d := input.Description
		desc = &d
	}

	refStr := ""
	if ref != nil {
		refStr = *ref
	}
	descStr := ""
	if desc != nil {
		descStr = *desc
	}

	// Build the cbdc:// URI that will be encoded into the QR image.
	// Amount and merchant ID are HMAC-signed — tampering invalidates the sig.
	qrPayload := qrcode.BuildURI(m.ID, input.AmountCents, refStr, descStr, expiresAt, s.signingKey)

	pr, err := s.repo.CreatePaymentRequest(ctx, CreatePaymentRequestParams{
		MerchantID:  m.ID,
		AmountCents: input.AmountCents,
		Currency:    "DD$",
		Reference:   ref,
		Description: desc,
		QRPayload:   qrPayload,
		ExpiresAt:   expiresAt,
	})
	if err != nil {
		return nil, err
	}

	return toDetail(pr, m.BusinessName), nil
}

// GetPaymentRequest fetches a single payment request for the owning merchant.
func (s *Service) GetPaymentRequest(ctx context.Context, userID uuid.UUID, requestID uuid.UUID) (*PaymentRequestDetail, error) {
	m, err := s.repo.FindByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	pr, err := s.repo.FindPaymentRequestByID(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if pr.MerchantID != m.ID {
		return nil, ErrPaymentRequestNotFound
	}

	return toDetail(pr, m.BusinessName), nil
}

// ListPaymentRequests returns paginated payment requests for the merchant.
func (s *Service) ListPaymentRequests(ctx context.Context, userID uuid.UUID, p ListParams) (*PaymentRequestListResponse, error) {
	m, err := s.repo.FindByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	results, total, err := s.repo.ListByMerchant(ctx, m.ID, p)
	if err != nil {
		return nil, err
	}

	details := make([]PaymentRequestDetail, len(results))
	for i, pr := range results {
		details[i] = *toDetail(&pr, m.BusinessName)
	}

	pages := int(math.Ceil(float64(total) / float64(p.Limit)))
	if pages < 1 {
		pages = 1
	}

	return &PaymentRequestListResponse{
		PaymentRequests: details,
		Pagination: Pagination{
			Page:  p.Page,
			Limit: p.Limit,
			Total: total,
			Pages: pages,
		},
	}, nil
}

// CancelPaymentRequest cancels a PENDING payment request owned by the merchant.
func (s *Service) CancelPaymentRequest(ctx context.Context, userID uuid.UUID, requestID uuid.UUID) error {
	m, err := s.repo.FindByUserID(ctx, userID)
	if err != nil {
		return err
	}
	return s.repo.CancelPaymentRequest(ctx, requestID, m.ID)
}

// PayViaQR executes a customer payment against a QR code URI.
// Called from POST /api/v1/payments/qr (customer endpoint, not merchant).
func (s *Service) PayViaQR(ctx context.Context, payerUserID uuid.UUID, payerWalletID uuid.UUID, idempotencyKey string, req *PayQRRequest) (*PayQRResponse, error) {
	// Parse and verify the cbdc:// URI
	params, err := qrcode.ParseURI(req.QRPayload)
	if err != nil {
		return nil, fmt.Errorf("invalid QR payload: %w", err)
	}
	if err := qrcode.Verify(params, s.signingKey, time.Now()); err != nil {
		return nil, fmt.Errorf("QR verification failed: %w", err)
	}

	// Look up merchant
	merchant, err := s.repo.FindByID(ctx, params.MerchantID)
	if err != nil {
		return nil, err
	}
	if !merchant.IsActive {
		return nil, ErrMerchantInactive
	}

	// Prevent merchant paying their own QR
	if merchant.UserID == payerUserID {
		return nil, ErrSelfPayment
	}

	// Find the merchant's wallet
	merchantWalletID, err := s.wallets(ctx, merchant.UserID)
	if err != nil {
		return nil, fmt.Errorf("find merchant wallet: %w", err)
	}

	// Find the specific payment request by matching the QR payload
	// (we need its ID to mark it paid after the transfer)
	// We do this by looking up PENDING requests for this merchant with matching amount
	pr, err := s.findPaymentRequestByPayload(ctx, req.QRPayload)
	if err != nil {
		return nil, err
	}

	// Guard: check status before attempting transfer (MarkPaid will do the atomic check)
	switch pr.Status {
	case StatusPaid:
		return nil, ErrPaymentRequestAlreadyPaid
	case StatusExpired:
		return nil, ErrPaymentRequestExpired
	case StatusCancelled:
		return nil, ErrPaymentRequestCancelled
	}

	// Execute the ledger transfer
	result, err := s.ledger.Transfer(ctx, ledger.TransferParams{
		SenderWalletID:   payerWalletID,
		ReceiverWalletID: merchantWalletID,
		AmountCents:      params.AmountCents,
		TxnType:          ledger.TypePayment,
		IdempotencyKey:   idempotencyKey,
		Reference:        params.Reference,
	})
	if err != nil {
		return nil, err
	}

	// Mark the payment request as paid (conditional UPDATE — prevents double-pay)
	if markErr := s.repo.MarkPaid(ctx, pr.ID, payerWalletID, result.Transaction.ID); markErr != nil {
		// Transfer succeeded but marking failed — very rare, log it.
		// The payment did go through; we don't reverse it here.
		slog.Error("payment request mark-paid failed after successful transfer",
			"payment_request_id", pr.ID,
			"transaction_id", result.Transaction.ID,
			"error", markErr,
		)
	}

	// Fire webhook to merchant (best-effort, non-blocking)
	if merchant.WebhookURL != nil {
		go s.fireWebhook(context.Background(), *merchant.WebhookURL, pr.ID, result.Transaction.ID, params.AmountCents)
	}

	// Publish WebSocket event to merchant's wallet
	if s.publisher != nil {
		newBalDisplay := currency.Format(result.ReceiverBalance, "DD$")
		_ = s.publisher.Publish(ctx, merchantWalletID, ws.Event{
			Type: ws.TypePaymentReceived,
			Payload: ws.PaymentEventPayload{
				TransactionID:     result.Transaction.ID.String(),
				Direction:         "CREDIT",
				AmountCents:       params.AmountCents,
				AmountDisplay:     currency.Format(params.AmountCents, "DD$"),
				NewBalanceCents:   result.ReceiverBalance,
				NewBalanceDisplay: newBalDisplay,
			},
		})
	}

	refStr := params.Reference
	return &PayQRResponse{
		TransactionID:     result.Transaction.ID.String(),
		AmountCents:       params.AmountCents,
		AmountDisplay:     currency.Format(params.AmountCents, "DD$"),
		MerchantName:      merchant.BusinessName,
		Reference:         refStr,
		NewBalanceCents:   result.SenderBalance,
		NewBalanceDisplay: currency.Format(result.SenderBalance, "DD$"),
	}, nil
}

// findPaymentRequestByPayload looks up a payment request by its qr_payload value.
// This is how we match a scanned QR code to a specific payment_request row.
func (s *Service) findPaymentRequestByPayload(ctx context.Context, payload string) (*PaymentRequest, error) {
	params, _ := qrcode.ParseURI(payload)
	// List recent PENDING requests for this merchant and match by payload
	list, _, err := s.repo.ListByMerchant(ctx, params.MerchantID, ListParams{
		Page:   1,
		Limit:  100,
		Status: string(StatusPending),
	})
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].QRPayload == payload {
			return &list[i], nil
		}
	}
	// No PENDING match — check if it was already paid/expired by fetching all recent
	all, _, err := s.repo.ListByMerchant(ctx, params.MerchantID, ListParams{Page: 1, Limit: 100})
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].QRPayload == payload {
			return &all[i], nil
		}
	}
	return nil, ErrPaymentRequestNotFound
}

// fireWebhook posts a JSON event to the merchant's webhook URL.
// Called in a goroutine — errors are logged but not returned.
func (s *Service) fireWebhook(ctx context.Context, webhookURL string, requestID, transactionID uuid.UUID, amountCents int64) {
	payload := map[string]any{
		"event":          "payment.completed",
		"payment_request_id": requestID.String(),
		"transaction_id": transactionID.String(),
		"amount_cents":   amountCents,
		"timestamp":      time.Now().UTC().Format(time.RFC3339),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("webhook marshal failed", "error", err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		slog.Error("webhook request create failed", "url", webhookURL, "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CBDC-Event", "payment.completed")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		slog.Warn("webhook delivery failed", "url", webhookURL, "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		slog.Warn("webhook returned error status", "url", webhookURL, "status", resp.StatusCode)
	}
}

// toProfile converts a Merchant to its JSON representation.
func toProfile(m *Merchant) *MerchantProfile {
	return &MerchantProfile{
		ID:           m.ID.String(),
		BusinessName: m.BusinessName,
		BusinessType: m.BusinessType,
		WebhookURL:   m.WebhookURL,
		APIKeyPrefix: m.APIKeyPrefix,
		IsActive:     m.IsActive,
		CreatedAt:    m.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// toDetail converts a PaymentRequest to its JSON representation.
func toDetail(pr *PaymentRequest, businessName string) *PaymentRequestDetail {
	d := &PaymentRequestDetail{
		ID:            pr.ID.String(),
		MerchantID:    pr.MerchantID.String(),
		BusinessName:  businessName,
		AmountCents:   pr.AmountCents,
		AmountDisplay: currency.Format(pr.AmountCents, "DD$"),
		Currency:      pr.Currency,
		Reference:     pr.Reference,
		Description:   pr.Description,
		QRPayload:     pr.QRPayload,
		Status:        pr.Status,
		ExpiresAt:     pr.ExpiresAt.UTC().Format(time.RFC3339),
		CreatedAt:     pr.CreatedAt.UTC().Format(time.RFC3339),
	}
	if pr.PaidAt != nil {
		s := pr.PaidAt.UTC().Format(time.RFC3339)
		d.PaidAt = &s
	}
	return d
}

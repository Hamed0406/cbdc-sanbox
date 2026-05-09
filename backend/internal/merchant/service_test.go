package merchant_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cbdc-simulator/backend/internal/ledger"
	"github.com/cbdc-simulator/backend/internal/merchant"
)

// ── Mock repository ───────────────────────────────────────────────────────────

type mockRepo struct {
	merchant     *merchant.Merchant
	paymentReq   *merchant.PaymentRequest
	list         []merchant.PaymentRequest
	total        int
	err          error
	createMerchantErr error
	markPaidErr  error
}

func (m *mockRepo) FindByUserID(_ context.Context, _ uuid.UUID) (*merchant.Merchant, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.merchant, nil
}

func (m *mockRepo) FindByID(_ context.Context, _ uuid.UUID) (*merchant.Merchant, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.merchant, nil
}

func (m *mockRepo) Create(_ context.Context, _ merchant.CreateMerchantParams) (*merchant.Merchant, error) {
	if m.createMerchantErr != nil {
		return nil, m.createMerchantErr
	}
	return m.merchant, nil
}

func (m *mockRepo) CreatePaymentRequest(_ context.Context, p merchant.CreatePaymentRequestParams) (*merchant.PaymentRequest, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &merchant.PaymentRequest{
		ID:          uuid.New(),
		MerchantID:  p.MerchantID,
		AmountCents: p.AmountCents,
		Currency:    p.Currency,
		Reference:   p.Reference,
		Description: p.Description,
		QRPayload:   p.QRPayload,
		Status:      merchant.StatusPending,
		ExpiresAt:   p.ExpiresAt,
		CreatedAt:   time.Now(),
	}, nil
}

func (m *mockRepo) FindPaymentRequestByID(_ context.Context, _ uuid.UUID) (*merchant.PaymentRequest, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.paymentReq, nil
}

func (m *mockRepo) MarkPaid(_ context.Context, _, _, _ uuid.UUID) error {
	return m.markPaidErr
}

func (m *mockRepo) CancelPaymentRequest(_ context.Context, _, _ uuid.UUID) error {
	return m.err
}

func (m *mockRepo) ListByMerchant(_ context.Context, _ uuid.UUID, _ merchant.ListParams) ([]merchant.PaymentRequest, int, error) {
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

func newMerchant() *merchant.Merchant {
	return &merchant.Merchant{
		ID:           uuid.New(),
		UserID:       uuid.New(),
		BusinessName: "Test Shop",
		IsActive:     true,
		APIKeyPrefix: "abcd1234",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
}

func newTransferResult() *ledger.TransferResult {
	senderID := uuid.New()
	receiverID := uuid.New()
	return &ledger.TransferResult{
		Transaction: &ledger.Transaction{
			ID:               uuid.New(),
			Type:             ledger.TypePayment,
			Status:           ledger.StatusSettled,
			SenderWalletID:   &senderID,
			ReceiverWalletID: &receiverID,
			AmountCents:      1000,
		},
		SenderBalance:   49000,
		ReceiverBalance: 51000,
	}
}

// walletIDFunc is a simple WalletIDLookup implementation.
func walletIDFunc(id uuid.UUID, err error) merchant.WalletIDLookup {
	return func(_ context.Context, _ uuid.UUID) (uuid.UUID, error) {
		return id, err
	}
}

func newSvc(repo *mockRepo, l *mockLedger, walletID uuid.UUID) *merchant.Service {
	return merchant.NewService(repo, walletIDFunc(walletID, nil), l, nil, nil, "test-signing-key")
}

// ── Tests: Register ───────────────────────────────────────────────────────────

func TestRegister_Success(t *testing.T) {
	m := newMerchant()
	repo := &mockRepo{
		err:      merchant.ErrMerchantNotFound, // FindByUserID returns not found (no existing profile)
		merchant: m,
	}
	repo.err = nil // reset after first call — handled by findFirst below

	// First call (check existing) should return ErrMerchantNotFound,
	// second call (Create) should succeed. Use separate repos to simulate.
	checkRepo := &mockRepo{err: merchant.ErrMerchantNotFound, merchant: m}
	checkRepo.createMerchantErr = nil

	svc := merchant.NewService(checkRepo, walletIDFunc(uuid.New(), nil), nil, nil, nil, "test-signing-key")
	profile, rawKey, err := svc.Register(context.Background(), m.UserID, &merchant.RegisterRequest{
		BusinessName: "Test Shop",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if profile == nil {
		t.Fatal("expected profile, got nil")
	}
	if rawKey == "" {
		t.Error("expected raw API key to be returned")
	}
	if len(rawKey) < 8 {
		t.Errorf("API key too short: %d chars", len(rawKey))
	}
}

func TestRegister_AlreadyExists(t *testing.T) {
	m := newMerchant()
	repo := &mockRepo{merchant: m}
	svc := merchant.NewService(repo, walletIDFunc(uuid.New(), nil), nil, nil, nil, "test-key")

	_, _, err := svc.Register(context.Background(), m.UserID, &merchant.RegisterRequest{
		BusinessName: "Test Shop",
	})
	if !errors.Is(err, merchant.ErrMerchantAlreadyExists) {
		t.Errorf("expected ErrMerchantAlreadyExists, got %v", err)
	}
}

// ── Tests: CreatePaymentRequest ───────────────────────────────────────────────

func TestCreatePaymentRequest_Success(t *testing.T) {
	m := newMerchant()
	repo := &mockRepo{merchant: m}
	svc := newSvc(repo, nil, uuid.New())

	detail, err := svc.CreatePaymentRequest(context.Background(), m.UserID, &merchant.CreatePaymentRequestInput{
		AmountCents: 5000,
		Reference:   "ORDER-1",
		Description: "Test purchase",
	})
	if err != nil {
		t.Fatalf("CreatePaymentRequest: %v", err)
	}
	if detail.AmountCents != 5000 {
		t.Errorf("amount: want 5000, got %d", detail.AmountCents)
	}
	if detail.QRPayload == "" {
		t.Error("expected QR payload to be set")
	}
	if detail.Status != merchant.StatusPending {
		t.Errorf("status: want PENDING, got %s", detail.Status)
	}
}

func TestCreatePaymentRequest_MerchantNotFound(t *testing.T) {
	repo := &mockRepo{err: merchant.ErrMerchantNotFound}
	svc := newSvc(repo, nil, uuid.New())

	_, err := svc.CreatePaymentRequest(context.Background(), uuid.New(), &merchant.CreatePaymentRequestInput{
		AmountCents: 1000,
	})
	if !errors.Is(err, merchant.ErrMerchantNotFound) {
		t.Errorf("expected ErrMerchantNotFound, got %v", err)
	}
}

func TestCreatePaymentRequest_InactiveMerchant(t *testing.T) {
	m := newMerchant()
	m.IsActive = false
	repo := &mockRepo{merchant: m}
	svc := newSvc(repo, nil, uuid.New())

	_, err := svc.CreatePaymentRequest(context.Background(), m.UserID, &merchant.CreatePaymentRequestInput{
		AmountCents: 1000,
	})
	if !errors.Is(err, merchant.ErrMerchantInactive) {
		t.Errorf("expected ErrMerchantInactive, got %v", err)
	}
}

// ── Tests: PayViaQR ───────────────────────────────────────────────────────────

func TestPayViaQR_Success(t *testing.T) {
	m := newMerchant()
	merchantWalletID := uuid.New()
	payerWalletID := uuid.New()

	// Build a valid QR payload
	expires := time.Now().Add(15 * time.Minute)
	// We need to import qrcode, but since this is a black-box test we build
	// via CreatePaymentRequest and then use the returned payload.
	repo := &mockRepo{merchant: m}
	svc := merchant.NewService(repo, walletIDFunc(merchantWalletID, nil), &mockLedger{result: newTransferResult()}, nil, nil, "test-signing-key")

	detail, err := svc.CreatePaymentRequest(context.Background(), m.UserID, &merchant.CreatePaymentRequestInput{
		AmountCents: 1000,
		ExpirySecs:  int(time.Until(expires).Seconds()),
	})
	if err != nil {
		t.Fatalf("setup CreatePaymentRequest: %v", err)
	}

	// Now pay via QR — re-use same repo so ListByMerchant returns the created request
	payReq := &merchant.PaymentRequest{
		ID:          uuid.MustParse(detail.ID),
		MerchantID:  m.ID,
		AmountCents: 1000,
		Currency:    "DD$",
		QRPayload:   detail.QRPayload,
		Status:      merchant.StatusPending,
		ExpiresAt:   expires,
		CreatedAt:   time.Now(),
	}
	repo2 := &mockRepo{
		merchant: m,
		list:     []merchant.PaymentRequest{*payReq},
		total:    1,
	}
	svc2 := merchant.NewService(repo2, walletIDFunc(merchantWalletID, nil), &mockLedger{result: newTransferResult()}, nil, nil, "test-signing-key")

	// Use a different userID as the payer (not the merchant's userID)
	payerUserID := uuid.New()
	result, err := svc2.PayViaQR(context.Background(), payerUserID, payerWalletID, "idem-key-1", &merchant.PayQRRequest{
		QRPayload: detail.QRPayload,
	})
	if err != nil {
		t.Fatalf("PayViaQR: %v", err)
	}
	if result.AmountCents != 1000 {
		t.Errorf("amount: want 1000, got %d", result.AmountCents)
	}
	if result.MerchantName != m.BusinessName {
		t.Errorf("merchant name: want %s, got %s", m.BusinessName, result.MerchantName)
	}
}

func TestPayViaQR_SelfPayment(t *testing.T) {
	m := newMerchant()
	merchantWalletID := uuid.New()

	// Build a valid QR payload with explicit long expiry
	repo := &mockRepo{merchant: m}
	svc := merchant.NewService(repo, walletIDFunc(merchantWalletID, nil), nil, nil, nil, "test-signing-key")

	detail, err := svc.CreatePaymentRequest(context.Background(), m.UserID, &merchant.CreatePaymentRequestInput{
		AmountCents: 500,
		ExpirySecs:  3600, // 1 hour
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Payer is the merchant themselves
	payReq := &merchant.PaymentRequest{
		ID:          uuid.MustParse(detail.ID),
		MerchantID:  m.ID,
		AmountCents: 500,
		QRPayload:   detail.QRPayload,
		Status:      merchant.StatusPending,
		ExpiresAt:   time.Now().Add(time.Hour),
		CreatedAt:   time.Now(),
	}
	repo2 := &mockRepo{merchant: m, list: []merchant.PaymentRequest{*payReq}, total: 1}
	svc2 := merchant.NewService(repo2, walletIDFunc(merchantWalletID, nil), nil, nil, nil, "test-signing-key")

	_, err = svc2.PayViaQR(context.Background(), m.UserID, merchantWalletID, "idem-2", &merchant.PayQRRequest{
		QRPayload: detail.QRPayload,
	})
	if !errors.Is(err, merchant.ErrSelfPayment) {
		t.Errorf("expected ErrSelfPayment, got %v", err)
	}
}

func TestPayViaQR_AlreadyPaid(t *testing.T) {
	m := newMerchant()
	merchantWalletID := uuid.New()

	repo := &mockRepo{merchant: m}
	svc := merchant.NewService(repo, walletIDFunc(merchantWalletID, nil), nil, nil, nil, "test-signing-key")

	detail, err := svc.CreatePaymentRequest(context.Background(), m.UserID, &merchant.CreatePaymentRequestInput{
		AmountCents: 500,
		ExpirySecs:  3600, // 1 hour
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	payReq := &merchant.PaymentRequest{
		ID:          uuid.MustParse(detail.ID),
		MerchantID:  m.ID,
		AmountCents: 500,
		QRPayload:   detail.QRPayload,
		Status:      merchant.StatusPaid, // already paid
		ExpiresAt:   time.Now().Add(time.Hour),
		CreatedAt:   time.Now(),
	}
	repo2 := &mockRepo{merchant: m, list: []merchant.PaymentRequest{*payReq}, total: 1}
	svc2 := merchant.NewService(repo2, walletIDFunc(merchantWalletID, nil), nil, nil, nil, "test-signing-key")

	_, err = svc2.PayViaQR(context.Background(), uuid.New(), uuid.New(), "idem-3", &merchant.PayQRRequest{
		QRPayload: detail.QRPayload,
	})
	if !errors.Is(err, merchant.ErrPaymentRequestAlreadyPaid) {
		t.Errorf("expected ErrPaymentRequestAlreadyPaid, got %v", err)
	}
}

// ── Tests: Validation ─────────────────────────────────────────────────────────

func TestCreatePaymentRequestInput_Validate(t *testing.T) {
	tests := []struct {
		name    string
		input   merchant.CreatePaymentRequestInput
		wantErr bool
	}{
		{"valid", merchant.CreatePaymentRequestInput{AmountCents: 100}, false},
		{"zero amount", merchant.CreatePaymentRequestInput{AmountCents: 0}, true},
		{"negative amount", merchant.CreatePaymentRequestInput{AmountCents: -1}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRegisterRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     merchant.RegisterRequest
		wantErr bool
	}{
		{"valid", merchant.RegisterRequest{BusinessName: "Shop"}, false},
		{"empty name", merchant.RegisterRequest{BusinessName: ""}, true},
		{"name too long", merchant.RegisterRequest{BusinessName: string(make([]byte, 201))}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

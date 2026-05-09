package payment_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/cbdc-simulator/backend/internal/ledger"
	"github.com/cbdc-simulator/backend/internal/middleware"
	"github.com/cbdc-simulator/backend/internal/payment"
)

// ── Mock service ──────────────────────────────────────────────────────────────

type mockPaymentService struct {
	sendResp *payment.SendResponse
	txnResp  *payment.TransactionDetail
	listResp *payment.PaymentListResponse
	err      error
}

func (m *mockPaymentService) Send(_ context.Context, _ payment.SendRequest, _, _ uuid.UUID, _, _ string) (*payment.SendResponse, error) {
	return m.sendResp, m.err
}

func (m *mockPaymentService) GetTransaction(_ context.Context, _, _ uuid.UUID) (*payment.TransactionDetail, error) {
	return m.txnResp, m.err
}

func (m *mockPaymentService) ListTransactions(_ context.Context, _ uuid.UUID, _ payment.ListParams) (*payment.PaymentListResponse, error) {
	return m.listResp, m.err
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// paymentRequest builds a request with auth context values injected.
func paymentRequest(t *testing.T, method, path string, body any, userID, walletID, urlParamID string) *http.Request {
	t.Helper()

	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode request body: %v", err)
		}
	}

	r := httptest.NewRequest(method, path, &buf)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Idempotency-Key", "test-key-abc")

	rctx := chi.NewRouteContext()
	if urlParamID != "" {
		rctx.URLParams.Add("id", urlParamID)
	}
	ctx := context.WithValue(r.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, middleware.ContextKeyUserID, userID)
	ctx = context.WithValue(ctx, middleware.ContextKeyWalletID, walletID)
	ctx = context.WithValue(ctx, middleware.ContextKeyUserRole, "user")

	return r.WithContext(ctx)
}

func newHandler(svc *mockPaymentService) (http.Handler, *payment.Handler) {
	h := payment.NewHandler(svc)
	return h.Routes(), h
}

// ── POST /payments/send ───────────────────────────────────────────────────────

func TestSendHandler_Success(t *testing.T) {
	txnID := uuid.New().String()
	svc := &mockPaymentService{
		sendResp: &payment.SendResponse{
			Transaction: payment.TransactionDetail{
				ID:          txnID,
				Type:        "TRANSFER",
				Status:      "SETTLED",
				AmountCents: 1000,
			},
			NewBalanceCents:   9000,
			NewBalanceDisplay: "DD$ 90.00",
		},
	}

	r, _ := newHandler(svc)
	userID := uuid.New().String()
	walletID := uuid.New().String()

	body := payment.SendRequest{
		ToWalletID:  uuid.New().String(),
		AmountCents: 1000,
		Reference:   "Test payment",
	}
	req := paymentRequest(t, http.MethodPost, "/send", body, userID, walletID, "")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body)
	}

	var resp payment.SendResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.NewBalanceCents != 9000 {
		t.Errorf("expected balance 9000, got %d", resp.NewBalanceCents)
	}
}

func TestSendHandler_MissingAuth(t *testing.T) {
	svc := &mockPaymentService{}
	r, _ := newHandler(svc)

	// No auth context — plain request
	req := httptest.NewRequest(http.MethodPost, "/send",
		bytes.NewBufferString(`{"to_wallet_id":"x","amount_cents":100}`))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestSendHandler_InsufficientFunds(t *testing.T) {
	svc := &mockPaymentService{err: ledger.ErrInsufficientFunds}
	r, _ := newHandler(svc)

	body := payment.SendRequest{ToWalletID: uuid.New().String(), AmountCents: 999999}
	req := paymentRequest(t, http.MethodPost, "/send", body, uuid.New().String(), uuid.New().String(), "")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", rec.Code)
	}
}

func TestSendHandler_WalletFrozen(t *testing.T) {
	svc := &mockPaymentService{err: ledger.ErrWalletFrozen}
	r, _ := newHandler(svc)

	body := payment.SendRequest{ToWalletID: uuid.New().String(), AmountCents: 100}
	req := paymentRequest(t, http.MethodPost, "/send", body, uuid.New().String(), uuid.New().String(), "")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", rec.Code)
	}
}

func TestSendHandler_SelfPayment(t *testing.T) {
	svc := &mockPaymentService{err: payment.ErrSelfPayment}
	r, _ := newHandler(svc)

	body := payment.SendRequest{ToWalletID: uuid.New().String(), AmountCents: 100}
	req := paymentRequest(t, http.MethodPost, "/send", body, uuid.New().String(), uuid.New().String(), "")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", rec.Code)
	}
}

// ── GET /payments/{id} ───────────────────────────────────────────────────────

func TestGetByIDHandler_Success(t *testing.T) {
	txnID := uuid.New()
	svc := &mockPaymentService{
		txnResp: &payment.TransactionDetail{
			ID:          txnID.String(),
			AmountCents: 500,
			Status:      "SETTLED",
		},
	}
	r, _ := newHandler(svc)

	req := paymentRequest(t, http.MethodGet, "/"+txnID.String(), nil,
		uuid.New().String(), uuid.New().String(), txnID.String())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body)
	}

	var body payment.TransactionDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.AmountCents != 500 {
		t.Errorf("expected 500 cents, got %d", body.AmountCents)
	}
}

func TestGetByIDHandler_NotFound(t *testing.T) {
	svc := &mockPaymentService{err: payment.ErrTransactionNotFound}
	r, _ := newHandler(svc)

	txnID := uuid.New()
	req := paymentRequest(t, http.MethodGet, "/"+txnID.String(), nil,
		uuid.New().String(), uuid.New().String(), txnID.String())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestGetByIDHandler_InvalidUUID(t *testing.T) {
	svc := &mockPaymentService{}
	r, _ := newHandler(svc)

	req := paymentRequest(t, http.MethodGet, "/not-a-uuid", nil,
		uuid.New().String(), uuid.New().String(), "not-a-uuid")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for invalid UUID, got %d", rec.Code)
	}
}

// ── GET /payments/ ────────────────────────────────────────────────────────────

func TestListHandler_Success(t *testing.T) {
	svc := &mockPaymentService{
		listResp: &payment.PaymentListResponse{
			Transactions: []payment.TransactionDetail{
				{ID: uuid.New().String(), AmountCents: 1000, Status: "SETTLED"},
			},
			Pagination: payment.Pagination{Page: 1, Limit: 20, Total: 1, Pages: 1},
		},
	}
	r, _ := newHandler(svc)

	req := paymentRequest(t, http.MethodGet, "/", nil,
		uuid.New().String(), uuid.New().String(), "")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body)
	}

	var body payment.PaymentListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(body.Transactions) != 1 {
		t.Errorf("expected 1 transaction, got %d", len(body.Transactions))
	}
}

func TestListHandler_InvalidPageParam(t *testing.T) {
	svc := &mockPaymentService{}
	r, _ := newHandler(svc)

	req := paymentRequest(t, http.MethodGet, "/?page=bad", nil,
		uuid.New().String(), uuid.New().String(), "")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

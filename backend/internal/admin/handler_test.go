package admin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/cbdc-simulator/backend/internal/admin"
	"github.com/cbdc-simulator/backend/internal/ledger"
	"github.com/cbdc-simulator/backend/internal/middleware"
)

// ── Mock admin service ────────────────────────────────────────────────────────

type mockAdminService struct {
	resp *admin.IssuanceResponse
	err  error
}

func (m *mockAdminService) IssueCBDC(_ context.Context, _ admin.IssueRequest, _ uuid.UUID, _, _ string) (*admin.IssuanceResponse, error) {
	return m.resp, m.err
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func makeAdminRequest(t *testing.T, body any, adminUserID, idempotencyKey string) *http.Request {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, "/issue-cbdc", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	if idempotencyKey != "" {
		r.Header.Set("X-Idempotency-Key", idempotencyKey)
	}
	// Inject admin user ID into context (normally done by Authenticate middleware)
	ctx := context.WithValue(r.Context(), middleware.ContextKeyUserID, adminUserID)
	ctx = context.WithValue(ctx, middleware.ContextKeyUserRole, "admin")
	return r.WithContext(ctx)
}

func successIssuanceResp() *admin.IssuanceResponse {
	return &admin.IssuanceResponse{
		Issuance: admin.IssuanceDetail{
			ID:            uuid.New().String(),
			WalletID:      uuid.New().String(),
			AmountCents:   10000,
			AmountDisplay: "DD$ 100.00",
			Reason:        "Test issuance for demo wallet",
			TransactionID: uuid.New().String(),
		},
		NewBalanceCents:   10000,
		NewBalanceDisplay: "DD$ 100.00",
	}
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestIssueCBDC_Handler_Success(t *testing.T) {
	svc := &mockAdminService{resp: successIssuanceResp()}
	h := admin.NewHandler(svc)
	r := h.Routes()

	req := makeAdminRequest(t, admin.IssueRequest{
		WalletID:    uuid.New().String(),
		AmountCents: 10000,
		Reason:      "Demo wallet funding",
	}, uuid.New().String(), "idempotency-key-abc")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", rec.Code, rec.Body)
	}

	var resp admin.IssuanceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.NewBalanceCents != 10000 {
		t.Errorf("expected new_balance_cents=10000, got %d", resp.NewBalanceCents)
	}
}

func TestIssueCBDC_Handler_MissingIdempotencyKey(t *testing.T) {
	svc := &mockAdminService{err: admin.ErrMissingIdempotencyKey}
	h := admin.NewHandler(svc)
	r := h.Routes()

	req := makeAdminRequest(t, admin.IssueRequest{
		WalletID:    uuid.New().String(),
		AmountCents: 5000,
		Reason:      "Reason is long enough",
	}, uuid.New().String(), "") // no idempotency key

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestIssueCBDC_Handler_WalletNotFound(t *testing.T) {
	svc := &mockAdminService{err: ledger.ErrWalletNotFound}
	h := admin.NewHandler(svc)
	r := h.Routes()

	req := makeAdminRequest(t, admin.IssueRequest{
		WalletID:    uuid.New().String(),
		AmountCents: 5000,
		Reason:      "Funding nonexistent wallet",
	}, uuid.New().String(), "key-xyz")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestIssueCBDC_Handler_WalletFrozen(t *testing.T) {
	svc := &mockAdminService{err: ledger.ErrWalletFrozen}
	h := admin.NewHandler(svc)
	r := h.Routes()

	req := makeAdminRequest(t, admin.IssueRequest{
		WalletID:    uuid.New().String(),
		AmountCents: 5000,
		Reason:      "Attempting to fund frozen wallet",
	}, uuid.New().String(), "key-xyz")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", rec.Code)
	}
}

func TestIssueCBDC_Handler_AmountTooLarge(t *testing.T) {
	svc := &mockAdminService{err: admin.ErrAmountTooLarge}
	h := admin.NewHandler(svc)
	r := h.Routes()

	req := makeAdminRequest(t, admin.IssueRequest{
		WalletID:    uuid.New().String(),
		AmountCents: 999_999_999,
		Reason:      "Attempting over-limit issuance",
	}, uuid.New().String(), "key-xyz")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", rec.Code)
	}
}

func TestIssueCBDC_Handler_InvalidJSON(t *testing.T) {
	svc := &mockAdminService{}
	h := admin.NewHandler(svc)
	r := h.Routes()

	req := httptest.NewRequest(http.MethodPost, "/issue-cbdc", bytes.NewBufferString("not-json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Idempotency-Key", "key-abc")
	ctx := context.WithValue(req.Context(), middleware.ContextKeyUserID, uuid.New().String())
	ctx = context.WithValue(ctx, middleware.ContextKeyUserRole, "admin")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestIssueCBDC_Handler_NoAuthContext(t *testing.T) {
	svc := &mockAdminService{}
	h := admin.NewHandler(svc)
	r := h.Routes()

	// Request with NO user ID in context
	req := httptest.NewRequest(http.MethodPost, "/issue-cbdc", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

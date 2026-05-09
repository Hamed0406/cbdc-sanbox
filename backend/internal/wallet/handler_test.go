package wallet_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/cbdc-simulator/backend/internal/middleware"
	"github.com/cbdc-simulator/backend/internal/wallet"
)

// ── Mock service ──────────────────────────────────────────────────────────────

type mockWalletService struct {
	walletResp *wallet.WalletResponse
	balResp    *wallet.BalanceResponse
	txnResp    *wallet.TransactionListResponse
	err        error
}

func (m *mockWalletService) GetWallet(_ context.Context, _, _ uuid.UUID, _ string) (*wallet.WalletResponse, error) {
	return m.walletResp, m.err
}

func (m *mockWalletService) GetBalance(_ context.Context, _, _ uuid.UUID, _ string) (*wallet.BalanceResponse, error) {
	return m.balResp, m.err
}

func (m *mockWalletService) GetTransactions(_ context.Context, _, _ uuid.UUID, _ string, _ wallet.ListParams) (*wallet.TransactionListResponse, error) {
	return m.txnResp, m.err
}

func (m *mockWalletService) SearchWallets(_ context.Context, _ string) ([]wallet.WalletSearchResult, error) {
	return nil, m.err
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// routedRequest builds an HTTP request with chi URL params and auth context values.
func routedRequest(t *testing.T, method, path, walletIDParam, userID, role string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)

	// Inject chi URL params (normally set by the router during routing)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", walletIDParam)
	ctx := context.WithValue(r.Context(), chi.RouteCtxKey, rctx)

	// Inject auth context values (normally set by middleware.Authenticate)
	ctx = context.WithValue(ctx, middleware.ContextKeyUserID, userID)
	ctx = context.WithValue(ctx, middleware.ContextKeyUserRole, role)

	return r.WithContext(ctx)
}

// ── GET /wallets/{id} ─────────────────────────────────────────────────────────

func TestGetWallet_Success(t *testing.T) {
	walletID := uuid.New()
	userID := uuid.New()

	svc := &mockWalletService{
		walletResp: &wallet.WalletResponse{
			ID:             walletID.String(),
			UserID:         userID.String(),
			Currency:       "DD$",
			BalanceCents:   10000,
			BalanceDisplay: "DD$ 100.00",
		},
	}

	h := wallet.NewHandler(svc)
	r := h.Routes()

	req := routedRequest(t, http.MethodGet, "/"+walletID.String(), walletID.String(), userID.String(), "user")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body)
	}

	var body wallet.WalletResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
	if body.BalanceCents != 10000 {
		t.Errorf("expected 10000 cents, got %d", body.BalanceCents)
	}
}

func TestGetWallet_InvalidUUID(t *testing.T) {
	svc := &mockWalletService{}
	h := wallet.NewHandler(svc)
	r := h.Routes()

	req := routedRequest(t, http.MethodGet, "/not-a-uuid", "not-a-uuid", uuid.New().String(), "user")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid UUID, got %d", rec.Code)
	}
}

func TestGetWallet_NotFound(t *testing.T) {
	svc := &mockWalletService{err: wallet.ErrWalletNotFound}
	h := wallet.NewHandler(svc)
	r := h.Routes()

	req := routedRequest(t, http.MethodGet, "/"+uuid.New().String(), uuid.New().String(), uuid.New().String(), "user")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestGetWallet_AccessDenied(t *testing.T) {
	svc := &mockWalletService{err: wallet.ErrAccessDenied}
	h := wallet.NewHandler(svc)
	r := h.Routes()

	req := routedRequest(t, http.MethodGet, "/"+uuid.New().String(), uuid.New().String(), uuid.New().String(), "user")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

// ── GET /wallets/{id}/balance ─────────────────────────────────────────────────

func TestGetBalance_Success(t *testing.T) {
	walletID := uuid.New()
	userID := uuid.New()

	svc := &mockWalletService{
		balResp: &wallet.BalanceResponse{
			WalletID:       walletID.String(),
			BalanceCents:   5050,
			BalanceDisplay: "DD$ 50.50",
			IsFrozen:       false,
		},
	}

	h := wallet.NewHandler(svc)
	r := h.Routes()

	req := routedRequest(t, http.MethodGet, "/"+walletID.String()+"/balance", walletID.String(), userID.String(), "user")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body)
	}

	var body wallet.BalanceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.BalanceCents != 5050 {
		t.Errorf("expected 5050 cents, got %d", body.BalanceCents)
	}
}

// ── GET /wallets/{id}/transactions ────────────────────────────────────────────

func TestGetTransactions_Success(t *testing.T) {
	walletID := uuid.New()
	userID := uuid.New()

	name := "Alice Johnson"
	ref := "Coffee"
	settled := "2026-05-08T10:01:00Z"
	svc := &mockWalletService{
		txnResp: &wallet.TransactionListResponse{
			Transactions: []wallet.TransactionRow{
				{
					ID:               uuid.New().String(),
					Type:             "TRANSFER",
					Status:           "SETTLED",
					Direction:        "CREDIT",
					CounterpartyName: &name,
					AmountCents:      2500,
					AmountDisplay:    "DD$ 25.00",
					Reference:        &ref,
					CreatedAt:        "2026-05-08T10:00:00Z",
					SettledAt:        &settled,
				},
			},
			Pagination: wallet.Pagination{Page: 1, Limit: 20, Total: 1, Pages: 1},
		},
	}

	h := wallet.NewHandler(svc)
	r := h.Routes()

	req := routedRequest(t, http.MethodGet, "/"+walletID.String()+"/transactions", walletID.String(), userID.String(), "user")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body)
	}

	var body wallet.TransactionListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(body.Transactions) != 1 {
		t.Errorf("expected 1 transaction, got %d", len(body.Transactions))
	}
	if body.Pagination.Total != 1 {
		t.Errorf("expected total=1, got %d", body.Pagination.Total)
	}
}

func TestGetTransactions_InvalidPageParam(t *testing.T) {
	svc := &mockWalletService{}
	h := wallet.NewHandler(svc)
	r := h.Routes()

	walletID := uuid.New()
	req := routedRequest(t, http.MethodGet, "/"+walletID.String()+"/transactions?page=abc", walletID.String(), uuid.New().String(), "user")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid page param, got %d", rec.Code)
	}
}

func TestGetTransactions_LimitOverMaxClamped(t *testing.T) {
	walletID := uuid.New()
	svc := &mockWalletService{
		txnResp: &wallet.TransactionListResponse{
			Transactions: []wallet.TransactionRow{},
			Pagination:   wallet.Pagination{Page: 1, Limit: 20, Total: 0, Pages: 1},
		},
	}
	h := wallet.NewHandler(svc)
	r := h.Routes()

	// limit=999 is over max=100, should return 400
	req := routedRequest(t, http.MethodGet, "/"+walletID.String()+"/transactions?limit=999", walletID.String(), uuid.New().String(), "user")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for limit > 100, got %d", rec.Code)
	}
}

func TestGetTransactions_MissingAuthReturns401(t *testing.T) {
	svc := &mockWalletService{}
	h := wallet.NewHandler(svc)
	r := h.Routes()

	walletID := uuid.New()
	// Request with NO user_id in context
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", walletID.String())
	ctx := context.WithValue(context.Background(), chi.RouteCtxKey, rctx)
	req := httptest.NewRequest(http.MethodGet, "/"+walletID.String()+"/transactions", nil).WithContext(ctx)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with no auth context, got %d", rec.Code)
	}
}

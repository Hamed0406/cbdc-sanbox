package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cbdc-simulator/backend/internal/middleware"
	"github.com/cbdc-simulator/backend/pkg/token"
)

// mockValidator simulates auth.Service.ValidateAccessToken for middleware tests.
type mockValidator struct {
	claims *token.Claims
	err    error
}

func (m *mockValidator) ValidateAccessToken(_ string) (*token.Claims, error) {
	return m.claims, m.err
}

// makeRequest builds a test HTTP request with an optional Authorization header.
func makeRequest(t *testing.T, token string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/wallets/123", nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

// nextHandler records whether it was called and extracts context values.
type nextHandler struct {
	called   bool
	userID   string
	userRole string
	walletID string
}

func (n *nextHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n.called = true
	n.userID, _ = middleware.GetUserID(r.Context())
	n.userRole, _ = middleware.GetUserRole(r.Context())
	n.walletID, _ = middleware.GetWalletID(r.Context())
	w.WriteHeader(http.StatusOK)
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestAuthenticate_ValidTokenCallsNext(t *testing.T) {
	validator := &mockValidator{
		claims: &token.Claims{UserID: "usr_123", Role: "user", WalletID: "wlt_456"},
	}
	next := &nextHandler{}
	mw := middleware.Authenticate(validator)(next)

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, makeRequest(t, "valid.token.here"))

	if !next.called {
		t.Fatal("next handler should be called for valid token")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestAuthenticate_InjectsClaimsIntoContext(t *testing.T) {
	validator := &mockValidator{
		claims: &token.Claims{UserID: "usr_alice", Role: "admin", WalletID: "wlt_alice"},
	}
	next := &nextHandler{}
	mw := middleware.Authenticate(validator)(next)

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, makeRequest(t, "valid.token"))

	if next.userID != "usr_alice" {
		t.Errorf("expected userID usr_alice, got %q", next.userID)
	}
	if next.userRole != "admin" {
		t.Errorf("expected role admin, got %q", next.userRole)
	}
	if next.walletID != "wlt_alice" {
		t.Errorf("expected walletID wlt_alice, got %q", next.walletID)
	}
}

func TestAuthenticate_MissingHeaderReturns401(t *testing.T) {
	validator := &mockValidator{claims: &token.Claims{UserID: "u", Role: "user", WalletID: "w"}}
	next := &nextHandler{}
	mw := middleware.Authenticate(validator)(next)

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, makeRequest(t, "")) // no Authorization header

	if next.called {
		t.Fatal("next handler must NOT be called when auth header is missing")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthenticate_InvalidTokenReturns401(t *testing.T) {
	validator := &mockValidator{err: errors.New("token invalid")}
	next := &nextHandler{}
	mw := middleware.Authenticate(validator)(next)

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, makeRequest(t, "bad.token"))

	if next.called {
		t.Fatal("next handler must NOT be called for invalid token")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthenticate_MalformedBearerSchemeReturns401(t *testing.T) {
	validator := &mockValidator{claims: &token.Claims{UserID: "u", Role: "user", WalletID: "w"}}
	next := &nextHandler{}
	mw := middleware.Authenticate(validator)(next)

	// "Token xxx" instead of "Bearer xxx"
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Token some-token")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, r)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for non-Bearer scheme, got %d", rec.Code)
	}
}

// ── Context helpers ───────────────────────────────────────────────────────────

func TestGetUserID_EmptyContext(t *testing.T) {
	_, ok := middleware.GetUserID(context.Background())
	if ok {
		t.Error("GetUserID should return false on empty context")
	}
}

func TestGetUserRole_EmptyContext(t *testing.T) {
	_, ok := middleware.GetUserRole(context.Background())
	if ok {
		t.Error("GetUserRole should return false on empty context")
	}
}

// ── RequireRole ───────────────────────────────────────────────────────────────

func TestRequireRole_AllowedRolePasses(t *testing.T) {
	next := &nextHandler{}
	mw := middleware.RequireRole("admin", "user")(next)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	// Inject role into context (normally done by Authenticate middleware)
	ctx := context.WithValue(r.Context(), middleware.ContextKeyUserRole, "admin")
	r = r.WithContext(ctx)

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, r)

	if !next.called {
		t.Fatal("next handler should be called for allowed role")
	}
}

func TestRequireRole_DisallowedRoleReturns403(t *testing.T) {
	next := &nextHandler{}
	mw := middleware.RequireRole("admin")(next)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(r.Context(), middleware.ContextKeyUserRole, "user") // not admin
	r = r.WithContext(ctx)

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, r)

	if next.called {
		t.Fatal("next handler must NOT be called for disallowed role")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestRequireRole_NoRoleInContextReturns401(t *testing.T) {
	next := &nextHandler{}
	mw := middleware.RequireRole("admin")(next)

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 when no role in context, got %d", rec.Code)
	}
}

func TestRequireAdmin_OnlyAdminPasses(t *testing.T) {
	for _, role := range []string{"user", "merchant"} {
		next := &nextHandler{}
		mw := middleware.RequireAdmin()(next)

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		ctx := context.WithValue(r.Context(), middleware.ContextKeyUserRole, role)
		r = r.WithContext(ctx)

		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, r)

		if rec.Code != http.StatusForbidden {
			t.Errorf("role %q should get 403 on admin-only route, got %d", role, rec.Code)
		}
	}
}

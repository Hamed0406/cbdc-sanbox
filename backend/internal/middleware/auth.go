package middleware

import (
	"context"
	"net/http"
	"strings"

	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/cbdc-simulator/backend/pkg/response"
	"github.com/cbdc-simulator/backend/pkg/token"
)

// tokenValidator is the interface the JWT middleware needs.
// auth.Service satisfies this interface without middleware needing to import auth —
// this breaks the auth ↔ middleware import cycle.
type tokenValidator interface {
	ValidateAccessToken(tokenString string) (*token.Claims, error)
}

// Authenticate validates the JWT Bearer token in the Authorization header.
// On success, injects userID, role, and walletID into the request context.
//
// WHY Bearer token in header (not cookie)?
// Access tokens are short-lived (15 min) and stateless.
// Using Authorization header means it works from mobile, CLI, and non-browser clients.
// Cookies are auto-sent by browsers and require CSRF protection; headers are not.
// The refresh token (long-lived, revocable) uses an HttpOnly cookie separately.
func Authenticate(svc tokenValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := chimiddleware.GetReqID(r.Context())

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				response.Unauthorized(w, requestID)
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				response.Unauthorized(w, requestID)
				return
			}

			tokenString := parts[1]
			if tokenString == "" {
				response.Unauthorized(w, requestID)
				return
			}

			claims, err := svc.ValidateAccessToken(tokenString)
			if err != nil {
				response.Unauthorized(w, requestID)
				return
			}

			// Inject claims into context — handlers read via GetUserID() etc.
			ctx := r.Context()
			ctx = context.WithValue(ctx, ContextKeyUserID, claims.UserID)
			ctx = context.WithValue(ctx, ContextKeyUserRole, claims.Role)
			ctx = context.WithValue(ctx, ContextKeyWalletID, claims.WalletID)
			ctx = context.WithValue(ctx, ContextKeyRequestID, requestID)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ── Context value helpers ─────────────────────────────────────────────────────

// GetUserID extracts the authenticated user's ID from the request context.
func GetUserID(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(ContextKeyUserID).(string)
	return id, ok && id != ""
}

// GetUserRole extracts the role from context.
func GetUserRole(ctx context.Context) (string, bool) {
	role, ok := ctx.Value(ContextKeyUserRole).(string)
	return role, ok && role != ""
}

// GetWalletID extracts the user's wallet ID from context.
func GetWalletID(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(ContextKeyWalletID).(string)
	return id, ok && id != ""
}

// GetRequestID extracts the request trace ID from context.
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(ContextKeyRequestID).(string); ok && id != "" {
		return id
	}
	return chimiddleware.GetReqID(ctx)
}

package middleware

import (
	"net/http"

	"github.com/cbdc-simulator/backend/pkg/response"
)

// RequireRole returns a middleware that blocks requests from users whose role
// is not in the allowed list. Must be used AFTER the Authenticate middleware —
// if there is no authenticated user in context, it returns 401, not 403.
//
// WHY separate from Authenticate?
// Authenticate answers "are you logged in?" (401 if not).
// RequireRole answers "do you have permission?" (403 if not).
// Keeping these separate lets routes mix and match:
//   - Some routes: authenticated, any role
//   - Other routes: authenticated, specific roles only
func RequireRole(allowedRoles ...string) func(http.Handler) http.Handler {
	// Build a set for O(1) lookup — avoids a linear scan on every request
	allowed := make(map[string]bool, len(allowedRoles))
	for _, r := range allowedRoles {
		allowed[r] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := GetRequestID(r.Context())

			role, ok := GetUserRole(r.Context())
			if !ok {
				// No role in context means Authenticate didn't run — config error
				response.Unauthorized(w, requestID)
				return
			}

			if !allowed[role] {
				response.Forbidden(w, requestID)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireAdmin is a convenience wrapper for admin-only routes.
func RequireAdmin() func(http.Handler) http.Handler {
	return RequireRole("admin")
}

// RequireMerchant allows merchant and admin roles.
// Admin can always access merchant endpoints for support purposes.
func RequireMerchant() func(http.Handler) http.Handler {
	return RequireRole("merchant", "admin")
}

// RequireUser allows regular users and admins (not merchants).
func RequireUser() func(http.Handler) http.Handler {
	return RequireRole("user", "admin")
}

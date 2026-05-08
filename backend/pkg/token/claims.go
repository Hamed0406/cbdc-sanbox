// Package token defines shared types used by both the auth and middleware packages.
// It intentionally has no internal imports to avoid circular dependencies.
// auth and middleware both depend on this package — neither depends on the other.
package token

// Claims holds the data extracted from a validated JWT access token.
// These are injected into the request context by the Authenticate middleware
// and read by handlers via middleware.GetUserID(), GetUserRole(), GetWalletID().
type Claims struct {
	UserID   string // user UUID
	Role     string // user | merchant | admin
	WalletID string // user's primary wallet UUID
}

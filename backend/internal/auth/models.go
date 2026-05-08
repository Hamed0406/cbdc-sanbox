// Package auth handles user registration, login, JWT issuance,
// and session management (refresh tokens).
package auth

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// ── Domain errors ─────────────────────────────────────────────────────────────
// Typed errors allow handlers to map each case to the correct HTTP status code
// without using string comparisons, which are fragile and easy to misspell.

var (
	ErrEmailTaken        = errors.New("email address is already registered")
	ErrInvalidCredentials = errors.New("email or password is incorrect")
	ErrAccountLocked     = errors.New("account is temporarily locked due to too many failed attempts")
	ErrAccountInactive   = errors.New("account has been deactivated")
	ErrTokenInvalid      = errors.New("token is invalid or expired")
	ErrTokenRevoked      = errors.New("session has been revoked")
	ErrWeakPassword      = errors.New("password does not meet complexity requirements")
	ErrInvalidEmail      = errors.New("email address format is invalid")
	ErrNameTooShort      = errors.New("full name must be at least 2 characters")
)

// ── Database models ───────────────────────────────────────────────────────────

// User represents a row in the users table.
type User struct {
	ID           uuid.UUID
	Email        string
	PasswordHash string
	FullName     string
	Role         string // user | merchant | admin
	IsActive     bool
	KYCStatus    string
	FailedLogins int
	LockedUntil  *time.Time // nil = not locked
	LastLoginAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// IsLocked returns true if the user's account is currently locked.
func (u *User) IsLocked() bool {
	return u.LockedUntil != nil && time.Now().Before(*u.LockedUntil)
}

// Session represents a refresh token session in the sessions table.
type Session struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	TokenHash  string // SHA-256 of the raw refresh token — never store the raw token
	UserAgent  string
	IPAddress  string
	IsRevoked  bool
	ExpiresAt  time.Time
	CreatedAt  time.Time
	LastUsedAt time.Time
}

// Wallet is a minimal wallet representation used only within the auth package
// for registration responses. The full wallet domain lives in internal/wallet.
type Wallet struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Currency  string
	Balance   int64 // in cents
	CreatedAt time.Time
}

// ── Request DTOs ──────────────────────────────────────────────────────────────

// RegisterRequest is the JSON body for POST /api/v1/auth/register.
type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	FullName string `json:"full_name"`
}

// Validate performs input validation and returns the first error found.
// Validation lives here (not in the handler) so it's reusable and testable
// independently of HTTP concerns.
func (r *RegisterRequest) Validate() error {
	r.Email = strings.TrimSpace(strings.ToLower(r.Email))
	r.FullName = strings.TrimSpace(r.FullName)

	if !isValidEmail(r.Email) {
		return ErrInvalidEmail
	}
	if len(r.FullName) < 2 || len(r.FullName) > 200 {
		return ErrNameTooShort
	}
	return validatePassword(r.Password)
}

// LoginRequest is the JSON body for POST /api/v1/auth/login.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (r *LoginRequest) Validate() error {
	r.Email = strings.TrimSpace(strings.ToLower(r.Email))
	if r.Email == "" || r.Password == "" {
		// Return same error as wrong password — prevents user enumeration
		return ErrInvalidCredentials
	}
	return nil
}

// ── Response DTOs ─────────────────────────────────────────────────────────────

// UserResponse is the public-facing user object returned in auth responses.
// Deliberately excludes: password_hash, failed_logins, locked_until.
type UserResponse struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	FullName  string `json:"full_name"`
	Role      string `json:"role"`
	KYCStatus string `json:"kyc_status"`
	CreatedAt string `json:"created_at"`
}

// WalletResponse is the wallet portion of the auth response.
type WalletResponse struct {
	ID             string `json:"id"`
	Currency       string `json:"currency"`
	BalanceCents   int64  `json:"balance_cents"`
	BalanceDisplay string `json:"balance_display"`
}

// TokenResponse contains the access token details.
// The refresh token is NOT included here — it is set as an HttpOnly cookie.
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"` // seconds until access token expires
}

// AuthResponse is the full response for register and login.
type AuthResponse struct {
	User   UserResponse   `json:"user"`
	Wallet WalletResponse `json:"wallet"`
	Tokens TokenResponse  `json:"tokens"`
}

// RefreshResponse is returned by the token refresh endpoint.
type RefreshResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// NOTE: Claims type has moved to pkg/token to avoid import cycles.
// Use token.Claims where JWT claim data is needed.

// ── Validation helpers ────────────────────────────────────────────────────────

var emailRegex = regexp.MustCompile(`(?i)^[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}$`)

func isValidEmail(email string) bool {
	return len(email) <= 254 && emailRegex.MatchString(email)
}

// validatePassword enforces minimum password complexity.
// Requirements: 10–72 chars, at least 1 uppercase, 1 lowercase, 1 digit, 1 special char.
//
// WHY max 72? bcrypt silently truncates passwords longer than 72 bytes.
// If we allow longer passwords, users believe they set "MyVeryLongPassphrase..." but
// bcrypt only hashes the first 72 chars — a shorter substring could match.
func validatePassword(p string) error {
	if len(p) < 10 || len(p) > 72 {
		return ErrWeakPassword
	}
	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for _, c := range p {
		switch {
		case unicode.IsUpper(c):
			hasUpper = true
		case unicode.IsLower(c):
			hasLower = true
		case unicode.IsDigit(c):
			hasDigit = true
		case !unicode.IsLetter(c) && !unicode.IsDigit(c):
			hasSpecial = true
		}
	}
	if !hasUpper || !hasLower || !hasDigit || !hasSpecial {
		return ErrWeakPassword
	}
	return nil
}

// formatBalance converts cents to a display string: 1050 → "DD$ 10.50"
func formatBalance(cents int64, currency string) string {
	whole := cents / 100
	frac := cents % 100
	if frac < 0 {
		frac = -frac
	}
	return strings.Join([]string{currency, " "}, "") +
		strings.Join([]string{
			itoa(whole), ".", padLeft(itoa(frac), 2, '0'),
		}, "")
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := make([]byte, 20)
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func padLeft(s string, n int, pad rune) string {
	for len(s) < n {
		s = string(pad) + s
	}
	return s
}

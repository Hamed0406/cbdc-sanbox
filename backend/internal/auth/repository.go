package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cbdc-simulator/backend/pkg/database"
)

// Repository handles all database operations for the auth package.
// All queries use parameterized statements — no string concatenation in SQL.
type Repository struct {
	db *database.Pool
}

// NewRepository creates a new auth Repository.
func NewRepository(db *database.Pool) *Repository {
	return &Repository{db: db}
}

// CreateUser inserts a new user row and returns the created user.
// On duplicate email, returns ErrEmailTaken instead of a raw DB error.
func (r *Repository) CreateUser(ctx context.Context, email, passwordHash, fullName, role string) (*User, error) {
	user := &User{}
	err := r.db.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, full_name, role)
		VALUES ($1, $2, $3, $4)
		RETURNING id, email, password_hash, full_name, role, is_active, kyc_status,
		          failed_logins, locked_until, last_login_at, created_at, updated_at
	`, email, passwordHash, fullName, role).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.FullName,
		&user.Role, &user.IsActive, &user.KYCStatus,
		&user.FailedLogins, &user.LockedUntil, &user.LastLoginAt,
		&user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		// Check for unique constraint violation (duplicate email).
		// We map this to a typed error so the handler returns 409 Conflict,
		// not a raw 500 with DB details.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("create user: %w", err)
	}
	return user, nil
}

// CreateWallet creates a DD$ wallet for a user at registration.
// Returns the created wallet. This lives in the auth repo (not wallet repo)
// so registration can happen in a single transaction.
func (r *Repository) CreateWallet(ctx context.Context, userID uuid.UUID) (*Wallet, error) {
	wallet := &Wallet{}
	err := r.db.QueryRow(ctx, `
		INSERT INTO wallets (user_id, currency, balance)
		VALUES ($1, 'DD$', 0)
		RETURNING id, user_id, currency, balance, created_at
	`, userID).Scan(
		&wallet.ID, &wallet.UserID, &wallet.Currency,
		&wallet.Balance, &wallet.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create wallet: %w", err)
	}
	return wallet, nil
}

// FindUserByEmail fetches a user by email address.
// Returns pgx.ErrNoRows (mapped to ErrInvalidCredentials in service) if not found.
func (r *Repository) FindUserByEmail(ctx context.Context, email string) (*User, error) {
	user := &User{}
	err := r.db.QueryRow(ctx, `
		SELECT id, email, password_hash, full_name, role, is_active, kyc_status,
		       failed_logins, locked_until, last_login_at, created_at, updated_at
		FROM users
		WHERE email = $1 AND deleted_at IS NULL
	`, email).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.FullName,
		&user.Role, &user.IsActive, &user.KYCStatus,
		&user.FailedLogins, &user.LockedUntil, &user.LastLoginAt,
		&user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("find user by email: %w", err)
	}
	return user, nil
}

// FindUserByID fetches a user by their UUID.
func (r *Repository) FindUserByID(ctx context.Context, id uuid.UUID) (*User, error) {
	user := &User{}
	err := r.db.QueryRow(ctx, `
		SELECT id, email, password_hash, full_name, role, is_active, kyc_status,
		       failed_logins, locked_until, last_login_at, created_at, updated_at
		FROM users
		WHERE id = $1 AND deleted_at IS NULL
	`, id).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.FullName,
		&user.Role, &user.IsActive, &user.KYCStatus,
		&user.FailedLogins, &user.LockedUntil, &user.LastLoginAt,
		&user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTokenInvalid
		}
		return nil, fmt.Errorf("find user by id: %w", err)
	}
	return user, nil
}

// FindWalletByUserID fetches the primary wallet for a user.
func (r *Repository) FindWalletByUserID(ctx context.Context, userID uuid.UUID) (*Wallet, error) {
	wallet := &Wallet{}
	err := r.db.QueryRow(ctx, `
		SELECT id, user_id, currency, balance, created_at
		FROM wallets
		WHERE user_id = $1 AND currency = 'DD$' AND deleted_at IS NULL
		LIMIT 1
	`, userID).Scan(
		&wallet.ID, &wallet.UserID, &wallet.Currency,
		&wallet.Balance, &wallet.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("wallet not found for user")
		}
		return nil, fmt.Errorf("find wallet by user id: %w", err)
	}
	return wallet, nil
}

// IncrementFailedLogins adds 1 to failed_logins for a user.
// Called after every failed login attempt to track brute-force attempts.
func (r *Repository) IncrementFailedLogins(ctx context.Context, userID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		UPDATE users SET failed_logins = failed_logins + 1, updated_at = NOW()
		WHERE id = $1
	`, userID)
	return err
}

// LockUser sets locked_until to prevent login for the given duration.
// Called when failed_logins reaches the threshold (5 attempts).
func (r *Repository) LockUser(ctx context.Context, userID uuid.UUID, until time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE users SET locked_until = $2, updated_at = NOW()
		WHERE id = $1
	`, userID, until)
	return err
}

// ResetFailedLogins clears the failed login counter after a successful login.
func (r *Repository) ResetFailedLogins(ctx context.Context, userID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		UPDATE users SET failed_logins = 0, locked_until = NULL, last_login_at = NOW(), updated_at = NOW()
		WHERE id = $1
	`, userID)
	return err
}

// CreateSession stores a hashed refresh token in the sessions table.
// We store the hash, never the raw token — protects against DB leaks.
func (r *Repository) CreateSession(ctx context.Context, userID uuid.UUID, tokenHash, userAgent, ipAddress string, expiresAt time.Time) (*Session, error) {
	session := &Session{}
	err := r.db.QueryRow(ctx, `
		INSERT INTO sessions (user_id, token_hash, user_agent, ip_address, expires_at)
		VALUES ($1, $2, $3, $4::inet, $5)
		RETURNING id, user_id, token_hash, user_agent, ip_address::text, is_revoked, expires_at, created_at, last_used_at
	`, userID, tokenHash, userAgent, ipAddress, expiresAt).Scan(
		&session.ID, &session.UserID, &session.TokenHash,
		&session.UserAgent, &session.IPAddress,
		&session.IsRevoked, &session.ExpiresAt,
		&session.CreatedAt, &session.LastUsedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return session, nil
}

// FindSessionByTokenHash looks up an active session by the hashed token.
// Used during token refresh to validate the incoming refresh token.
func (r *Repository) FindSessionByTokenHash(ctx context.Context, tokenHash string) (*Session, error) {
	session := &Session{}
	err := r.db.QueryRow(ctx, `
		SELECT id, user_id, token_hash, user_agent, ip_address::text, is_revoked, expires_at, created_at, last_used_at
		FROM sessions
		WHERE token_hash = $1 AND is_revoked = FALSE AND expires_at > NOW()
	`, tokenHash).Scan(
		&session.ID, &session.UserID, &session.TokenHash,
		&session.UserAgent, &session.IPAddress,
		&session.IsRevoked, &session.ExpiresAt,
		&session.CreatedAt, &session.LastUsedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTokenRevoked
		}
		return nil, fmt.Errorf("find session: %w", err)
	}
	return session, nil
}

// RevokeSession marks a session as revoked (logout).
// We use soft-revocation (is_revoked = true) rather than DELETE
// so we retain the audit trail of when sessions were active.
func (r *Repository) RevokeSession(ctx context.Context, tokenHash string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE sessions SET is_revoked = TRUE WHERE token_hash = $1
	`, tokenHash)
	return err
}

// DeleteSession physically removes a session row during token rotation.
// After rotation, the old token must be deleted (not just revoked) so that
// token-reuse detection works: if we later see the old hash, we know
// it was stolen and rotation should invalidate ALL sessions for that user.
func (r *Repository) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := r.db.Exec(ctx, `
		DELETE FROM sessions WHERE token_hash = $1
	`, tokenHash)
	return err
}

// RevokeAllUserSessions invalidates every active session for a user.
// Used when a compromise is detected or on forced logout.
func (r *Repository) RevokeAllUserSessions(ctx context.Context, userID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		UPDATE sessions SET is_revoked = TRUE
		WHERE user_id = $1 AND is_revoked = FALSE
	`, userID)
	return err
}

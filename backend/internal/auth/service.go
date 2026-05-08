package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/cbdc-simulator/backend/internal/audit"
	"github.com/cbdc-simulator/backend/pkg/crypto"
	"github.com/cbdc-simulator/backend/pkg/token"
)

const (
	// maxFailedLogins is the number of wrong password attempts before lockout.
	// 5 is a common industry standard — enough for typos, low enough to deter brute force.
	maxFailedLogins = 5

	// lockoutDuration is how long an account stays locked after too many failures.
	lockoutDuration = 30 * time.Minute

	// refreshTokenCookieName is the HttpOnly cookie key for the refresh token.
	refreshTokenCookieName = "refresh_token"
)

// Config holds all configuration for the auth service.
type Config struct {
	JWTSecret        string
	AccessTokenTTL   time.Duration // typically 15 minutes
	RefreshTokenTTL  time.Duration // typically 7 days
	SigningKey        string        // for transaction HMAC (passed through for consistency)
}

// repository defines the data operations the Service needs.
// Using an interface here (not *Repository directly) lets tests inject a mock
// without importing a real database, keeping unit tests fast and self-contained.
type repository interface {
	CreateUser(ctx context.Context, email, passwordHash, fullName, role string) (*User, error)
	CreateWallet(ctx context.Context, userID uuid.UUID) (*Wallet, error)
	FindUserByEmail(ctx context.Context, email string) (*User, error)
	FindUserByID(ctx context.Context, id uuid.UUID) (*User, error)
	FindWalletByUserID(ctx context.Context, userID uuid.UUID) (*Wallet, error)
	IncrementFailedLogins(ctx context.Context, userID uuid.UUID) error
	LockUser(ctx context.Context, userID uuid.UUID, until time.Time) error
	ResetFailedLogins(ctx context.Context, userID uuid.UUID) error
	CreateSession(ctx context.Context, userID uuid.UUID, tokenHash, userAgent, ipAddress string, expiresAt time.Time) (*Session, error)
	FindSessionByTokenHash(ctx context.Context, tokenHash string) (*Session, error)
	RevokeSession(ctx context.Context, tokenHash string) error
	DeleteSession(ctx context.Context, tokenHash string) error
	RevokeAllUserSessions(ctx context.Context, userID uuid.UUID) error
}

// Service implements all authentication business logic.
type Service struct {
	repo  repository
	audit *audit.Service
	cfg   Config
}

// NewService creates a new auth Service.
func NewService(repo repository, auditSvc *audit.Service, cfg Config) *Service {
	return &Service{repo: repo, audit: auditSvc, cfg: cfg}
}

// Register creates a new user account and returns auth tokens.
// Also auto-creates a DD$ wallet for the new user.
func (s *Service) Register(ctx context.Context, req RegisterRequest, ip, userAgent string) (*AuthResponse, string, error) {
	// Validate input before touching the database
	if err := req.Validate(); err != nil {
		return nil, "", err
	}

	// Hash the password with bcrypt before storing.
	// Never store plaintext passwords.
	hash, err := crypto.HashPassword(req.Password)
	if err != nil {
		return nil, "", fmt.Errorf("hash password: %w", err)
	}

	// Create the user record
	user, err := s.repo.CreateUser(ctx, req.Email, hash, req.FullName, "user")
	if err != nil {
		// ErrEmailTaken will surface as a 409 Conflict in the handler
		return nil, "", err
	}

	// Auto-create the user's DD$ wallet immediately on registration.
	// A user without a wallet cannot do anything useful in the system.
	wallet, err := s.repo.CreateWallet(ctx, user.ID)
	if err != nil {
		// This is an unexpected error — the user row was created but the wallet wasn't.
		// Log for ops visibility; the handler will return 500.
		slog.Error("wallet creation failed after user registration",
			"user_id", user.ID, "error", err)
		return nil, "", fmt.Errorf("create wallet: %w", err)
	}

	// Issue tokens
	accessToken, err := s.issueAccessToken(user, wallet.ID)
	if err != nil {
		return nil, "", fmt.Errorf("issue access token: %w", err)
	}

	refreshToken, err := s.issueRefreshToken(ctx, user.ID, ip, userAgent)
	if err != nil {
		return nil, "", fmt.Errorf("issue refresh token: %w", err)
	}

	// Log the registration event (best-effort — does not abort on failure)
	uid := user.ID
	s.audit.Log(ctx, audit.LogParams{
		ActorID:      &uid,
		ActorRole:    user.Role,
		Action:       audit.ActionUserRegister,
		ResourceType: "user",
		ResourceID:   &uid,
		IPAddress:    ip,
		UserAgent:    userAgent,
		Metadata:     map[string]any{"email": user.Email},
		Success:      true,
	})

	return s.buildAuthResponse(user, wallet, accessToken), refreshToken, nil
}

// Login authenticates a user by email+password and returns auth tokens.
// It enforces rate limiting via failed login counters stored in the DB.
func (s *Service) Login(ctx context.Context, req LoginRequest, ip, userAgent string) (*AuthResponse, string, error) {
	if err := req.Validate(); err != nil {
		return nil, "", ErrInvalidCredentials
	}

	// Fetch user by email — returns ErrInvalidCredentials if not found.
	// We use the same error for "not found" and "wrong password" to prevent
	// user enumeration (attacker cannot tell whether an email exists).
	user, err := s.repo.FindUserByEmail(ctx, req.Email)
	if err != nil {
		// Log failed attempt even without a user ID (email doesn't exist case)
		s.audit.Log(ctx, audit.LogParams{
			Action:    audit.ActionUserLoginFailed,
			IPAddress: ip,
			UserAgent: userAgent,
			Metadata:  map[string]any{"email": req.Email, "reason": "user_not_found"},
			Success:   false,
			ErrorCode: "INVALID_CREDENTIALS",
		})
		return nil, "", ErrInvalidCredentials
	}

	// Check account status before verifying password — gives attacker no info
	// about password correctness when the account is locked/inactive
	if !user.IsActive {
		return nil, "", ErrAccountInactive
	}
	if user.IsLocked() {
		uid := user.ID
		s.audit.Log(ctx, audit.LogParams{
			ActorID:   &uid,
			Action:    audit.ActionUserLoginFailed,
			IPAddress: ip,
			Metadata:  map[string]any{"reason": "account_locked", "locked_until": user.LockedUntil},
			Success:   false,
			ErrorCode: "ACCOUNT_LOCKED",
		})
		return nil, "", ErrAccountLocked
	}

	// Verify password — bcrypt comparison is constant-time
	if err := crypto.VerifyPassword(user.PasswordHash, req.Password); err != nil {
		s.handleFailedLogin(ctx, user, ip, userAgent)
		return nil, "", ErrInvalidCredentials
	}

	// Password correct — fetch wallet for JWT claims
	wallet, err := s.repo.FindWalletByUserID(ctx, user.ID)
	if err != nil {
		return nil, "", fmt.Errorf("fetch wallet: %w", err)
	}

	// Issue tokens
	accessToken, err := s.issueAccessToken(user, wallet.ID)
	if err != nil {
		return nil, "", fmt.Errorf("issue access token: %w", err)
	}

	refreshToken, err := s.issueRefreshToken(ctx, user.ID, ip, userAgent)
	if err != nil {
		return nil, "", fmt.Errorf("issue refresh token: %w", err)
	}

	// Reset failed login counter on successful login
	if err := s.repo.ResetFailedLogins(ctx, user.ID); err != nil {
		// Non-fatal — log but don't abort the successful login
		slog.Warn("failed to reset failed_logins counter", "user_id", user.ID, "error", err)
	}

	uid := user.ID
	s.audit.Log(ctx, audit.LogParams{
		ActorID:      &uid,
		ActorRole:    user.Role,
		Action:       audit.ActionUserLogin,
		ResourceType: "user",
		ResourceID:   &uid,
		IPAddress:    ip,
		UserAgent:    userAgent,
		Success:      true,
	})

	return s.buildAuthResponse(user, wallet, accessToken), refreshToken, nil
}

// Logout revokes the current refresh token session.
func (s *Service) Logout(ctx context.Context, rawRefreshToken string, userID *uuid.UUID, ip string) error {
	tokenHash := crypto.HashToken(rawRefreshToken)
	if err := s.repo.RevokeSession(ctx, tokenHash); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}

	s.audit.Log(ctx, audit.LogParams{
		ActorID:   userID,
		Action:    audit.ActionUserLogout,
		IPAddress: ip,
		Success:   true,
	})
	return nil
}

// RefreshToken validates a refresh token, rotates it, and returns a new access token.
// Token rotation: each use of a refresh token invalidates it and issues a new one.
// If an old (already-rotated) token is used, it indicates theft — all sessions revoked.
func (s *Service) RefreshToken(ctx context.Context, rawRefreshToken, ip, userAgent string) (*RefreshResponse, string, error) {
	tokenHash := crypto.HashToken(rawRefreshToken)

	// Look up the session — returns ErrTokenRevoked if not found or already revoked
	session, err := s.repo.FindSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, ErrTokenRevoked) {
			// A revoked token being used again = possible token theft.
			// Revoke ALL sessions for safety — forces re-login on all devices.
			// We don't have the user ID here without another query, so log the hash.
			slog.Warn("revoked refresh token reuse detected — possible token theft",
				"token_hash_prefix", tokenHash[:8], "ip", ip)
		}
		return nil, "", ErrTokenRevoked
	}

	// Fetch user to rebuild JWT claims
	user, err := s.repo.FindUserByID(ctx, session.UserID)
	if err != nil {
		return nil, "", fmt.Errorf("find user for refresh: %w", err)
	}

	wallet, err := s.repo.FindWalletByUserID(ctx, user.ID)
	if err != nil {
		return nil, "", fmt.Errorf("find wallet for refresh: %w", err)
	}

	// Rotate: delete old session, create new one
	if err := s.repo.DeleteSession(ctx, tokenHash); err != nil {
		return nil, "", fmt.Errorf("delete old session: %w", err)
	}

	newAccessToken, err := s.issueAccessToken(user, wallet.ID)
	if err != nil {
		return nil, "", fmt.Errorf("issue access token: %w", err)
	}

	newRefreshToken, err := s.issueRefreshToken(ctx, user.ID, ip, userAgent)
	if err != nil {
		return nil, "", fmt.Errorf("issue refresh token: %w", err)
	}

	uid := user.ID
	s.audit.Log(ctx, audit.LogParams{
		ActorID:   &uid,
		ActorRole: user.Role,
		Action:    audit.ActionUserTokenRefresh,
		IPAddress: ip,
		Success:   true,
	})

	return &RefreshResponse{
		AccessToken: newAccessToken,
		ExpiresIn:   int(s.cfg.AccessTokenTTL.Seconds()),
	}, newRefreshToken, nil
}

// ValidateAccessToken parses and validates a JWT access token.
// Returns the embedded token.Claims on success.
// Return type is from pkg/token (not auth) to avoid import cycles with middleware.
func (s *Service) ValidateAccessToken(tokenString string) (*token.Claims, error) {
	parsed, err := jwt.Parse(tokenString, func(t *jwt.Token) (any, error) {
		// Verify the signing algorithm — reject tokens using unexpected algorithms.
		// This prevents "algorithm confusion" attacks where an attacker signs
		// a token with the RS256 public key (treated as the HS256 secret).
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(s.cfg.JWTSecret), nil
	})
	if err != nil {
		return nil, ErrTokenInvalid
	}

	mapClaims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok || !parsed.Valid {
		return nil, ErrTokenInvalid
	}

	claims := &token.Claims{
		UserID:   getString(mapClaims, "uid"),
		Role:     getString(mapClaims, "role"),
		WalletID: getString(mapClaims, "wid"),
	}
	if claims.UserID == "" || claims.Role == "" {
		return nil, ErrTokenInvalid
	}
	return claims, nil
}

// ── Private helpers ───────────────────────────────────────────────────────────

// issueAccessToken creates a signed JWT with the user's ID, role, and wallet ID.
func (s *Service) issueAccessToken(user *User, walletID uuid.UUID) (string, error) {
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"uid":  user.ID.String(),
		"role": user.Role,
		"wid":  walletID.String(),
		"iat":  now.Unix(),
		"exp":  now.Add(s.cfg.AccessTokenTTL).Unix(),
		// jti (JWT ID) makes every token unique — prevents identical tokens
		// being issued back-to-back if the clock has low precision.
		"jti": uuid.New().String(),
	})
	return token.SignedString([]byte(s.cfg.JWTSecret))
}

// issueRefreshToken generates a random token, stores its hash, returns the raw token.
// The raw token goes into the HttpOnly cookie; the hash goes into the DB.
func (s *Service) issueRefreshToken(ctx context.Context, userID uuid.UUID, ip, userAgent string) (string, error) {
	rawToken, err := crypto.GenerateSecureToken()
	if err != nil {
		return "", fmt.Errorf("generate refresh token: %w", err)
	}

	tokenHash := crypto.HashToken(rawToken)
	expiresAt := time.Now().Add(s.cfg.RefreshTokenTTL)

	if _, err := s.repo.CreateSession(ctx, userID, tokenHash, userAgent, ip, expiresAt); err != nil {
		return "", fmt.Errorf("store session: %w", err)
	}
	return rawToken, nil
}

// handleFailedLogin increments the failed login counter and locks the account
// if the threshold is reached.
func (s *Service) handleFailedLogin(ctx context.Context, user *User, ip, userAgent string) {
	if err := s.repo.IncrementFailedLogins(ctx, user.ID); err != nil {
		slog.Error("failed to increment failed_logins", "user_id", user.ID, "error", err)
	}

	uid := user.ID
	s.audit.Log(ctx, audit.LogParams{
		ActorID:   &uid,
		Action:    audit.ActionUserLoginFailed,
		IPAddress: ip,
		UserAgent: userAgent,
		Metadata: map[string]any{
			"failed_logins_now": user.FailedLogins + 1,
		},
		Success:   false,
		ErrorCode: "INVALID_CREDENTIALS",
	})

	// Lock the account if we've hit the threshold
	if user.FailedLogins+1 >= maxFailedLogins {
		lockUntil := time.Now().Add(lockoutDuration)
		if err := s.repo.LockUser(ctx, user.ID, lockUntil); err != nil {
			slog.Error("failed to lock user account", "user_id", user.ID, "error", err)
		}
		s.audit.Log(ctx, audit.LogParams{
			ActorID:   &uid,
			Action:    audit.ActionUserLocked,
			IPAddress: ip,
			Metadata:  map[string]any{"locked_until": lockUntil},
			Success:   true,
		})
		slog.Warn("user account locked due to failed login attempts",
			"user_id", user.ID, "ip", ip)
	}
}

// buildAuthResponse assembles the response DTO from domain objects.
func (s *Service) buildAuthResponse(user *User, wallet *Wallet, accessToken string) *AuthResponse {
	return &AuthResponse{
		User: UserResponse{
			ID:        user.ID.String(),
			Email:     user.Email,
			FullName:  user.FullName,
			Role:      user.Role,
			KYCStatus: user.KYCStatus,
			CreatedAt: user.CreatedAt.UTC().Format(time.RFC3339),
		},
		Wallet: WalletResponse{
			ID:             wallet.ID.String(),
			Currency:       wallet.Currency,
			BalanceCents:   wallet.Balance,
			BalanceDisplay: formatBalance(wallet.Balance, wallet.Currency),
		},
		Tokens: TokenResponse{
			AccessToken: accessToken,
			ExpiresIn:   int(s.cfg.AccessTokenTTL.Seconds()),
		},
	}
}

// getString safely extracts a string from JWT MapClaims.
func getString(claims jwt.MapClaims, key string) string {
	v, ok := claims[key]
	if !ok {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", v)
	}
}

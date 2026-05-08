package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cbdc-simulator/backend/internal/audit"
	"github.com/cbdc-simulator/backend/internal/auth"
	"github.com/cbdc-simulator/backend/pkg/crypto"
	_ "github.com/cbdc-simulator/backend/pkg/token" // Claims lives here now
)

// ── Test doubles ─────────────────────────────────────────────────────────────
// Minimal in-memory implementations of the repository for unit testing.
// We test the service logic without hitting a real database.

type mockRepo struct {
	users    map[string]*auth.User     // keyed by email
	userByID map[uuid.UUID]*auth.User
	wallets  map[uuid.UUID]*auth.Wallet // keyed by userID
	sessions map[string]*auth.Session  // keyed by tokenHash

	// Capture calls for assertion
	failedLoginsIncr map[uuid.UUID]int
	lockedUsers      map[uuid.UUID]time.Time
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		users:            make(map[string]*auth.User),
		userByID:         make(map[uuid.UUID]*auth.User),
		wallets:          make(map[uuid.UUID]*auth.Wallet),
		sessions:         make(map[string]*auth.Session),
		failedLoginsIncr: make(map[uuid.UUID]int),
		lockedUsers:      make(map[uuid.UUID]time.Time),
	}
}

func (m *mockRepo) CreateUser(_ context.Context, email, hash, name, role string) (*auth.User, error) {
	if _, exists := m.users[email]; exists {
		return nil, auth.ErrEmailTaken
	}
	u := &auth.User{
		ID:           uuid.New(),
		Email:        email,
		PasswordHash: hash,
		FullName:     name,
		Role:         role,
		IsActive:     true,
		KYCStatus:    "pending",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	m.users[email] = u
	m.userByID[u.ID] = u
	return u, nil
}

func (m *mockRepo) CreateWallet(_ context.Context, userID uuid.UUID) (*auth.Wallet, error) {
	w := &auth.Wallet{ID: uuid.New(), UserID: userID, Currency: "DD$", Balance: 0, CreatedAt: time.Now()}
	m.wallets[userID] = w
	return w, nil
}

func (m *mockRepo) FindUserByEmail(_ context.Context, email string) (*auth.User, error) {
	u, ok := m.users[email]
	if !ok {
		return nil, auth.ErrInvalidCredentials
	}
	return u, nil
}

func (m *mockRepo) FindUserByID(_ context.Context, id uuid.UUID) (*auth.User, error) {
	u, ok := m.userByID[id]
	if !ok {
		return nil, auth.ErrTokenInvalid
	}
	return u, nil
}

func (m *mockRepo) FindWalletByUserID(_ context.Context, userID uuid.UUID) (*auth.Wallet, error) {
	w, ok := m.wallets[userID]
	if !ok {
		return nil, errors.New("wallet not found")
	}
	return w, nil
}

func (m *mockRepo) IncrementFailedLogins(_ context.Context, id uuid.UUID) error {
	m.failedLoginsIncr[id]++
	if u, ok := m.userByID[id]; ok {
		u.FailedLogins++
	}
	return nil
}

func (m *mockRepo) LockUser(_ context.Context, id uuid.UUID, until time.Time) error {
	m.lockedUsers[id] = until
	if u, ok := m.userByID[id]; ok {
		u.LockedUntil = &until
	}
	return nil
}

func (m *mockRepo) ResetFailedLogins(_ context.Context, id uuid.UUID) error {
	if u, ok := m.userByID[id]; ok {
		u.FailedLogins = 0
		u.LockedUntil = nil
	}
	return nil
}

func (m *mockRepo) CreateSession(_ context.Context, userID uuid.UUID, tokenHash, _, _ string, expiresAt time.Time) (*auth.Session, error) {
	s := &auth.Session{
		ID: uuid.New(), UserID: userID, TokenHash: tokenHash,
		IsRevoked: false, ExpiresAt: expiresAt, CreatedAt: time.Now(), LastUsedAt: time.Now(),
	}
	m.sessions[tokenHash] = s
	return s, nil
}

func (m *mockRepo) FindSessionByTokenHash(_ context.Context, tokenHash string) (*auth.Session, error) {
	s, ok := m.sessions[tokenHash]
	if !ok || s.IsRevoked || time.Now().After(s.ExpiresAt) {
		return nil, auth.ErrTokenRevoked
	}
	return s, nil
}

func (m *mockRepo) RevokeSession(_ context.Context, tokenHash string) error {
	if s, ok := m.sessions[tokenHash]; ok {
		s.IsRevoked = true
	}
	return nil
}

func (m *mockRepo) DeleteSession(_ context.Context, tokenHash string) error {
	delete(m.sessions, tokenHash)
	return nil
}

func (m *mockRepo) RevokeAllUserSessions(_ context.Context, userID uuid.UUID) error {
	for _, s := range m.sessions {
		if s.UserID == userID {
			s.IsRevoked = true
		}
	}
	return nil
}

// mockAudit captures audit calls without writing to DB
type mockAudit struct{ calls []audit.LogParams }

func (m *mockAudit) Log(_ context.Context, p audit.LogParams) { m.calls = append(m.calls, p) }

// newTestService creates a Service with mock dependencies for unit testing.
func newTestService() (*auth.Service, *mockRepo, *mockAudit) {
	repo := newMockRepo()
	auditMock := &mockAudit{}

	// We need a real audit.Service but we can use a nil db since the mock
	// captures calls. Actually, let's use a thin wrapper.
	// For simplicity, we'll test via the exported Service directly.
	// The audit.Service needs a real DB — for unit tests we accept that
	// audit calls are fire-and-forget and use a nil-safe wrapper.

	svc := auth.NewService(
		repo,
		audit.NewServiceWithLogger(auditMock), // uses the mock logger
		auth.Config{
			JWTSecret:       "test-secret-32-bytes-minimum-ok!",
			AccessTokenTTL:  15 * time.Minute,
			RefreshTokenTTL: 7 * 24 * time.Hour,
		},
	)
	return svc, repo, auditMock
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestService_Register_SuccessCreatesUserAndWallet(t *testing.T) {
	svc, repo, _ := newTestService()

	req := auth.RegisterRequest{
		Email:    "alice@example.com",
		Password: "SecurePass1!",
		FullName: "Alice Johnson",
	}

	resp, refreshToken, err := svc.Register(context.Background(), req, "127.0.0.1", "TestAgent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Response must include user, wallet, and tokens
	if resp.User.Email != "alice@example.com" {
		t.Errorf("expected email alice@example.com, got %q", resp.User.Email)
	}
	if resp.User.Role != "user" {
		t.Errorf("expected role user, got %q", resp.User.Role)
	}
	if resp.Wallet.Currency != "DD$" {
		t.Errorf("expected currency DD$, got %q", resp.Wallet.Currency)
	}
	if resp.Wallet.BalanceCents != 0 {
		t.Errorf("new wallet should have 0 balance, got %d", resp.Wallet.BalanceCents)
	}
	if resp.Tokens.AccessToken == "" {
		t.Error("access token must not be empty")
	}
	if resp.Tokens.ExpiresIn != 900 {
		t.Errorf("expected 900s expiry, got %d", resp.Tokens.ExpiresIn)
	}
	if refreshToken == "" {
		t.Error("refresh token must not be empty")
	}

	// Verify DB state
	if _, ok := repo.users["alice@example.com"]; !ok {
		t.Error("user was not persisted to repository")
	}
}

func TestService_Register_DuplicateEmailFails(t *testing.T) {
	svc, _, _ := newTestService()
	req := auth.RegisterRequest{Email: "alice@example.com", Password: "SecurePass1!", FullName: "Alice"}

	svc.Register(context.Background(), req, "", "") //nolint:errcheck

	// Second registration with same email
	_, _, err := svc.Register(context.Background(), req, "", "")
	if !errors.Is(err, auth.ErrEmailTaken) {
		t.Errorf("expected ErrEmailTaken, got %v", err)
	}
}

func TestService_Register_WeakPasswordFails(t *testing.T) {
	svc, _, _ := newTestService()
	req := auth.RegisterRequest{Email: "b@example.com", Password: "weak", FullName: "Bob"}

	_, _, err := svc.Register(context.Background(), req, "", "")
	if !errors.Is(err, auth.ErrWeakPassword) {
		t.Errorf("expected ErrWeakPassword, got %v", err)
	}
}

func TestService_Register_PasswordIsNotStoredInPlaintext(t *testing.T) {
	svc, repo, _ := newTestService()
	req := auth.RegisterRequest{Email: "c@example.com", Password: "SecurePass1!", FullName: "Carol"}
	svc.Register(context.Background(), req, "", "") //nolint:errcheck

	user := repo.users["c@example.com"]
	if user.PasswordHash == "SecurePass1!" {
		t.Fatal("SECURITY BUG: password stored in plaintext!")
	}
	// Verify bcrypt hash is valid
	if err := crypto.VerifyPassword(user.PasswordHash, "SecurePass1!"); err != nil {
		t.Errorf("stored hash does not verify against original password: %v", err)
	}
}

func TestService_Login_ValidCredentialsSucceeds(t *testing.T) {
	svc, _, _ := newTestService()

	// Register first
	regReq := auth.RegisterRequest{Email: "d@example.com", Password: "SecurePass1!", FullName: "Dave"}
	svc.Register(context.Background(), regReq, "", "") //nolint:errcheck

	// Then login
	loginReq := auth.LoginRequest{Email: "d@example.com", Password: "SecurePass1!"}
	resp, refreshToken, err := svc.Login(context.Background(), loginReq, "127.0.0.1", "TestAgent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.User.Email != "d@example.com" {
		t.Errorf("unexpected user email: %q", resp.User.Email)
	}
	if resp.Tokens.AccessToken == "" {
		t.Error("access token must not be empty on login")
	}
	if refreshToken == "" {
		t.Error("refresh token must not be empty on login")
	}
}

func TestService_Login_WrongPasswordFails(t *testing.T) {
	svc, _, _ := newTestService()
	svc.Register(context.Background(), auth.RegisterRequest{ //nolint:errcheck
		Email: "e@example.com", Password: "SecurePass1!", FullName: "Eve",
	}, "", "")

	_, _, err := svc.Login(context.Background(),
		auth.LoginRequest{Email: "e@example.com", Password: "WrongPass1!"},
		"127.0.0.1", "")
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestService_Login_UnknownEmailFails(t *testing.T) {
	svc, _, _ := newTestService()
	_, _, err := svc.Login(context.Background(),
		auth.LoginRequest{Email: "nobody@example.com", Password: "SecurePass1!"},
		"127.0.0.1", "")
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("expected ErrInvalidCredentials for unknown email, got %v", err)
	}
}

func TestService_Login_FailedAttemptsIncrementCounter(t *testing.T) {
	svc, repo, _ := newTestService()
	svc.Register(context.Background(), auth.RegisterRequest{ //nolint:errcheck
		Email: "f@example.com", Password: "SecurePass1!", FullName: "Frank",
	}, "", "")

	for i := 0; i < 3; i++ {
		svc.Login(context.Background(), //nolint:errcheck
			auth.LoginRequest{Email: "f@example.com", Password: "Wrong1!"},
			"127.0.0.1", "")
	}

	user := repo.users["f@example.com"]
	if user.FailedLogins != 3 {
		t.Errorf("expected 3 failed logins, got %d", user.FailedLogins)
	}
}

func TestService_Login_LocksAfterMaxFailedAttempts(t *testing.T) {
	svc, repo, _ := newTestService()
	svc.Register(context.Background(), auth.RegisterRequest{ //nolint:errcheck
		Email: "g@example.com", Password: "SecurePass1!", FullName: "Grace",
	}, "", "")

	// 5 failed attempts should trigger lockout
	for i := 0; i < 5; i++ {
		svc.Login(context.Background(), //nolint:errcheck
			auth.LoginRequest{Email: "g@example.com", Password: "WrongPass1!"},
			"127.0.0.1", "")
	}

	user := repo.users["g@example.com"]
	if user.LockedUntil == nil {
		t.Fatal("user should be locked after 5 failed attempts")
	}
	if time.Now().After(*user.LockedUntil) {
		t.Error("locked_until should be in the future")
	}
}

func TestService_Login_LockedAccountFails(t *testing.T) {
	svc, repo, _ := newTestService()
	svc.Register(context.Background(), auth.RegisterRequest{ //nolint:errcheck
		Email: "h@example.com", Password: "SecurePass1!", FullName: "Hank",
	}, "", "")

	// Manually lock the account
	future := time.Now().Add(30 * time.Minute)
	user := repo.users["h@example.com"]
	user.LockedUntil = &future

	_, _, err := svc.Login(context.Background(),
		auth.LoginRequest{Email: "h@example.com", Password: "SecurePass1!"},
		"127.0.0.1", "")
	if !errors.Is(err, auth.ErrAccountLocked) {
		t.Errorf("expected ErrAccountLocked, got %v", err)
	}
}

func TestService_Login_SuccessResetsFailedLogins(t *testing.T) {
	svc, repo, _ := newTestService()
	svc.Register(context.Background(), auth.RegisterRequest{ //nolint:errcheck
		Email: "i@example.com", Password: "SecurePass1!", FullName: "Iris",
	}, "", "")

	// Fail once
	svc.Login(context.Background(), //nolint:errcheck
		auth.LoginRequest{Email: "i@example.com", Password: "WrongPass1!"}, "127.0.0.1", "")

	// Succeed
	svc.Login(context.Background(), //nolint:errcheck
		auth.LoginRequest{Email: "i@example.com", Password: "SecurePass1!"}, "127.0.0.1", "")

	user := repo.users["i@example.com"]
	if user.FailedLogins != 0 {
		t.Errorf("successful login should reset failed_logins to 0, got %d", user.FailedLogins)
	}
}

func TestService_ValidateAccessToken_ValidTokenPasses(t *testing.T) {
	svc, _, _ := newTestService()
	regResp, _, _ := svc.Register(context.Background(),
		auth.RegisterRequest{Email: "j@example.com", Password: "SecurePass1!", FullName: "Jane"},
		"", "")

	claims, err := svc.ValidateAccessToken(regResp.Tokens.AccessToken)
	if err != nil {
		t.Fatalf("valid token should pass validation: %v", err)
	}
	if claims.UserID == "" {
		t.Error("claims must contain user ID")
	}
	if claims.Role != "user" {
		t.Errorf("expected role user, got %q", claims.Role)
	}
	if claims.WalletID == "" {
		t.Error("claims must contain wallet ID")
	}
}

func TestService_ValidateAccessToken_TamperedTokenFails(t *testing.T) {
	svc, _, _ := newTestService()
	regResp, _, _ := svc.Register(context.Background(),
		auth.RegisterRequest{Email: "k@example.com", Password: "SecurePass1!", FullName: "Karl"},
		"", "")

	// Tamper: flip the last character of the token
	token := regResp.Tokens.AccessToken
	tampered := token[:len(token)-1] + "X"

	_, err := svc.ValidateAccessToken(tampered)
	if !errors.Is(err, auth.ErrTokenInvalid) {
		t.Errorf("tampered token should fail validation, got %v", err)
	}
}

func TestService_ValidateAccessToken_EmptyTokenFails(t *testing.T) {
	svc, _, _ := newTestService()
	_, err := svc.ValidateAccessToken("")
	if !errors.Is(err, auth.ErrTokenInvalid) {
		t.Errorf("empty token should fail, got %v", err)
	}
}

func TestService_RefreshToken_RotatesToken(t *testing.T) {
	svc, _, _ := newTestService()
	svc.Register(context.Background(), auth.RegisterRequest{ //nolint:errcheck
		Email: "l@example.com", Password: "SecurePass1!", FullName: "Lena",
	}, "", "")

	_, origRefreshToken, _ := svc.Login(context.Background(),
		auth.LoginRequest{Email: "l@example.com", Password: "SecurePass1!"},
		"127.0.0.1", "agent")

	// Refresh
	_, newRefreshToken, err := svc.RefreshToken(context.Background(), origRefreshToken, "127.0.0.1", "agent")
	if err != nil {
		t.Fatalf("refresh should succeed: %v", err)
	}

	// New token must be different from old
	if newRefreshToken == origRefreshToken {
		t.Error("token rotation must issue a new refresh token, not reuse the old one")
	}

	// Old token must no longer work (it was deleted)
	_, _, err = svc.RefreshToken(context.Background(), origRefreshToken, "127.0.0.1", "agent")
	if err == nil {
		t.Error("old refresh token must be invalid after rotation")
	}
}

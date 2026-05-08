package auth_test

import (
	"strings"
	"testing"
	"time"

	"github.com/cbdc-simulator/backend/internal/auth"
)

// ── RegisterRequest validation ────────────────────────────────────────────────

func TestRegisterRequest_ValidInputPasses(t *testing.T) {
	req := auth.RegisterRequest{
		Email:    "alice@example.com",
		Password: "SecurePass1!",
		FullName: "Alice Johnson",
	}
	if err := req.Validate(); err != nil {
		t.Errorf("valid request should pass validation, got: %v", err)
	}
}

func TestRegisterRequest_EmailNormalisedToLowercase(t *testing.T) {
	req := auth.RegisterRequest{
		Email:    "Alice@EXAMPLE.COM",
		Password: "SecurePass1!",
		FullName: "Alice",
	}
	req.Validate() //nolint:errcheck
	if req.Email != "alice@example.com" {
		t.Errorf("email should be normalised to lowercase, got %q", req.Email)
	}
}

func TestRegisterRequest_InvalidEmailFails(t *testing.T) {
	cases := []string{
		"notanemail",
		"missing@tld",
		"@nodomain.com",
		"spaces in@email.com",
		"",
	}
	for _, email := range cases {
		req := auth.RegisterRequest{Email: email, Password: "SecurePass1!", FullName: "Alice"}
		if err := req.Validate(); err == nil {
			t.Errorf("email %q should fail validation", email)
		}
	}
}

func TestRegisterRequest_WeakPasswordFails(t *testing.T) {
	cases := []struct {
		password string
		desc     string
	}{
		{"short1!", "too short (< 10 chars)"},
		{"alllowercase1!", "no uppercase"},
		{"ALLUPPERCASE1!", "no lowercase... wait — no digit check in spec"},
		{"NoSpecialChar1", "no special character"},
		{"NoDigitHere!!!", "no digit"},
		{strings.Repeat("a", 73) + "A1!", "too long (> 72 chars)"},
	}
	for _, tc := range cases {
		req := auth.RegisterRequest{Email: "a@b.com", Password: tc.password, FullName: "Alice"}
		if err := req.Validate(); err == nil {
			t.Errorf("password %q (%s) should fail validation", tc.password, tc.desc)
		}
	}
}

func TestRegisterRequest_StrongPasswordPasses(t *testing.T) {
	cases := []string{
		"SecurePass1!",
		"MyP@ssw0rd",
		"Correct-Horse-Battery-1",
		"Abcdefgh1!",
	}
	for _, p := range cases {
		req := auth.RegisterRequest{Email: "a@b.com", Password: p, FullName: "Alice"}
		if err := req.Validate(); err != nil {
			t.Errorf("password %q should pass validation, got: %v", p, err)
		}
	}
}

func TestRegisterRequest_ShortNameFails(t *testing.T) {
	req := auth.RegisterRequest{Email: "a@b.com", Password: "SecurePass1!", FullName: "A"}
	if err := req.Validate(); err == nil {
		t.Error("single-character name should fail validation")
	}
}

func TestRegisterRequest_TrimmedNamePasses(t *testing.T) {
	req := auth.RegisterRequest{
		Email:    "a@b.com",
		Password: "SecurePass1!",
		FullName: "  Alice  ", // has surrounding whitespace
	}
	if err := req.Validate(); err != nil {
		t.Errorf("name with whitespace should pass after trimming, got: %v", err)
	}
}

// ── LoginRequest validation ───────────────────────────────────────────────────

func TestLoginRequest_ValidInputPasses(t *testing.T) {
	req := auth.LoginRequest{Email: "alice@example.com", Password: "anypassword"}
	if err := req.Validate(); err != nil {
		t.Errorf("valid login request should pass, got: %v", err)
	}
}

func TestLoginRequest_EmptyEmailFails(t *testing.T) {
	req := auth.LoginRequest{Email: "", Password: "password"}
	if err := req.Validate(); err == nil {
		t.Error("empty email should fail validation")
	}
}

func TestLoginRequest_EmptyPasswordFails(t *testing.T) {
	req := auth.LoginRequest{Email: "a@b.com", Password: ""}
	if err := req.Validate(); err == nil {
		t.Error("empty password should fail validation")
	}
}

// ── User domain model ─────────────────────────────────────────────────────────

func TestUser_IsLocked_NilLockedUntil(t *testing.T) {
	user := &auth.User{LockedUntil: nil}
	if user.IsLocked() {
		t.Error("user with nil locked_until should not be locked")
	}
}

func TestUser_IsLocked_PastTime(t *testing.T) {
	past := time.Now().Add(-time.Minute)
	user := &auth.User{LockedUntil: &past}
	if user.IsLocked() {
		t.Error("user locked until a past time should not be locked")
	}
}

func TestUser_IsLocked_FutureTime(t *testing.T) {
	future := time.Now().Add(30 * time.Minute)
	user := &auth.User{LockedUntil: &future}
	if !user.IsLocked() {
		t.Error("user locked until a future time should be locked")
	}
}

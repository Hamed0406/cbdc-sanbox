// Tests for the crypto package.
// These are pure unit tests — no DB or Redis needed.
// We test every exported function including edge cases and security properties.
package crypto_test

import (
	"strings"
	"testing"

	"github.com/cbdc-simulator/backend/pkg/crypto"
)

// ── Password hashing ─────────────────────────────────────────────────────────

func TestHashPassword_ProducesNonEmptyHash(t *testing.T) {
	hash, err := crypto.HashPassword("SecurePass1!")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
}

func TestHashPassword_DifferentHashesForSameInput(t *testing.T) {
	// bcrypt generates a random salt on every call — same password must
	// produce different hashes each time. If they're the same, the salt
	// isn't working and rainbow table attacks become trivial.
	hash1, _ := crypto.HashPassword("SecurePass1!")
	hash2, _ := crypto.HashPassword("SecurePass1!")
	if hash1 == hash2 {
		t.Fatal("two hashes of the same password must differ (bcrypt uses random salt)")
	}
}

func TestHashPassword_AlwaysProduces60CharBcryptHash(t *testing.T) {
	// bcrypt output is always exactly 60 characters.
	// Our DB column is VARCHAR(60) — if this changes, the insert will fail.
	hash, _ := crypto.HashPassword("SecurePass1!")
	if len(hash) != 60 {
		t.Fatalf("expected 60-char bcrypt hash, got %d chars", len(hash))
	}
}

func TestVerifyPassword_CorrectPasswordPasses(t *testing.T) {
	hash, _ := crypto.HashPassword("SecurePass1!")
	if err := crypto.VerifyPassword(hash, "SecurePass1!"); err != nil {
		t.Fatalf("correct password should verify: %v", err)
	}
}

func TestVerifyPassword_WrongPasswordFails(t *testing.T) {
	hash, _ := crypto.HashPassword("SecurePass1!")
	if err := crypto.VerifyPassword(hash, "WrongPass1!"); err == nil {
		t.Fatal("wrong password should not verify")
	}
}

func TestVerifyPassword_EmptyPasswordFails(t *testing.T) {
	hash, _ := crypto.HashPassword("SecurePass1!")
	if err := crypto.VerifyPassword(hash, ""); err == nil {
		t.Fatal("empty password should not verify")
	}
}

func TestVerifyPassword_CaseSensitive(t *testing.T) {
	// Passwords are case-sensitive. "password" != "Password".
	hash, _ := crypto.HashPassword("SecurePass1!")
	if err := crypto.VerifyPassword(hash, "securepass1!"); err == nil {
		t.Fatal("password verification must be case-sensitive")
	}
}

// ── Transaction signing ───────────────────────────────────────────────────────

func TestSignTransaction_ReturnsDeterministicSignature(t *testing.T) {
	// Same inputs must always produce the same HMAC signature.
	// If this is non-deterministic, verification would randomly fail.
	sig1 := crypto.SignTransaction("key", "txn1", "wallet1", "wallet2", 1000, 1234567890)
	sig2 := crypto.SignTransaction("key", "txn1", "wallet1", "wallet2", 1000, 1234567890)
	if sig1 != sig2 {
		t.Fatal("signing same payload twice must produce identical signatures")
	}
}

func TestSignTransaction_ReturnsHexString(t *testing.T) {
	sig := crypto.SignTransaction("key", "txn1", "wallet1", "wallet2", 1000, 1234567890)
	if len(sig) != 64 {
		t.Fatalf("HMAC-SHA256 hex output must be 64 chars, got %d", len(sig))
	}
	// Must be valid hex
	for _, c := range sig {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Fatalf("signature contains non-hex character: %c", c)
		}
	}
}

func TestVerifyTransactionSignature_ValidSignaturePasses(t *testing.T) {
	key := "mysigningkey"
	sig := crypto.SignTransaction(key, "txn1", "walletA", "walletB", 5000, 1000000)
	if !crypto.VerifyTransactionSignature(key, sig, "txn1", "walletA", "walletB", 5000, 1000000) {
		t.Fatal("valid signature should pass verification")
	}
}

func TestVerifyTransactionSignature_TamperedAmountFails(t *testing.T) {
	// This is the core tamper-detection test: if someone changes the amount
	// in the DB, the signature will no longer match.
	key := "mysigningkey"
	sig := crypto.SignTransaction(key, "txn1", "walletA", "walletB", 5000, 1000000)
	// Tamper: change amount from 5000 to 9999
	if crypto.VerifyTransactionSignature(key, sig, "txn1", "walletA", "walletB", 9999, 1000000) {
		t.Fatal("tampered amount must fail signature verification")
	}
}

func TestVerifyTransactionSignature_TamperedSenderFails(t *testing.T) {
	key := "mysigningkey"
	sig := crypto.SignTransaction(key, "txn1", "walletA", "walletB", 5000, 1000000)
	// Tamper: change sender
	if crypto.VerifyTransactionSignature(key, sig, "txn1", "walletEVIL", "walletB", 5000, 1000000) {
		t.Fatal("tampered sender must fail signature verification")
	}
}

func TestVerifyTransactionSignature_WrongKeyFails(t *testing.T) {
	sig := crypto.SignTransaction("correctkey", "txn1", "walletA", "walletB", 5000, 1000000)
	if crypto.VerifyTransactionSignature("wrongkey", sig, "txn1", "walletA", "walletB", 5000, 1000000) {
		t.Fatal("wrong signing key must fail verification")
	}
}

func TestVerifyTransactionSignature_DifferentKeysDifferentSigs(t *testing.T) {
	sig1 := crypto.SignTransaction("key1", "txn1", "walletA", "walletB", 5000, 1000000)
	sig2 := crypto.SignTransaction("key2", "txn1", "walletA", "walletB", 5000, 1000000)
	if sig1 == sig2 {
		t.Fatal("different signing keys must produce different signatures")
	}
}

// ── QR payload signing ────────────────────────────────────────────────────────

func TestSignQRPayload_DeterministicAndVerifiable(t *testing.T) {
	key := "merchantapikey"
	sig := crypto.SignQRPayload(key, "wallet1", 4999, "ORDER-001", 1700000000)

	if !crypto.VerifyQRSignature(key, sig, "wallet1", 4999, "ORDER-001", 1700000000) {
		t.Fatal("valid QR signature should verify")
	}
}

func TestVerifyQRSignature_TamperedAmountFails(t *testing.T) {
	// Prevents a malicious customer from changing the QR code amount.
	key := "merchantapikey"
	sig := crypto.SignQRPayload(key, "wallet1", 4999, "ORDER-001", 1700000000)
	// Tamper: change amount from 4999 (DD$49.99) to 1 (DD$0.01)
	if crypto.VerifyQRSignature(key, sig, "wallet1", 1, "ORDER-001", 1700000000) {
		t.Fatal("tampered QR amount must fail verification")
	}
}

// ── Secure token generation ───────────────────────────────────────────────────

func TestGenerateSecureToken_ReturnsToken(t *testing.T) {
	token, err := crypto.GenerateSecureToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token == "" {
		t.Fatal("token must not be empty")
	}
}

func TestGenerateSecureToken_IsUnique(t *testing.T) {
	// Generate 100 tokens and verify no duplicates.
	// With 256-bit entropy, a collision here would be astronomically unlikely —
	// if we see one, something is very wrong with the random source.
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		token, _ := crypto.GenerateSecureToken()
		if seen[token] {
			t.Fatalf("duplicate token generated at iteration %d — RNG is broken", i)
		}
		seen[token] = true
	}
}

func TestGenerateSecureToken_IsHex64Chars(t *testing.T) {
	// 32 bytes encoded as hex = 64 characters.
	token, _ := crypto.GenerateSecureToken()
	if len(token) != 64 {
		t.Fatalf("expected 64-char hex token, got %d chars", len(token))
	}
}

func TestHashToken_DeterministicAndNonReversible(t *testing.T) {
	token := "somesecrettoken"
	hash1 := crypto.HashToken(token)
	hash2 := crypto.HashToken(token)

	// Deterministic: same token always hashes to same value (SHA-256, no salt)
	if hash1 != hash2 {
		t.Fatal("HashToken must be deterministic")
	}
	// Non-reversible: hash must not equal the original token
	if hash1 == token {
		t.Fatal("HashToken must not return the plain token")
	}
	// SHA-256 hex output is always 64 chars
	if len(hash1) != 64 {
		t.Fatalf("expected 64-char SHA-256 hex hash, got %d", len(hash1))
	}
}

func TestHashToken_DifferentTokensDifferentHashes(t *testing.T) {
	hash1 := crypto.HashToken("token_alice")
	hash2 := crypto.HashToken("token_bob")
	if hash1 == hash2 {
		t.Fatal("different tokens must produce different hashes")
	}
}

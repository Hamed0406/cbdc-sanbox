// Package crypto provides cryptographic primitives used throughout the app:
// - bcrypt password hashing (for user credentials)
// - HMAC-SHA256 transaction signing (for payment payload integrity)
// - Secure random token generation (for refresh tokens, API keys)
//
// WHY HMAC and not asymmetric signing (RSA/ECDSA)?
// In a real CBDC, you'd use HSM-backed asymmetric keys so that
// anyone with the public key can verify a transaction without access to the signing key.
// For this simulator, HMAC is appropriate: simpler, no key infrastructure needed,
// and provides the same tamper-detection guarantee within the system boundary.
package crypto

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

const (
	// BcryptCost sets the work factor for password hashing.
	// Cost 12 takes ~250ms on modern hardware — acceptable UX latency,
	// but makes brute-force attacks ~250ms per attempt instead of microseconds.
	BcryptCost = 12

	// TokenBytes is the number of random bytes in a refresh token / API key.
	// 32 bytes = 256 bits of entropy — unguessable with current compute.
	TokenBytes = 32
)

// HashPassword creates a bcrypt hash of the plaintext password.
// The hash includes the salt and cost factor, so only the hash needs to be stored.
func HashPassword(plaintext string) (string, error) {
	// bcrypt silently truncates at 72 bytes — enforce max at the service layer.
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), BcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

// VerifyPassword checks whether plaintext matches the stored bcrypt hash.
// Returns nil on match, error on mismatch or malformed hash.
func VerifyPassword(hash, plaintext string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext))
}

// SignTransaction creates an HMAC-SHA256 signature over the transaction payload.
// The payload format is: "txnID|senderWalletID|receiverWalletID|amountCents|unixTimestamp"
// This signature is stored with the transaction so auditors can detect tampering.
func SignTransaction(signingKey, txnID, senderWalletID, receiverWalletID string, amountCents int64, timestamp int64) string {
	payload := fmt.Sprintf("%s|%s|%s|%d|%d",
		txnID, senderWalletID, receiverWalletID, amountCents, timestamp)
	return hmacSHA256(signingKey, payload)
}

// VerifyTransactionSignature recomputes the expected signature and compares
// using constant-time comparison to prevent timing attacks.
func VerifyTransactionSignature(signingKey, expectedSig, txnID, senderWalletID, receiverWalletID string, amountCents, timestamp int64) bool {
	expected := SignTransaction(signingKey, txnID, senderWalletID, receiverWalletID, amountCents, timestamp)
	// hmac.Equal uses constant-time comparison — prevents timing side-channel attacks
	// that could let an attacker learn the signature one byte at a time.
	return hmac.Equal([]byte(expected), []byte(expectedSig))
}

// SignQRPayload creates an HMAC-SHA256 signature for a merchant QR code payload.
// Format: "walletID|amountCents|merchantRef|expiresUnix"
// Prevents a malicious user from modifying a QR code's amount or destination.
func SignQRPayload(merchantAPIKey, walletID string, amountCents int64, ref string, expiresUnix int64) string {
	payload := fmt.Sprintf("%s|%d|%s|%d", walletID, amountCents, ref, expiresUnix)
	return hmacSHA256(merchantAPIKey, payload)
}

// VerifyQRSignature validates a scanned QR code's signature.
func VerifyQRSignature(merchantAPIKey, sig, walletID string, amountCents int64, ref string, expiresUnix int64) bool {
	expected := SignQRPayload(merchantAPIKey, walletID, amountCents, ref, expiresUnix)
	return hmac.Equal([]byte(expected), []byte(sig))
}

// GenerateSecureToken creates a cryptographically random hex string.
// Used for refresh tokens, merchant API keys, and session IDs.
// crypto/rand uses the OS entropy source (/dev/urandom on Linux) — not math/rand.
func GenerateSecureToken() (string, error) {
	b := make([]byte, TokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// HashToken creates a SHA-256 hash of a token for safe storage.
// We store the hash (not the raw token) in the DB so that a DB leak
// doesn't expose valid session tokens.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// hmacSHA256 is the internal HMAC computation used by all signing functions.
func hmacSHA256(key, message string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

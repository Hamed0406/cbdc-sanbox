// Package qrcode generates and parses cbdc:// payment URIs.
// The URI scheme is inspired by BIP-21 (Bitcoin payment URIs).
//
// Format:  cbdc://pay?merchant=<uuid>&amount=<cents>&ref=<ref>&desc=<desc>&expires=<unix>&sig=<hmac>
//
// The sig field is HMAC-SHA256(signingKey, "merchant=<id>&amount=<cents>&expires=<unix>")
// covering only the fields that must not be tampered with. ref and desc are
// informational and not signed.
package qrcode

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const scheme = "cbdc"

// Params holds the decoded fields of a cbdc:// URI.
type Params struct {
	MerchantID  uuid.UUID
	AmountCents int64
	Reference   string
	Description string
	ExpiresAt   time.Time
	Signature   string
}

// BuildURI creates a signed cbdc:// payment URI for a payment request.
func BuildURI(merchantID uuid.UUID, amountCents int64, reference, description string, expiresAt time.Time, signingKey string) string {
	expiresUnix := strconv.FormatInt(expiresAt.Unix(), 10)
	sig := sign(signingKey, merchantID.String(), strconv.FormatInt(amountCents, 10), expiresUnix)

	q := url.Values{}
	q.Set("merchant", merchantID.String())
	q.Set("amount", strconv.FormatInt(amountCents, 10))
	q.Set("expires", expiresUnix)
	q.Set("sig", sig)
	if reference != "" {
		q.Set("ref", reference)
	}
	if description != "" {
		q.Set("desc", description)
	}

	return fmt.Sprintf("%s://pay?%s", scheme, q.Encode())
}

// ParseURI decodes a cbdc:// URI into a Params struct.
// Returns an error if the URI is malformed or has an unknown scheme.
// Does NOT verify the signature or expiry — callers must call Verify().
func ParseURI(raw string) (*Params, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid URI: %w", err)
	}
	if u.Scheme != scheme {
		return nil, fmt.Errorf("unsupported URI scheme %q, expected %q", u.Scheme, scheme)
	}
	if u.Host != "pay" {
		return nil, fmt.Errorf("unsupported URI host %q, expected \"pay\"", u.Host)
	}

	q := u.Query()

	merchantID, err := uuid.Parse(q.Get("merchant"))
	if err != nil {
		return nil, fmt.Errorf("invalid merchant ID: %w", err)
	}

	amountCents, err := strconv.ParseInt(q.Get("amount"), 10, 64)
	if err != nil || amountCents <= 0 {
		return nil, fmt.Errorf("invalid amount")
	}

	expiresUnix, err := strconv.ParseInt(q.Get("expires"), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid expires timestamp")
	}

	sig := strings.TrimSpace(q.Get("sig"))
	if sig == "" {
		return nil, fmt.Errorf("missing signature")
	}

	return &Params{
		MerchantID:  merchantID,
		AmountCents: amountCents,
		Reference:   q.Get("ref"),
		Description: q.Get("desc"),
		ExpiresAt:   time.Unix(expiresUnix, 0).UTC(),
		Signature:   sig,
	}, nil
}

// Verify checks the signature and expiry of a parsed URI.
func Verify(p *Params, signingKey string, now time.Time) error {
	if now.After(p.ExpiresAt) {
		return fmt.Errorf("payment URI has expired")
	}

	expiresUnix := strconv.FormatInt(p.ExpiresAt.Unix(), 10)
	expected := sign(signingKey, p.MerchantID.String(), strconv.FormatInt(p.AmountCents, 10), expiresUnix)
	if !hmac.Equal([]byte(p.Signature), []byte(expected)) {
		return fmt.Errorf("invalid payment URI signature")
	}
	return nil
}

// sign computes HMAC-SHA256 over "merchant=<id>&amount=<cents>&expires=<unix>".
func sign(key, merchantID, amount, expires string) string {
	msg := fmt.Sprintf("merchant=%s&amount=%s&expires=%s", merchantID, amount, expires)
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))
}

package qrcode_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cbdc-simulator/backend/pkg/qrcode"
)

const testKey = "super-secret-signing-key"

func TestBuildAndParse(t *testing.T) {
	merchantID := uuid.New()
	amount := int64(5000)
	ref := "ORDER-123"
	desc := "Coffee purchase"
	exp := time.Now().Add(15 * time.Minute).UTC().Truncate(time.Second)

	uri := qrcode.BuildURI(merchantID, amount, ref, desc, exp, testKey)

	if !strings.HasPrefix(uri, "cbdc://pay?") {
		t.Fatalf("expected cbdc:// URI, got %s", uri)
	}

	p, err := qrcode.ParseURI(uri)
	if err != nil {
		t.Fatalf("ParseURI: %v", err)
	}
	if p.MerchantID != merchantID {
		t.Errorf("merchant: want %s, got %s", merchantID, p.MerchantID)
	}
	if p.AmountCents != amount {
		t.Errorf("amount: want %d, got %d", amount, p.AmountCents)
	}
	if p.Reference != ref {
		t.Errorf("ref: want %s, got %s", ref, p.Reference)
	}
	if p.Description != desc {
		t.Errorf("desc: want %s, got %s", desc, p.Description)
	}
	if !p.ExpiresAt.Equal(exp) {
		t.Errorf("expires: want %s, got %s", exp, p.ExpiresAt)
	}
}

func TestVerify_Valid(t *testing.T) {
	merchantID := uuid.New()
	exp := time.Now().Add(15 * time.Minute)
	uri := qrcode.BuildURI(merchantID, 1000, "ref", "desc", exp, testKey)

	p, err := qrcode.ParseURI(uri)
	if err != nil {
		t.Fatal(err)
	}
	if err := qrcode.Verify(p, testKey, time.Now()); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}
}

func TestVerify_Expired(t *testing.T) {
	merchantID := uuid.New()
	exp := time.Now().Add(-1 * time.Minute) // in the past
	uri := qrcode.BuildURI(merchantID, 1000, "", "", exp, testKey)

	p, err := qrcode.ParseURI(uri)
	if err != nil {
		t.Fatal(err)
	}
	if err := qrcode.Verify(p, testKey, time.Now()); err == nil {
		t.Error("expected expiry error, got nil")
	}
}

func TestVerify_WrongKey(t *testing.T) {
	merchantID := uuid.New()
	exp := time.Now().Add(15 * time.Minute)
	uri := qrcode.BuildURI(merchantID, 1000, "", "", exp, testKey)

	p, err := qrcode.ParseURI(uri)
	if err != nil {
		t.Fatal(err)
	}
	if err := qrcode.Verify(p, "wrong-key", time.Now()); err == nil {
		t.Error("expected signature error, got nil")
	}
}

func TestParseURI_InvalidScheme(t *testing.T) {
	_, err := qrcode.ParseURI("https://example.com/pay?merchant=abc")
	if err == nil {
		t.Error("expected error for wrong scheme")
	}
}

func TestParseURI_MissingSignature(t *testing.T) {
	merchantID := uuid.New()
	uri := "cbdc://pay?merchant=" + merchantID.String() + "&amount=1000&expires=9999999999"
	_, err := qrcode.ParseURI(uri)
	if err == nil {
		t.Error("expected error for missing sig")
	}
}

func TestBuildURI_NoRefOrDesc(t *testing.T) {
	merchantID := uuid.New()
	exp := time.Now().Add(5 * time.Minute)
	uri := qrcode.BuildURI(merchantID, 100, "", "", exp, testKey)

	if strings.Contains(uri, "ref=") {
		t.Error("expected no ref param when empty")
	}
	if strings.Contains(uri, "desc=") {
		t.Error("expected no desc param when empty")
	}
}

func TestTamperResistance(t *testing.T) {
	merchantID := uuid.New()
	exp := time.Now().Add(15 * time.Minute)
	uri := qrcode.BuildURI(merchantID, 1000, "", "", exp, testKey)

	// Replace amount in URI — sig should not match
	tampered := strings.Replace(uri, "amount=1000", "amount=999999", 1)
	p, err := qrcode.ParseURI(tampered)
	if err != nil {
		t.Fatalf("parse tampered: %v", err)
	}
	if err := qrcode.Verify(p, testKey, time.Now()); err == nil {
		t.Error("expected signature error after tampering, got nil")
	}
}

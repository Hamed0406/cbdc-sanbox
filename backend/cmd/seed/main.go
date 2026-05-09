// cmd/seed inserts deterministic demo data into the database.
// It is idempotent: running it twice produces the same result.
// Run with:  go run ./cmd/seed
// Or via:    make seed  (which runs this inside the backend container)
package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/cbdc-simulator/backend/pkg/database"
)

const bcryptCost = 12

func main() {
	ctx := context.Background()

	slog.Info("seed starting")

	db, err := database.New(ctx, database.Config{
		Host:         getEnv("DB_HOST", "localhost"),
		Port:         getEnv("DB_PORT", "5432"),
		Name:         getEnv("DB_NAME", "cbdc_db"),
		User:         getEnv("DB_USER", "cbdc_app"),
		Password:     mustEnv("DB_PASSWORD"),
		MaxOpenConns: 5,
		MaxIdleConns: 2,
		ConnTimeout:  5 * time.Second,
	})
	if err != nil {
		slog.Error("db connect", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// ── Demo users ────────────────────────────────────────────────────────────
	// Passwords all meet the 10-char, upper+lower+digit+special rule.
	users := []seedUser{
		{Email: "admin@cbdc.local", Password: "Admin1234!", Name: "System Admin", Role: "admin"},
		{Email: "alice@example.com", Password: "Alice1234!", Name: "Alice Johnson", Role: "user"},
		{Email: "bob@example.com", Password: "Bob12345!", Name: "Bob Smith", Role: "user"},
		{Email: "carol@example.com", Password: "Carol123!", Name: "Carol Williams", Role: "user"},
		{Email: "cafe@example.com", Password: "Cafe1234!", Name: "Good Coffee Co.", Role: "merchant"},
		{Email: "market@example.com", Password: "Market123!", Name: "Digital Market", Role: "merchant"},
	}

	// Initial balances (in cents). Admin has no wallet.
	balances := map[string]int64{
		"alice@example.com":  492900, // DD$ 4,929.00
		"bob@example.com":    163150, // DD$ 1,631.50
		"carol@example.com":  41000,  // DD$   410.00
		"cafe@example.com":   2950,   // DD$    29.50
		"market@example.com": 0,
	}

	// Merchant business names
	merchantNames := map[string]string{
		"cafe@example.com":   "Good Coffee Co.",
		"market@example.com": "Digital Market",
	}
	merchantTypes := map[string]string{
		"cafe@example.com":   "food_and_beverage",
		"market@example.com": "retail",
	}

	// Keep track of user IDs for wallet/issuance creation
	userIDs := make(map[string]uuid.UUID)
	walletIDs := make(map[string]uuid.UUID)

	signingKey := getEnv("SIGNING_KEY", "dev-signing-key-change-in-prod")

	for _, u := range users {
		id, err := upsertUser(ctx, db, u)
		if err != nil {
			slog.Error("upsert user", "email", u.Email, "error", err)
			os.Exit(1)
		}
		userIDs[u.Email] = id
		slog.Info("user ready", "email", u.Email, "id", id)
	}

	// ── Wallets ───────────────────────────────────────────────────────────────
	// Admin has no wallet (they issue CBDC, don't hold it)
	for _, u := range users {
		if u.Role == "admin" {
			continue
		}
		wid, err := upsertWallet(ctx, db, userIDs[u.Email])
		if err != nil {
			slog.Error("upsert wallet", "email", u.Email, "error", err)
			os.Exit(1)
		}
		walletIDs[u.Email] = wid
		slog.Info("wallet ready", "email", u.Email, "wallet_id", wid)
	}

	// ── Balances ──────────────────────────────────────────────────────────────
	// Set balances directly via ledger entries so the bookkeeping is correct.
	adminID := userIDs["admin@cbdc.local"]
	for email, targetCents := range balances {
		if targetCents == 0 {
			continue
		}
		wid := walletIDs[email]
		if err := seedBalance(ctx, db, adminID, wid, targetCents, signingKey); err != nil {
			slog.Error("seed balance", "email", email, "error", err)
			os.Exit(1)
		}
		slog.Info("balance set", "email", email, "cents", targetCents)
	}

	// ── Merchant profiles ─────────────────────────────────────────────────────
	for email, bizName := range merchantNames {
		uid := userIDs[email]
		bizType := merchantTypes[email]
		if err := upsertMerchant(ctx, db, uid, bizName, bizType); err != nil {
			slog.Error("upsert merchant", "email", email, "error", err)
			os.Exit(1)
		}
		slog.Info("merchant ready", "email", email, "business", bizName)
	}

	slog.Info("seed complete",
		"users", len(users),
		"wallets", len(walletIDs),
	)
}

// ── Domain helpers ────────────────────────────────────────────────────────────

type seedUser struct {
	Email    string
	Password string
	Name     string
	Role     string
}

// upsertUser inserts the user if the email doesn't exist, or updates password+name if it does.
// Returns the user's UUID.
func upsertUser(ctx context.Context, db *database.Pool, u seedUser) (uuid.UUID, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(u.Password), bcryptCost)
	if err != nil {
		return uuid.Nil, fmt.Errorf("hash password: %w", err)
	}

	var id uuid.UUID
	err = db.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, full_name, role, kyc_status)
		VALUES ($1, $2, $3, $4::user_role, 'verified')
		ON CONFLICT (email) DO UPDATE
		    SET password_hash = EXCLUDED.password_hash,
		        full_name     = EXCLUDED.full_name,
		        role          = EXCLUDED.role,
		        kyc_status    = 'verified',
		        is_active     = TRUE,
		        failed_logins = 0,
		        locked_until  = NULL
		RETURNING id
	`, u.Email, string(hash), u.Name, u.Role).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("upsert user %s: %w", u.Email, err)
	}
	return id, nil
}

// upsertWallet creates a wallet for the user if one doesn't exist.
// Returns the wallet UUID.
func upsertWallet(ctx context.Context, db *database.Pool, userID uuid.UUID) (uuid.UUID, error) {
	var wid uuid.UUID
	err := db.QueryRow(ctx, `
		INSERT INTO wallets (user_id, currency, balance)
		VALUES ($1, 'DD$', 0)
		ON CONFLICT (user_id, currency) DO UPDATE SET updated_at = NOW()
		RETURNING id
	`, userID).Scan(&wid)
	if err != nil {
		return uuid.Nil, fmt.Errorf("upsert wallet: %w", err)
	}
	return wid, nil
}

// seedBalance sets a wallet's balance by creating a CBDC issuance if the current
// balance is less than targetCents. Idempotent: won't issue if balance already meets target.
func seedBalance(ctx context.Context, db *database.Pool, adminID, walletID uuid.UUID, targetCents int64, signingKey string) error {
	// Check current balance
	var current int64
	if err := db.QueryRow(ctx, `SELECT balance FROM wallets WHERE id = $1`, walletID).Scan(&current); err != nil {
		return fmt.Errorf("get balance: %w", err)
	}
	if current >= targetCents {
		return nil // already has enough
	}
	issueCents := targetCents - current

	// Begin transaction
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	now := time.Now().UTC()
	txnID := uuid.New()

	// Signature over the issuance
	sig := signIssuance(signingKey, txnID.String(), walletID.String(), issueCents, now.Unix())

	// Insert transaction
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions
		    (id, type, status, receiver_wallet_id, amount_cents, signature, settled_at)
		VALUES ($1, 'ISSUANCE', 'SETTLED', $2, $3, $4, NOW())
	`, txnID, walletID, issueCents, sig)
	if err != nil {
		return fmt.Errorf("insert transaction: %w", err)
	}

	// Credit ledger entry
	newBalance := current + issueCents
	_, err = tx.Exec(ctx, `
		INSERT INTO ledger_entries (wallet_id, transaction_id, entry_type, amount_cents, balance_after)
		VALUES ($1, $2, 'CREDIT', $3, $4)
	`, walletID, txnID, issueCents, newBalance)
	if err != nil {
		return fmt.Errorf("insert ledger entry: %w", err)
	}

	// Update wallet balance
	_, err = tx.Exec(ctx, `UPDATE wallets SET balance = $1 WHERE id = $2`, newBalance, walletID)
	if err != nil {
		return fmt.Errorf("update wallet balance: %w", err)
	}

	// Insert issuance audit record
	_, err = tx.Exec(ctx, `
		INSERT INTO cbdc_issuance (admin_id, wallet_id, transaction_id, amount_cents, reason, ip_address)
		VALUES ($1, $2, $3, $4, $5, '127.0.0.1')
	`, adminID, walletID, txnID, issueCents, "seed data — initial demo balance")
	if err != nil {
		return fmt.Errorf("insert issuance record: %w", err)
	}

	return tx.Commit(ctx)
}

// upsertMerchant creates a merchant profile if one doesn't exist for the user.
func upsertMerchant(ctx context.Context, db *database.Pool, userID uuid.UUID, businessName, businessType string) error {
	// Generate a deterministic-looking API key from the userID (for demo purposes)
	rawKey := "demo_" + userID.String()[:8] + "_key_for_testing_only"
	keyHash := sha256Hex(rawKey)
	keyPrefix := rawKey[:8]

	_, err := db.Exec(ctx, `
		INSERT INTO merchants (user_id, business_name, business_type, api_key_hash, api_key_prefix)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id) DO UPDATE
		    SET business_name = EXCLUDED.business_name,
		        business_type = EXCLUDED.business_type
	`, userID, businessName, businessType, keyHash, keyPrefix)
	if err != nil {
		return fmt.Errorf("upsert merchant: %w", err)
	}
	return nil
}

// signIssuance computes the HMAC-SHA256 signature for a seed issuance transaction.
func signIssuance(signingKey, txnID, walletID string, amountCents, timestamp int64) string {
	msg := fmt.Sprintf("%s||%s|%d|%d", txnID, walletID, amountCents, timestamp)
	mac := hmac.New(sha256.New, []byte(signingKey))
	mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("required env var not set", "key", key)
		os.Exit(1)
	}
	return v
}

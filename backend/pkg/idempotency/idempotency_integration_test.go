//go:build integration

// Integration tests for the idempotency package.
// Requires a running Redis instance.
// Run with: go test -tags=integration ./pkg/idempotency/...
//
// Environment variables:
//   TEST_REDIS_URL=redis://localhost:6379
package idempotency_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	rdb "github.com/cbdc-simulator/backend/pkg/redis"
	"github.com/cbdc-simulator/backend/pkg/idempotency"
)

func newTestRedis(t *testing.T) *rdb.Client {
	t.Helper()
	host := os.Getenv("TEST_REDIS_HOST")
	if host == "" {
		host = "localhost"
	}
	client, err := rdb.New(context.Background(), rdb.Config{
		Host: host, Port: "6379", Password: "", DB: 1, // DB 1 for tests
	})
	if err != nil {
		t.Skipf("redis not available: %v", err)
	}
	t.Cleanup(func() {
		// Flush test DB after each test to avoid state leakage between tests
		client.Client.FlushDB(context.Background())
		client.Close()
	})
	return client
}

func TestIntegration_CheckReturnsNotFoundForNewKey(t *testing.T) {
	redis := newTestRedis(t)
	store := idempotency.New(redis)

	result, found, err := store.Check(context.Background(), "wallet_1", "key_new_abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("new key should not be found")
	}
	if result != nil {
		t.Fatal("result should be nil for new key")
	}
}

func TestIntegration_StoreAndCheckReturnsOriginalResponse(t *testing.T) {
	redis := newTestRedis(t)
	store := idempotency.New(redis)
	ctx := context.Background()

	walletID := "wallet_alice"
	idemKey := "payment_key_001"
	responseBody := map[string]any{
		"transaction_id": "txn_123",
		"amount_cents":   5000,
		"status":         "CONFIRMED",
	}

	// Store the response
	if err := store.Store(ctx, walletID, idemKey, 201, responseBody); err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// Check — should now find it
	cached, found, err := store.Check(ctx, walletID, idemKey)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if !found {
		t.Fatal("key should be found after Store")
	}
	if cached.StatusCode != 201 {
		t.Errorf("expected status 201, got %d", cached.StatusCode)
	}

	// Verify the body content survived the Redis roundtrip
	var body map[string]any
	if err := json.Unmarshal(cached.Body, &body); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if body["transaction_id"] != "txn_123" {
		t.Errorf("unexpected transaction_id: %v", body["transaction_id"])
	}
}

func TestIntegration_KeyIsScopedPerWallet(t *testing.T) {
	// SECURITY: Two different wallets using the same idempotency key string
	// must be treated as independent — wallet B must not get wallet A's cached response.
	redis := newTestRedis(t)
	store := idempotency.New(redis)
	ctx := context.Background()

	// Wallet A stores a response
	store.Store(ctx, "wallet_A", "shared_key", 201, map[string]any{"owner": "alice"}) //nolint:errcheck

	// Wallet B checks same key — must NOT find wallet A's response
	_, found, err := store.Check(ctx, "wallet_B", "shared_key")
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if found {
		t.Fatal("SECURITY BUG: wallet B must not find wallet A's idempotency entry")
	}
}

func TestIntegration_DuplicateStoreIsIdempotent(t *testing.T) {
	// If two concurrent requests both pass Check() (race window) and both
	// call Store(), only the first one should succeed. The second Store()
	// must not overwrite the first (SetNX semantics).
	redis := newTestRedis(t)
	store := idempotency.New(redis)
	ctx := context.Background()

	walletID := "wallet_race"
	key := "concurrent_key"

	// First store: original response
	store.Store(ctx, walletID, key, 201, map[string]any{"attempt": 1}) //nolint:errcheck

	// Second store: should be silently ignored (SetNX)
	store.Store(ctx, walletID, key, 201, map[string]any{"attempt": 2}) //nolint:errcheck

	// Should still have the first response
	cached, found, _ := store.Check(ctx, walletID, key)
	if !found {
		t.Fatal("key should exist")
	}

	var body map[string]any
	json.Unmarshal(cached.Body, &body) //nolint:errcheck
	if body["attempt"] != float64(1) {
		t.Errorf("second Store should not overwrite first — expected attempt 1, got %v", body["attempt"])
	}
}

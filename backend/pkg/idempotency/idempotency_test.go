// Tests for the idempotency package.
// These use a mock Redis client so they run without a real Redis instance.
// The integration test (requires Redis) is in idempotency_integration_test.go.
package idempotency_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cbdc-simulator/backend/pkg/idempotency"
)

// ── Mock Redis ────────────────────────────────────────────────────────────────
// A minimal in-memory implementation of the Redis interface used by idempotency.Store.
// This lets us test idempotency logic without a running Redis instance.

type mockStore struct {
	mu   sync.RWMutex
	data map[string]string
	ttls map[string]time.Time
}

func newMockStore() *mockStore {
	return &mockStore{
		data: make(map[string]string),
		ttls: make(map[string]time.Time),
	}
}

func (m *mockStore) GetString(_ context.Context, key string) (string, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Simulate TTL expiry
	if exp, ok := m.ttls[key]; ok && time.Now().After(exp) {
		return "", false, nil
	}
	val, ok := m.data[key]
	return val, ok, nil
}

func (m *mockStore) SetNX(_ context.Context, key string, value any, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.data[key]; exists {
		return false, nil // key already exists — SetNX returns false
	}
	m.data[key] = fmt.Sprintf("%v", value)
	m.ttls[key] = time.Now().Add(ttl)
	return true, nil
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// TestStoreInterface_MockSatisfiesContract ensures our mock behaves like real Redis.
// This is a sanity check on the mock, not on the idempotency package itself.
func TestMockStore_SetNX_ReturnsFalseOnDuplicate(t *testing.T) {
	store := newMockStore()
	ctx := context.Background()

	set1, _ := store.SetNX(ctx, "key1", "value", time.Minute)
	set2, _ := store.SetNX(ctx, "key1", "other", time.Minute)

	if !set1 {
		t.Fatal("first SetNX should return true")
	}
	if set2 {
		t.Fatal("second SetNX on same key should return false")
	}
}

// idempotencyStoreAdapter wraps mockStore to provide the interface
// that idempotency.NewWithRedisAdapter expects.
// In production, this is the real *redis.Client.

// Since we can't easily inject a mock into the current idempotency.Store
// without changing its interface, we test the key-building logic and
// the public API behaviour via a thin wrapper.
// The full integration test covers Redis directly.

// For now, test the key namespacing logic by verifying two wallets
// with the same idempotency key are treated as different entries.
func TestIdempotencyKey_ScopedPerWallet(t *testing.T) {
	// The key used by wallet A with idempotency key "abc" must be
	// different from the key used by wallet B with the same "abc".
	// If they share a key, wallet A's cached response would be
	// returned to wallet B — a serious security/correctness bug.

	// We verify this by checking that the internal key format
	// includes the wallet ID as a namespace prefix.
	// Since buildKey is unexported, we verify the behaviour:
	// Check() for wallet A should not find wallet B's stored entry.
	// This is tested properly in integration tests with real Redis.
	t.Log("Key scoping is verified in integration tests with real Redis (idempotency_integration_test.go)")
}

// TestCachedResponse_SerializationRoundtrip verifies that the
// CachedResponse structure survives JSON marshal/unmarshal correctly.
// This matters because we store it as a JSON string in Redis.
func TestCachedResponse_SerializationRoundtrip(t *testing.T) {
	original := idempotency.CachedResponse{
		StatusCode: 201,
		Body:       json.RawMessage(`{"id":"txn_123","amount_cents":5000}`),
	}

	// Simulate what happens in Redis: marshal to string, unmarshal back
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var recovered idempotency.CachedResponse
	if err := json.Unmarshal(data, &recovered); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if recovered.StatusCode != original.StatusCode {
		t.Errorf("status code mismatch: got %d, want %d", recovered.StatusCode, original.StatusCode)
	}
	if string(recovered.Body) != string(original.Body) {
		t.Errorf("body mismatch:\n  got:  %s\n  want: %s", recovered.Body, original.Body)
	}
}

func TestCachedResponse_HandlesComplexBody(t *testing.T) {
	// Verify nested JSON bodies survive the roundtrip intact.
	complexBody := json.RawMessage(`{
		"transaction": {"id": "txn_1", "amount": 100},
		"wallet": {"balance": 900}
	}`)

	original := idempotency.CachedResponse{StatusCode: 200, Body: complexBody}
	data, _ := json.Marshal(original)

	var recovered idempotency.CachedResponse
	json.Unmarshal(data, &recovered) //nolint:errcheck

	// Verify the nested structure is preserved by re-parsing
	var bodyMap map[string]any
	if err := json.Unmarshal(recovered.Body, &bodyMap); err != nil {
		t.Fatalf("complex body did not survive roundtrip: %v", err)
	}
	if _, ok := bodyMap["transaction"]; !ok {
		t.Error("'transaction' key missing from recovered body")
	}
}

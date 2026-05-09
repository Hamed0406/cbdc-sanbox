// Package idempotency prevents duplicate payment processing when clients
// retry requests due to network timeouts or failures.
//
// Flow:
// 1. Client sends X-Idempotency-Key header with every payment request
// 2. Before processing, we check Redis for that key
// 3. If found: return the original response immediately (no double processing)
// 4. If not found: process the payment, then cache the result in Redis
//
// Redis is the primary store (fast lookups). The DB table is a backup
// for cold Redis restarts. TTL is 24 hours — matches typical "retry window".
package idempotency

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	rdb "github.com/cbdc-simulator/backend/pkg/redis"
)

const (
	// KeyTTL is how long an idempotency key is remembered.
	// 24 hours covers any reasonable client retry scenario.
	KeyTTL = 24 * time.Hour

	// keyPrefix namespaces idempotency keys in Redis to avoid collisions
	// with other Redis data (rate limit counters, sessions, etc.).
	keyPrefix = "idempotency:"
)

// CachedResponse is what we store in Redis when a request completes.
type CachedResponse struct {
	StatusCode int             `json:"status_code"`
	Body       json.RawMessage `json:"body"`
}

// UnmarshalBody deserializes the cached response body into the target value.
func (c *CachedResponse) UnmarshalBody(v any) error {
	return json.Unmarshal(c.Body, v)
}

// Store manages idempotency key lookups and storage.
type Store struct {
	redis *rdb.Client
}

// New creates a new idempotency Store.
func New(redis *rdb.Client) *Store {
	return &Store{redis: redis}
}

// Check looks up an idempotency key for a specific wallet.
// Returns (response, true, nil) if the key exists (already processed).
// Returns (nil, false, nil) if the key is new (proceed with processing).
func (s *Store) Check(ctx context.Context, walletID, idempotencyKey string) (*CachedResponse, bool, error) {
	redisKey := s.buildKey(walletID, idempotencyKey)

	val, exists, err := s.redis.GetString(ctx, redisKey)
	if err != nil {
		return nil, false, fmt.Errorf("idempotency check: %w", err)
	}
	if !exists {
		return nil, false, nil
	}

	var cached CachedResponse
	if err := json.Unmarshal([]byte(val), &cached); err != nil {
		// Corrupted cache entry — treat as not found so request is reprocessed.
		// This is safer than returning a corrupt response.
		return nil, false, nil
	}

	return &cached, true, nil
}

// Store saves the response for an idempotency key.
// Should be called AFTER a payment is successfully processed.
func (s *Store) Store(ctx context.Context, walletID, idempotencyKey string, statusCode int, body any) error {
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal idempotency body: %w", err)
	}

	cached := CachedResponse{
		StatusCode: statusCode,
		Body:       bodyJSON,
	}

	value, err := json.Marshal(cached)
	if err != nil {
		return fmt.Errorf("marshal cached response: %w", err)
	}

	redisKey := s.buildKey(walletID, idempotencyKey)
	// SetNX: only set if not exists — prevents a race where two concurrent
	// identical requests both pass the Check() and both try to Store().
	// The second one will fail silently (key already set by the first).
	_, err = s.redis.SetNX(ctx, redisKey, string(value), KeyTTL)
	return err
}

// buildKey constructs the Redis key.
// Scoped per-wallet so different wallets can use the same key value independently.
func (s *Store) buildKey(walletID, idempotencyKey string) string {
	return fmt.Sprintf("%s%s:%s", keyPrefix, walletID, idempotencyKey)
}

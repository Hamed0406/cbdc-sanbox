// Package redis wraps go-redis to provide app-specific helpers for:
// - Session/refresh token storage
// - Rate limiting counters (sliding window)
// - Idempotency key cache
// - WebSocket pub/sub for live payment notifications
package redis

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// Config holds Redis connection parameters.
type Config struct {
	Host     string
	Port     string
	Password string
	DB       int
}

// Client wraps redis.Client with app-specific helpers.
type Client struct {
	*redis.Client
}

// New creates a Redis client and verifies connectivity.
func New(ctx context.Context, cfg Config) (*Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", cfg.Host, cfg.Port),
		Password: cfg.Password,
		DB:       cfg.DB,

		// Pool settings — keep connections warm so payment requests don't
		// pay the TCP handshake cost on every idempotency check.
		PoolSize:        20,
		MinIdleConns:    5,
		ConnMaxIdleTime: 5 * time.Minute,
		DialTimeout:     5 * time.Second,
		ReadTimeout:     3 * time.Second,
		WriteTimeout:    3 * time.Second,
	})

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	slog.Info("redis connected", "addr", fmt.Sprintf("%s:%s", cfg.Host, cfg.Port))
	return &Client{rdb}, nil
}

// HealthCheck returns nil if Redis is responsive.
func (c *Client) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return c.Ping(ctx).Err()
}

// SetNX sets a key only if it does not already exist.
// Returns true if the key was set (i.e., it was new), false if it already existed.
// Used for distributed locking and idempotency checks.
func (c *Client) SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error) {
	return c.Client.SetNX(ctx, key, value, ttl).Result()
}

// GetString retrieves a string value; returns ("", false, nil) if key not found.
func (c *Client) GetString(ctx context.Context, key string) (string, bool, error) {
	val, err := c.Client.Get(ctx, key).Result()
	if err == redis.Nil {
		// Key does not exist — not an error, just absent.
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("redis get %q: %w", key, err)
	}
	return val, true, nil
}

// Delete removes one or more keys. Safe to call on non-existent keys.
func (c *Client) Delete(ctx context.Context, keys ...string) error {
	return c.Client.Del(ctx, keys...).Err()
}

// IncrWithExpiry increments a counter and sets TTL only on the first increment.
// This is the core of the sliding-window rate limiter:
// - On first request: key is created with TTL = window size
// - On subsequent requests within the window: key is incremented, TTL unchanged
// - After window expires: key is gone, counter resets automatically
func (c *Client) IncrWithExpiry(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	pipe := c.Client.Pipeline()
	incr := pipe.Incr(ctx, key)
	// EXPIRE only sets TTL if key has no TTL (i.e., NX flag).
	// This preserves the original window boundary for rate limiting.
	pipe.Expire(ctx, key, ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, fmt.Errorf("redis incr+expire %q: %w", key, err)
	}
	return incr.Val(), nil
}

// Publish sends a message to a Redis pub/sub channel.
// Used by the WebSocket hub to broadcast payment events across backend instances.
func (c *Client) Publish(ctx context.Context, channel string, message any) error {
	return c.Client.Publish(ctx, channel, message).Err()
}

// Subscribe returns a PubSub subscription for the given channels.
func (c *Client) Subscribe(ctx context.Context, channels ...string) *redis.PubSub {
	return c.Client.Subscribe(ctx, channels...)
}

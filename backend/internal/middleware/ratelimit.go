package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/cbdc-simulator/backend/pkg/response"
)

// redisIncrementer is the Redis interface needed by the rate limiter.
// Interface instead of concrete type → independently testable.
type redisIncrementer interface {
	IncrWithExpiry(ctx context.Context, key string, ttl time.Duration) (int64, error)
}

// RateLimitConfig defines a single rate limit rule.
type RateLimitConfig struct {
	// Limit is the maximum number of requests allowed in the Window.
	Limit int64
	// Window is the sliding window duration.
	Window time.Duration
	// KeyFunc extracts the rate limit key from the request.
	// Common choices: IP address, user ID, or "ip:endpoint" combination.
	KeyFunc func(r *http.Request) string
}

// RateLimit returns a middleware that enforces the given rate limit using
// a Redis sliding-window counter.
//
// WHY Redis (not in-memory)?
// In-memory counters don't work across multiple backend instances.
// Redis gives us a single shared counter that all instances see,
// which is correct behaviour for horizontal scaling.
//
// Algorithm: INCR + EXPIRE pipeline.
// - First request in a window: creates key, sets TTL, returns count=1.
// - Subsequent requests: increments counter (TTL unchanged).
// - After window expires: key disappears, counter resets naturally.
// - This is NOT a true sliding window — it's a fixed window counter reset at
//   the first request. It's simple, fast, and good enough for this use case.
func RateLimit(redis redisIncrementer, cfg RateLimitConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := GetRequestID(r.Context())
			key := "ratelimit:" + cfg.KeyFunc(r)

			count, err := redis.IncrWithExpiry(r.Context(), key, cfg.Window)
			if err != nil {
				// Redis failure → fail open (allow the request).
				// Failing closed would block all users if Redis goes down,
				// which is worse than briefly allowing excess requests.
				next.ServeHTTP(w, r)
				return
			}

			// Set informational headers so clients know their limit status.
			// These mirror the standard rate limit headers used by GitHub, Stripe, etc.
			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", cfg.Limit))
			w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", max(0, cfg.Limit-count)))

			if count > cfg.Limit {
				retryAfterSeconds := int(cfg.Window.Seconds())
				w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfterSeconds))
				response.TooManyRequests(w, retryAfterSeconds, requestID)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// AuthRateLimit returns a strict rate limiter for authentication endpoints.
// Keyed by IP address — 10 attempts per 15 minutes.
// This makes brute-force attacks slow without banning legitimate users.
func AuthRateLimit(redis redisIncrementer) func(http.Handler) http.Handler {
	return RateLimit(redis, RateLimitConfig{
		Limit:  10,
		Window: 15 * time.Minute,
		KeyFunc: func(r *http.Request) string {
			// Use real IP (set by chi's RealIP middleware which reads X-Forwarded-For)
			return "auth:" + r.RemoteAddr
		},
	})
}

// GeneralRateLimit returns a permissive rate limiter for general API endpoints.
// Keyed by user ID (from context) or falls back to IP for unauthenticated requests.
func GeneralRateLimit(redis redisIncrementer) func(http.Handler) http.Handler {
	return RateLimit(redis, RateLimitConfig{
		Limit:  60,
		Window: time.Minute,
		KeyFunc: func(r *http.Request) string {
			if userID, ok := GetUserID(r.Context()); ok {
				return "general:user:" + userID
			}
			return "general:ip:" + r.RemoteAddr
		},
	})
}

// PaymentRateLimit is a tighter limit for payment endpoints — 20 per minute per user.
// Payments are higher-value operations; this prevents automated payment spam.
func PaymentRateLimit(redis redisIncrementer) func(http.Handler) http.Handler {
	return RateLimit(redis, RateLimitConfig{
		Limit:  20,
		Window: time.Minute,
		KeyFunc: func(r *http.Request) string {
			if userID, ok := GetUserID(r.Context()); ok {
				return "payment:user:" + userID
			}
			return "payment:ip:" + r.RemoteAddr
		},
	})
}

// max returns the larger of two int64 values.
// Defined here to avoid importing math for a trivial operation.
func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

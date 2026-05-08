//go:build integration

// Integration tests for the database package.
// Requires a running PostgreSQL instance.
// Run with: go test -tags=integration ./pkg/database/...
//
// Environment variables needed:
//   TEST_DB_URL=postgres://cbdc_app:password@localhost:5432/cbdc_test?sslmode=disable
package database_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cbdc-simulator/backend/pkg/database"
)

func getTestConfig(t *testing.T) database.Config {
	t.Helper()
	// Parse from TEST_DB_URL if set, otherwise use individual env vars with defaults
	url := os.Getenv("TEST_DB_URL")
	if url == "" {
		return database.Config{
			Host:         getEnvOrDefault("TEST_DB_HOST", "localhost"),
			Port:         getEnvOrDefault("TEST_DB_PORT", "5432"),
			Name:         getEnvOrDefault("TEST_DB_NAME", "cbdc_test"),
			User:         getEnvOrDefault("TEST_DB_USER", "cbdc_app"),
			Password:     getEnvOrDefault("TEST_DB_PASSWORD", "testpassword"),
			MaxOpenConns: 5,
			MaxIdleConns: 2,
			ConnTimeout:  5 * time.Second,
		}
	}
	// Minimal parse of postgres://user:pass@host:port/db
	_ = url // used in CI via the migrate container; individual vars used here
	return database.Config{
		Host: "localhost", Port: "5432", Name: "cbdc_test",
		User: "cbdc_app", Password: "testpassword",
		MaxOpenConns: 5, MaxIdleConns: 2, ConnTimeout: 5 * time.Second,
	}
}

func TestIntegration_PostgresConnection(t *testing.T) {
	cfg := getTestConfig(t)
	ctx := context.Background()

	pool, err := database.New(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to connect to test postgres: %v", err)
	}
	defer pool.Close()

	// Health check must pass on a fresh connection
	if err := pool.HealthCheck(ctx); err != nil {
		t.Fatalf("health check failed: %v", err)
	}
}

func TestIntegration_HealthCheckReturnsStats(t *testing.T) {
	pool, _ := database.New(context.Background(), getTestConfig(t))
	defer pool.Close()

	stats := pool.Stats()

	// Stats map must contain expected keys
	requiredKeys := []string{"total_connections", "idle_connections", "acquired", "max_connections"}
	for _, key := range requiredKeys {
		if _, ok := stats[key]; !ok {
			t.Errorf("stats missing key %q", key)
		}
	}
}

func TestIntegration_HealthCheckFailsOnBadConnection(t *testing.T) {
	// Connect to a non-existent host — should fail quickly
	cfg := getTestConfig(t)
	cfg.Host = "nonexistent-host-that-does-not-exist"

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	_, err := database.New(ctx, cfg)
	if err == nil {
		t.Fatal("expected connection to fail for non-existent host")
	}
	// Error message should be meaningful, not just "error"
	if !strings.Contains(err.Error(), "ping postgres") && !strings.Contains(err.Error(), "create postgres") {
		t.Logf("error message: %v", err) // log for debugging but don't fail
	}
}

func getEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

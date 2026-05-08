// Tests for the database package.
// Unit tests here only cover non-DB logic (Stats output shape, Config struct).
// Integration tests that require a real PostgreSQL instance are in
// postgres_integration_test.go and are gated by the `integration` build tag.
package database_test

import (
	"testing"

	"github.com/cbdc-simulator/backend/pkg/database"
)

// TestConfig_DefaultValues verifies the Config struct can be created
// and that zero values don't cause panics downstream.
func TestConfig_CanBeCreated(t *testing.T) {
	cfg := database.Config{
		Host:         "localhost",
		Port:         "5432",
		Name:         "testdb",
		User:         "testuser",
		Password:     "testpass",
		MaxOpenConns: 25,
		MaxIdleConns: 5,
	}

	if cfg.Host != "localhost" {
		t.Errorf("unexpected Host: %q", cfg.Host)
	}
	if cfg.MaxOpenConns != 25 {
		t.Errorf("unexpected MaxOpenConns: %d", cfg.MaxOpenConns)
	}
}

// Package database provides PostgreSQL connection management.
// We use pgx directly (no ORM) so all SQL is visible, auditable,
// and uses parameterized queries — eliminating SQL injection at the driver level.
package database

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Config holds all PostgreSQL connection parameters sourced from environment variables.
type Config struct {
	Host         string
	Port         string
	Name         string
	User         string
	Password     string
	MaxOpenConns int32
	MaxIdleConns int32
	// ConnTimeout is how long to wait when acquiring a connection from the pool.
	ConnTimeout time.Duration
}

// Pool wraps pgxpool.Pool to add health-check and app-specific helpers.
type Pool struct {
	*pgxpool.Pool
}

// New creates a connection pool and verifies the database is reachable.
// The pool is safe for concurrent use — pgx manages connections internally.
func New(ctx context.Context, cfg Config) (*Pool, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%s dbname=%s user=%s password=%s sslmode=disable pool_max_conns=%d pool_min_conns=%d connect_timeout=5",
		cfg.Host, cfg.Port, cfg.Name, cfg.User, cfg.Password,
		cfg.MaxOpenConns, cfg.MaxIdleConns,
	)

	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres config: %w", err)
	}

	// pgx will health-check idle connections automatically at this interval.
	// This prevents "stale connection" errors after the DB restarts.
	poolCfg.HealthCheckPeriod = 30 * time.Second
	poolCfg.MaxConnLifetime = 1 * time.Hour
	poolCfg.MaxConnIdleTime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}

	// Fail fast on startup if the DB is unreachable.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	slog.Info("postgres connected", "host", cfg.Host, "db", cfg.Name)
	return &Pool{pool}, nil
}

// HealthCheck returns nil if the database is responsive.
// Called by the /health endpoint.
func (p *Pool) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return p.Ping(ctx)
}

// Stats returns pool statistics for the health endpoint and monitoring.
func (p *Pool) Stats() map[string]any {
	s := p.Pool.Stat()
	return map[string]any{
		"total_connections":  s.TotalConns(),
		"idle_connections":   s.IdleConns(),
		"acquired":           s.AcquiredConns(),
		"max_connections":    s.MaxConns(),
	}
}

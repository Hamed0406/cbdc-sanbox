// Package main is the entry point for the CBDC Payment Gateway Simulator backend.
// It wires together all dependencies (DB, Redis, services) and starts the HTTP server
// with graceful shutdown on SIGINT/SIGTERM.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/cbdc-simulator/backend/internal/admin"
	"github.com/cbdc-simulator/backend/internal/audit"
	"github.com/cbdc-simulator/backend/internal/auth"
	"github.com/cbdc-simulator/backend/internal/ledger"
	"github.com/cbdc-simulator/backend/internal/middleware"
	"github.com/cbdc-simulator/backend/internal/payment"
	"github.com/cbdc-simulator/backend/internal/wallet"
	"github.com/cbdc-simulator/backend/pkg/database"
	"github.com/cbdc-simulator/backend/pkg/idempotency"
	rdb "github.com/cbdc-simulator/backend/pkg/redis"
	"github.com/cbdc-simulator/backend/pkg/response"
)

// Version is injected at build time via ldflags: -X main.Version=1.0.0
var Version = "dev"

func main() {
	// Structured JSON logging — every log line is machine-parseable.
	// In development, use text format for readability.
	logLevel := slog.LevelInfo
	if getEnv("LOG_LEVEL", "info") == "debug" {
		logLevel = slog.LevelDebug
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	slog.Info("starting CBDC simulator", "version", Version, "env", getEnv("APP_ENV", "development"))

	// ── Database connection ──────────────────────────────────────────────────
	maxOpen, _ := strconv.Atoi(getEnv("DB_MAX_OPEN_CONNS", "25"))
	maxIdle, _ := strconv.Atoi(getEnv("DB_MAX_IDLE_CONNS", "5"))

	dbPool, err := database.New(context.Background(), database.Config{
		Host:         getEnv("DB_HOST", "localhost"),
		Port:         getEnv("DB_PORT", "5432"),
		Name:         getEnv("DB_NAME", "cbdc_db"),
		User:         getEnv("DB_USER", "cbdc_app"),
		Password:     mustEnv("DB_PASSWORD"),
		MaxOpenConns: int32(maxOpen),
		MaxIdleConns: int32(maxIdle),
		ConnTimeout:  5 * time.Second,
	})
	if err != nil {
		slog.Error("failed to connect to postgres", "error", err)
		os.Exit(1)
	}
	defer dbPool.Close()

	// ── Redis connection ─────────────────────────────────────────────────────
	redisClient, err := rdb.New(context.Background(), rdb.Config{
		Host:     getEnv("REDIS_HOST", "localhost"),
		Port:     getEnv("REDIS_PORT", "6379"),
		Password: getEnv("REDIS_PASSWORD", ""),
		DB:       0,
	})
	if err != nil {
		slog.Error("failed to connect to redis", "error", err)
		os.Exit(1)
	}
	defer redisClient.Close()

	// ── HTTP Router ──────────────────────────────────────────────────────────
	r := chi.NewRouter()

	// Request ID: generated for every request and echoed in error responses.
	// Allows correlating client errors with server logs without exposing internals.
	r.Use(chimiddleware.RequestID)

	// Real IP: extract client IP from X-Forwarded-For when behind a proxy (Nginx).
	r.Use(chimiddleware.RealIP)

	// Structured request logger: logs method, path, status, latency.
	r.Use(chimiddleware.Logger)

	// Panic recovery: converts panics to 500 errors instead of crashing the server.
	// Any panic in a handler is a bug — log it and keep serving other requests.
	r.Use(chimiddleware.Recoverer)

	// CORS: only allow the configured frontend origin.
	// Never use wildcard (*) — that would allow any website to make
	// authenticated requests using the user's cookies.
	allowedOrigin := getEnv("ALLOWED_ORIGIN", "http://localhost:3000")
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{allowedOrigin},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "X-Request-ID", "X-Idempotency-Key"},
		AllowCredentials: true, // required for HttpOnly cookie refresh tokens
		MaxAge:           300,
	}))

	// Security headers on every response
	r.Use(securityHeadersMiddleware)

	// ── Routes ──────────────────────────────────────────────────────────────

	// Health check — no auth, used by Docker, load balancers, and monitoring
	r.Get("/health", healthHandler(dbPool, redisClient))

	// ── Auth service wiring ──────────────────────────────────────────────────
	jwtSecret := mustEnv("JWT_SECRET")
	signingKey := mustEnv("SIGNING_KEY")

	accessTTL := parseDuration(getEnv("JWT_ACCESS_TTL_SECONDS", "900"), 900)
	refreshTTL := parseDuration(getEnv("JWT_REFRESH_TTL_SECONDS", "604800"), 604800)

	auditSvc := audit.NewService(dbPool)
	authRepo := auth.NewRepository(dbPool)
	authSvc := auth.NewService(authRepo, auditSvc, auth.Config{
		JWTSecret:       jwtSecret,
		AccessTokenTTL:  accessTTL,
		RefreshTokenTTL: refreshTTL,
	})

	// secureCookies: true in production (HTTPS enforced), false in dev (plain HTTP)
	secureCookies := getEnv("APP_ENV", "development") == "production"
	authHandler := auth.NewHandler(authSvc, refreshTTL, secureCookies)

	// API v1
	r.Route("/api/v1", func(r chi.Router) {
		// System info — public, no auth
		r.Get("/system/info", systemInfoHandler())

		// Auth routes — rate limited, no JWT required
		r.Group(func(r chi.Router) {
			r.Use(middleware.AuthRateLimit(redisClient))
			r.Mount("/auth", authHandler.Routes())
		})

		// Protected routes — JWT required
		r.Group(func(r chi.Router) {
			r.Use(middleware.Authenticate(authSvc))
			r.Use(middleware.GeneralRateLimit(redisClient))

			// Phase 3: Wallet reads
			ledgerRepo := ledger.NewRepository(dbPool)
			ledgerSvc := ledger.NewService(ledgerRepo)

			walletRepo := wallet.NewRepository(dbPool)
			walletSvc := wallet.NewService(walletRepo)
			walletHandler := wallet.NewHandler(walletSvc)
			r.Mount("/wallets", walletHandler.Routes())

			// Phase 4: CBDC issuance — admin only
			idempotentStore := idempotency.New(redisClient)
			adminSvc := admin.NewService(ledgerSvc, idempotentStore, auditSvc, signingKey)
			adminHandler := admin.NewHandler(adminSvc)
			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireAdmin())
				r.Mount("/admin", adminHandler.Routes())
			})

			// Phase 5: P2P Payments
			paymentRepo := payment.NewRepository(dbPool)
			paymentSvc := payment.NewService(paymentRepo, ledgerSvc, idempotentStore, auditSvc, signingKey)
			paymentHandler := payment.NewHandler(paymentSvc)
			r.Mount("/payments", paymentHandler.Routes())

			// r.Mount("/merchant", merchantHandler.Routes()) // Phase 7
		})
	})

	// ── Server startup ───────────────────────────────────────────────────────
	port := getEnv("APP_PORT", "8080")
	srv := &http.Server{
		Addr:    ":" + port,
		Handler: r,
		// Timeouts prevent slow clients from holding connections indefinitely.
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second, // longer for payments which hit DB + Redis
		IdleTimeout:       60 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Start server in a goroutine so we can handle shutdown signals below.
	serverErr := make(chan error, 1)
	go func() {
		slog.Info("server listening", "port", port, "url", fmt.Sprintf("http://localhost:%s", port))
		serverErr <- srv.ListenAndServe()
	}()

	// ── Graceful shutdown ────────────────────────────────────────────────────
	// Wait for SIGINT (Ctrl+C) or SIGTERM (Docker stop).
	// On signal: stop accepting new requests, wait up to 30s for in-flight
	// requests to complete (e.g., a payment that's mid-transaction),
	// then exit cleanly. This prevents cutting off active database transactions.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		slog.Error("server error", "error", err)
		os.Exit(1)
	case sig := <-quit:
		slog.Info("shutdown signal received", "signal", sig)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	slog.Info("server stopped cleanly")
}

// healthHandler returns a handler that checks DB and Redis connectivity.
// Docker's healthcheck pings this endpoint to determine if the container is ready.
func healthHandler(db *database.Pool, redis *rdb.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dbErr := db.HealthCheck(r.Context())
		redisErr := redis.HealthCheck(r.Context())

		dbStatus := "ok"
		if dbErr != nil {
			dbStatus = "error: " + dbErr.Error()
		}

		redisStatus := "ok"
		if redisErr != nil {
			redisStatus = "error: " + redisErr.Error()
		}

		status := http.StatusOK
		overallStatus := "ok"
		if dbErr != nil || redisErr != nil {
			// 503 tells the load balancer to stop routing traffic here
			status = http.StatusServiceUnavailable
			overallStatus = "degraded"
		}

		response.JSON(w, status, map[string]any{
			"status":  overallStatus,
			"version": Version,
			"services": map[string]string{
				"database": dbStatus,
				"redis":    redisStatus,
			},
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	}
}

// systemInfoHandler returns public system metadata (no auth required).
func systemInfoHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response.OK(w, map[string]any{
			"currency":      "DD$",
			"currency_name": "DigitalDollar",
			"environment":   getEnv("APP_ENV", "development"),
			"version":       Version,
			"disclaimer":    "This is a sandbox simulator. No real money is involved.",
		})
	}
}

// securityHeadersMiddleware adds security-related HTTP headers to every response.
// These headers instruct the browser to enforce additional protections
// against XSS, clickjacking, and content sniffing attacks.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prevent browsers from guessing content type (MIME sniffing attacks)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// Deny rendering in iframes (clickjacking prevention)
		w.Header().Set("X-Frame-Options", "DENY")
		// Don't send Referer header to third parties
		w.Header().Set("Referrer-Policy", "no-referrer")
		// Allow camera for QR scanning, deny everything else
		w.Header().Set("Permissions-Policy", "camera=self, microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

// parseDuration converts an environment variable (seconds as string) to time.Duration.
func parseDuration(s string, defaultSecs int) time.Duration {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return time.Duration(defaultSecs) * time.Second
	}
	return time.Duration(n) * time.Second
}

// getEnv reads an environment variable, falling back to a default value.
func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// mustEnv reads an environment variable and exits if it's not set.
// Used for secrets that have no safe default value.
func mustEnv(key string) string {
	val := os.Getenv(key)
	if val == "" {
		slog.Error("required environment variable not set", "key", key)
		os.Exit(1)
	}
	return val
}

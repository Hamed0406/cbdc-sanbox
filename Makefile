# CBDC Payment Gateway Simulator — Makefile
# Usage: make <target>
# Run `make help` to see all available targets.

.PHONY: help setup up down dev logs build test lint fmt vet swagger migrate seed reset-db

# Detect docker compose vs docker-compose (V1 vs V2)
DOCKER_COMPOSE := $(shell docker compose version >/dev/null 2>&1 && echo "docker compose" || echo "docker-compose")

# ── First-time setup ─────────────────────────────────────────────────────────

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

setup: ## First-time setup: generate secrets, build, migrate, start
	@bash scripts/init.sh

generate-secrets: ## Regenerate .env secrets (WARNING: invalidates all active sessions)
	@rm -f backend/.env
	@bash scripts/generate-secrets.sh

# ── Docker Compose ────────────────────────────────────────────────────────────

up: ## Start all services (production mode)
	$(DOCKER_COMPOSE) up -d

down: ## Stop all services
	$(DOCKER_COMPOSE) down

restart: down up ## Restart all services

dev: ## Start in development mode (hot reload)
	$(DOCKER_COMPOSE) -f docker-compose.yml -f docker-compose.dev.yml up

build: ## Build all Docker images
	$(DOCKER_COMPOSE) build

build-backend: ## Build only the backend image
	$(DOCKER_COMPOSE) build backend

build-frontend: ## Build only the frontend image
	$(DOCKER_COMPOSE) build frontend

# ── Logging ──────────────────────────────────────────────────────────────────

logs: ## Tail logs from all services
	$(DOCKER_COMPOSE) logs -f

logs-backend: ## Tail backend logs only
	$(DOCKER_COMPOSE) logs -f backend

logs-db: ## Tail postgres logs
	$(DOCKER_COMPOSE) logs -f postgres

# ── Shell access ─────────────────────────────────────────────────────────────

shell-backend: ## Open a shell in the backend container
	$(DOCKER_COMPOSE) exec backend sh

shell-db: ## Open psql shell in the postgres container
	$(DOCKER_COMPOSE) exec postgres psql -U cbdc_app -d cbdc_db

shell-redis: ## Open redis-cli in the redis container
	$(DOCKER_COMPOSE) exec redis redis-cli -a $$(grep REDIS_PASSWORD backend/.env | cut -d= -f2)

# ── Database ──────────────────────────────────────────────────────────────────

migrate: ## Run pending database migrations
	$(DOCKER_COMPOSE) run --rm migrate up

migrate-down: ## Roll back the last migration
	$(DOCKER_COMPOSE) run --rm migrate down 1

migrate-version: ## Show current migration version
	$(DOCKER_COMPOSE) run --rm migrate version

seed: ## Load demo seed data
	@bash scripts/seed.sh

reset-db: ## DROP and recreate database, run migrations, seed (DESTRUCTIVE)
	@echo "WARNING: This will destroy all data. Press Ctrl+C to cancel..."
	@sleep 3
	$(DOCKER_COMPOSE) exec postgres psql -U cbdc_app -c "DROP DATABASE IF EXISTS cbdc_db;"
	$(DOCKER_COMPOSE) exec postgres psql -U cbdc_app -c "CREATE DATABASE cbdc_db;"
	$(MAKE) migrate
	$(MAKE) seed

# ── Go backend development ────────────────────────────────────────────────────

test: ## Run all backend tests
	cd backend && go test -v -race -count=1 ./...

test-unit: ## Run unit tests only (no DB required)
	cd backend && go test -v -race -count=1 -run 'Unit' ./...

test-int: ## Run integration tests (requires running DB)
	cd backend && go test -v -race -count=1 -tags=integration ./...

lint: ## Run golangci-lint
	cd backend && golangci-lint run ./...

fmt: ## Format Go code
	cd backend && gofmt -w .

vet: ## Run go vet
	cd backend && go vet ./...

vuln-check: ## Check for known Go vulnerabilities
	cd backend && govulncheck ./...

sec-scan: ## Run gosec security scanner
	cd backend && gosec -quiet ./...

swagger: ## Generate Swagger API docs
	cd backend && swag init -g cmd/server/main.go -o docs

# ── Frontend development ──────────────────────────────────────────────────────

frontend-install: ## Install frontend npm dependencies
	cd frontend && npm ci

frontend-lint: ## Lint frontend TypeScript
	cd frontend && npm run lint

frontend-typecheck: ## TypeScript type check
	cd frontend && npm run type-check

frontend-build: ## Build frontend for production
	cd frontend && npm run build

frontend-audit: ## Audit frontend dependencies for vulnerabilities
	cd frontend && npm audit --audit-level=high

# ── Health checks ────────────────────────────────────────────────────────────

health: ## Check health of all services
	@echo "Backend:" && curl -sf http://localhost:8080/health | python3 -m json.tool || echo "UNREACHABLE"
	@echo ""
	@echo "Frontend:" && curl -sf http://localhost:3000 >/dev/null && echo "OK" || echo "UNREACHABLE"

# ── Cleanup ───────────────────────────────────────────────────────────────────

clean: ## Remove containers, volumes, and built images (DESTRUCTIVE)
	$(DOCKER_COMPOSE) down -v --rmi local
	rm -f backend/.env frontend/.env

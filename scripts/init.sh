#!/usr/bin/env bash
# First-time setup script for the CBDC Simulator.
# Generates secrets, builds images, runs migrations, starts all services.
# Usage: bash scripts/init.sh

set -euo pipefail

# Colours for output
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
info()  { echo -e "${GREEN}▶${NC} $1"; }
warn()  { echo -e "${YELLOW}⚠${NC}  $1"; }
error() { echo -e "${RED}✗${NC} $1"; exit 1; }

echo ""
echo "╔══════════════════════════════════════════════════╗"
echo "║     CBDC Payment Gateway Simulator — Setup       ║"
echo "╚══════════════════════════════════════════════════╝"
echo ""

# ── Prerequisites check ──────────────────────────────────────────────────────
command -v docker   >/dev/null 2>&1 || error "Docker not found. Install: https://docs.docker.com/get-docker/"
command -v docker compose >/dev/null 2>&1 || \
command -v docker-compose >/dev/null 2>&1 || error "Docker Compose not found."
command -v openssl  >/dev/null 2>&1 || error "openssl not found (needed for secret generation)."

info "Prerequisites OK"

# ── Generate secrets ─────────────────────────────────────────────────────────
bash scripts/generate-secrets.sh

# ── Source DB_PASSWORD for health checks ─────────────────────────────────────
# shellcheck source=/dev/null
source backend/.env

# ── Start infrastructure only ────────────────────────────────────────────────
info "Starting PostgreSQL and Redis..."
docker compose up -d postgres redis

# Wait for PostgreSQL to be ready (up to 60s)
info "Waiting for PostgreSQL to be ready..."
RETRIES=30
until docker compose exec -T postgres pg_isready -U cbdc_app -d cbdc_db >/dev/null 2>&1; do
    RETRIES=$((RETRIES - 1))
    if [ $RETRIES -eq 0 ]; then
        error "PostgreSQL did not become ready in time. Check: docker compose logs postgres"
    fi
    sleep 2
done
info "PostgreSQL is ready"

# ── Run migrations ────────────────────────────────────────────────────────────
info "Running database migrations..."
docker compose run --rm migrate
info "Migrations complete"

# ── Build and start all services ──────────────────────────────────────────────
info "Building and starting all services..."
docker compose up -d --build

# Wait for backend health check
info "Waiting for backend to be healthy..."
RETRIES=30
until curl -sf http://localhost:8080/health >/dev/null 2>&1; do
    RETRIES=$((RETRIES - 1))
    if [ $RETRIES -eq 0 ]; then
        warn "Backend health check timed out. Check logs: docker compose logs backend"
        break
    fi
    sleep 2
done

echo ""
echo -e "${GREEN}╔══════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║         CBDC Simulator is running!                   ║${NC}"
echo -e "${GREEN}╠══════════════════════════════════════════════════════╣${NC}"
echo -e "${GREEN}║  Frontend:   http://localhost:3000                   ║${NC}"
echo -e "${GREEN}║  API:        http://localhost:8080                   ║${NC}"
echo -e "${GREEN}║  API Docs:   http://localhost:8080/swagger/index.html║${NC}"
echo -e "${GREEN}╠══════════════════════════════════════════════════════╣${NC}"
echo -e "${GREEN}║  Demo credentials:                                   ║${NC}"
echo -e "${GREEN}║  Admin:    admin@cbdc.local  / Admin1234!            ║${NC}"
echo -e "${GREEN}║  User:     alice@example.com / Alice1234!            ║${NC}"
echo -e "${GREEN}║  Merchant: cafe@example.com  / Cafe1234!             ║${NC}"
echo -e "${GREEN}╚══════════════════════════════════════════════════════╝${NC}"
echo ""

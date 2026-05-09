#!/usr/bin/env bash
# Loads demo seed data into the database.
# Idempotent: safe to run multiple times — upserts, never duplicates.
#
# Usage (from project root):
#   make seed                    run via docker exec (stack must be running)
#   go run ./cmd/seed            run directly from backend/ with env vars set

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# Source backend env so the seed program can connect to Postgres.
# Works when called from the host with the Docker stack running.
if [ -f "$REPO_ROOT/backend/.env" ]; then
  # Override DB_HOST to localhost since we're connecting from the host machine,
  # not from inside a container where the hostname is "postgres".
  export $(grep -v '^#' "$REPO_ROOT/backend/.env" | xargs)
  export DB_HOST=localhost
fi

echo "Seeding database at ${DB_HOST:-localhost}:${DB_PORT:-5432}/${DB_NAME:-cbdc_db} ..."
cd "$REPO_ROOT/backend"
go run ./cmd/seed

echo ""
echo "Demo accounts ready:"
echo "  admin@cbdc.local    Admin1234!    (admin)"
echo "  alice@example.com   Alice1234!    (user,   DD\$ 4,929.00)"
echo "  bob@example.com     Bob12345!     (user,   DD\$ 1,631.50)"
echo "  carol@example.com   Carol123!     (user,   DD\$   410.00)"
echo "  cafe@example.com    Cafe1234!     (merchant, DD\$ 29.50)"
echo "  market@example.com  Market123!    (merchant, DD\$ 0.00)"

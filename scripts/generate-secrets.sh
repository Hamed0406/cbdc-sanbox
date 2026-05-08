#!/usr/bin/env bash
# Generates cryptographically random secrets and writes them to backend/.env
# Run this once on first setup. It will NOT overwrite an existing .env.
# Usage: bash scripts/generate-secrets.sh

set -euo pipefail

BACKEND_ENV="backend/.env"

if [ -f "$BACKEND_ENV" ]; then
    echo "⚠️  $BACKEND_ENV already exists. Delete it first if you want to regenerate secrets."
    exit 0
fi

echo "Generating secrets..."

JWT_SECRET=$(openssl rand -hex 32)
SIGNING_KEY=$(openssl rand -hex 32)
DB_PASSWORD=$(openssl rand -hex 16)
REDIS_PASSWORD=$(openssl rand -hex 16)

# Substitute placeholders in the example file
sed \
    -e "s|REPLACE_WITH_RANDOM_32_BYTE_HEX|${JWT_SECRET}|1" \
    -e "s|REPLACE_WITH_RANDOM_32_BYTE_HEX|${SIGNING_KEY}|1" \
    -e "s|REPLACE_WITH_STRONG_PASSWORD|${DB_PASSWORD}|" \
    -e "s|REPLACE_WITH_REDIS_PASSWORD|${REDIS_PASSWORD}|" \
    "$BACKEND_ENV.example" > "$BACKEND_ENV"

# Copy frontend env
if [ ! -f "frontend/.env" ]; then
    cp frontend/.env.example frontend/.env
fi

# Write root .env for Docker Compose variable substitution.
# Docker Compose automatically reads .env from the project root to resolve
# ${VAR} references inside docker-compose.yml (e.g. postgres POSTGRES_PASSWORD).
cat > ".env" <<EOF
DB_PASSWORD=${DB_PASSWORD}
REDIS_PASSWORD=${REDIS_PASSWORD}
APP_VERSION=1.0.0
EOF

echo "✓ Secrets written to backend/.env"
echo "✓ Docker Compose vars written to .env"
echo "✓ Frontend config written to frontend/.env"
echo ""
echo "Keep backend/.env and .env private — they contain your signing keys."

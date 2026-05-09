# CBDC Payment Gateway Simulator

A sandbox simulation of a retail Central Bank Digital Currency (CBDC) payment infrastructure, inspired by **e-Krona** (Sveriges Riksbank), **BIS Project Rosalind**, and **Project mBridge**. Runs entirely locally — no real bank connections, no real money.

> **Currency:** DigitalDollar (DD$) — test-only. All amounts are stored as integers (cents).

---

## Table of Contents

- [What This Is](#what-this-is)
- [Architecture Overview](#architecture-overview)
- [Tech Stack](#tech-stack)
- [Project Structure](#project-structure)
- [Database Schema](#database-schema)
- [API Reference](#api-reference)
- [Security Model](#security-model)
- [Getting Started](#getting-started)
- [Demo Accounts](#demo-accounts)
- [Development](#development)
- [Implementation Status](#implementation-status)

---

## What This Is

A full-stack CBDC simulator that demonstrates the core mechanics of a modern digital currency system:

- **Double-entry accounting** — every debit has a matching credit; money is never created or destroyed by accident
- **Central bank issuance** — an admin role mints DD$ into wallets (models central bank operations)
- **P2P transfers** — wallet-to-wallet payments with pessimistic locking and idempotency
- **Merchant QR payments** — `cbdc://pay?...` URI scheme with signed, expiring QR codes
- **Complete audit trail** — append-only log of every action, who did it, and from where
- **Role-based access** — user / merchant / admin with enforced permissions at every layer

---

## Architecture Overview

```
┌──────────────────────────────────────────────────────────────┐
│                  Browser / Mobile PWA                         │
│      React 18 + TypeScript + Tailwind + shadcn/ui            │
│   Dashboard | Send | QR Pay | Merchant POS | Admin | History │
└───────────────────────────┬──────────────────────────────────┘
                            │ HTTPS / WebSocket
┌───────────────────────────▼──────────────────────────────────┐
│                 Go HTTP Server (Chi Router)                    │
│         /api/v1/*   JWT · Rate Limiting · HMAC Signing        │
│                                                               │
│  ┌─────────┐ ┌─────────┐ ┌──────────┐ ┌──────────┐         │
│  │  Auth   │ │ Payment │ │ Merchant │ │  Admin   │         │
│  │ Handler │ │ Handler │ │ Handler  │ │ Handler  │         │
│  └────┬────┘ └────┬────┘ └────┬─────┘ └────┬─────┘         │
│       └───────────┴───────────┴─────────────┘               │
│                         │                                     │
│              ┌──────────▼──────────┐                         │
│              │    Ledger Engine    │ ← single source of truth │
│              │  Double-entry core  │                         │
│              └──────────┬──────────┘                         │
│                         │                                     │
│        ┌────────────────┼────────────────┐                   │
│        │                │                │                   │
│  ┌─────▼──────┐  ┌──────▼──────┐ ┌──────▼──────┐          │
│  │   Wallet   │  │    Audit    │ │  WebSocket  │          │
│  │  Service   │  │   Service   │ │     Hub     │          │
│  └────────────┘  └─────────────┘ └─────────────┘          │
└──────────┬────────────────────────────────────┬─────────────┘
           │                                    │
┌──────────▼────────────────┐     ┌─────────────▼────────────┐
│      PostgreSQL 16         │     │         Redis 7           │
│  Double-entry ledger       │     │  Idempotency keys (24h)   │
│  Wallets, Users            │     │  Rate limit counters      │
│  Audit logs (append-only)  │     │  Refresh tokens           │
│  Merchants, Sessions       │     │  WebSocket pub/sub        │
└────────────────────────────┘     └──────────────────────────┘
```

### Design Principles

| Principle | Implementation |
|-----------|---------------|
| **Security first** | Attack surface designed before happy path; HMAC-signed transactions, bcrypt cost 12, HttpOnly cookies |
| **Wallet = ledger view** | Wallet balance is a cached value; true balance is always sum of `ledger_entries` |
| **Pessimistic locking** | `SELECT ... FOR UPDATE` on wallet rows prevents double-spend race conditions |
| **Idempotency** | Redis-backed idempotency store; duplicate payment requests return the same result without double-processing |
| **Append-only audit** | `audit_logs` table has no UPDATE/DELETE; app DB role cannot delete audit records |
| **Modular monolith** | One binary, clear internal package boundaries — can be split into microservices later without rewriting business logic |

### Transaction State Machine

```
        ┌─────────┐
        │ PENDING │  ← transaction record created, DB locks acquired
        └────┬────┘
             │ balance check passes, ledger entries written
        ┌────▼──────┐
        │ CONFIRMED │  ← balances updated atomically
        └────┬──────┘
             │ settlement (immediate in sandbox)
        ┌────▼──────┐
        │  SETTLED  │  ← final state; signatures verified, audit logged
        └───────────┘

   From PENDING:            From SETTLED:
   ┌────────┐               ┌──────────┐
   │ FAILED │               │ REFUNDED │  ← creates a new reverse transaction
   └────────┘               └──────────┘
```

### P2P Payment Data Flow

```
Client              API Handler           Ledger Engine         PostgreSQL
  │                      │                      │                    │
  │  POST /payments/send  │                      │                    │
  │  X-Idempotency-Key    │                      │                    │
  ├─────────────────────►│                      │                    │
  │                      │ 1. Validate JWT       │                    │
  │                      │ 2. Redis idempotency  │                    │
  │                      │ 3. Validate body      │                    │
  │                      │ 4. HMAC sign payload  │                    │
  │                      │                      │                    │
  │                      │   BeginTx             │                    │
  │                      ├─────────────────────►│                    │
  │                      │                      │ SELECT wallet      │
  │                      │                      │ FOR UPDATE (both)  │
  │                      │                      ├──────────────────► │
  │                      │                      │ Check balance ≥ amt│
  │                      │                      │ INSERT transaction │
  │                      │                      │ INSERT ledger_entry│
  │                      │                      │  DEBIT  (sender)   │
  │                      │                      │ INSERT ledger_entry│
  │                      │                      │  CREDIT (receiver) │
  │                      │                      │ UPDATE status →    │
  │                      │                      │  SETTLED           │
  │                      │                      │ COMMIT             │
  │                      │                      ├──────────────────► │
  │                      │ 5. Audit log          │                    │
  │                      │ 6. Cache in Redis     │                    │
  │                      │ 7. WebSocket event    │                    │
  │◄─────────────────────┤                      │                    │
  │  201 {transaction}    │                      │                    │
```

---

## Tech Stack

| Layer | Technology | Version |
|-------|-----------|---------|
| Backend | Go + Chi router | 1.22+ |
| Frontend | React + TypeScript + Vite | 18 |
| Styling | Tailwind CSS + shadcn/ui | — |
| State management | Zustand + TanStack Query | — |
| Database | PostgreSQL | 16 |
| Cache / sessions | Redis | 7 |
| Auth | JWT (HS256 access) + Redis (refresh) | — |
| Mobile | PWA (service worker + manifest) | — |
| Containers | Docker Compose | — |
| Transaction signing | HMAC-SHA256 | — |
| Password hashing | bcrypt cost 12 | — |
| Migrations | golang-migrate | v4.17 |

---

## Project Structure

```
cbdc/
├── backend/
│   ├── cmd/server/main.go          # Entry point, dependency wiring, graceful shutdown
│   ├── internal/
│   │   ├── auth/                   # Registration, login, JWT, token rotation
│   │   ├── ledger/                 # Double-entry engine — the financial core
│   │   ├── wallet/                 # Wallet reads (balance, history) — read-only vs ledger
│   │   ├── payment/                # P2P send, idempotency, transaction history
│   │   ├── merchant/               # Payment requests, QR generation, webhooks
│   │   ├── admin/                  # CBDC issuance, freeze/unfreeze, monitoring
│   │   ├── audit/                  # Append-only audit log (called by all packages)
│   │   ├── middleware/             # JWT validation, rate limiting, RBAC
│   │   └── websocket/              # Live payment notification hub
│   ├── pkg/
│   │   ├── crypto/                 # HMAC signing, bcrypt helpers
│   │   ├── currency/               # DD$ amount formatter (cents → "DD$ 10.50")
│   │   ├── database/               # PostgreSQL connection pool
│   │   ├── idempotency/            # Redis-backed idempotency store
│   │   ├── redis/                  # Redis client wrapper
│   │   ├── response/               # Standard JSON response helpers
│   │   └── token/                  # Shared JWT Claims struct
│   └── migrations/                 # 12 sequential SQL migrations
├── frontend/                       # React PWA (Phase 6+)
├── docs/                           # Detailed architecture and API docs
├── scripts/                        # init.sh, seed.sh, generate-secrets.sh
├── docker-compose.yml
├── docker-compose.dev.yml          # Adds hot-reload (air) and volume mounts
└── Makefile
```

---

## Database Schema

12 migrations build the full schema. Key tables:

```
users           — accounts with role (user/merchant/admin), bcrypt password, soft deletes
wallets         — one wallet per user; balance in cents; is_frozen flag
transactions    — every value movement; HMAC signature; idempotency_key per sender
ledger_entries  — double-entry rows (DEBIT/CREDIT); balance_after snapshot per entry
merchants       — merchant profiles with API keys and webhook URLs
payment_requests — QR payment targets with expiry and HMAC signature
sessions        — refresh token store (backed by Redis too)
audit_logs      — append-only; actor, action, resource, IP, metadata
cbdc_issuance   — record of every admin mint operation
system_config   — key/value config (supply cap, feature flags)
```

**Key constraints enforced at DB level:**
- `wallets.balance >= 0` — balance can never go negative
- `transactions.amount_cents > 0` — zero-value transactions are rejected
- `transactions.sender_wallet_id != receiver_wallet_id` — self-payments blocked
- `cbdc_issuance.amount_cents <= 100_000_000` — max DD$1,000,000 per issuance
- All foreign keys use `ON DELETE RESTRICT` — financial records cannot be orphaned

### Concurrency: Why Pessimistic Locking

```sql
-- Both concurrent requests race to acquire this lock.
-- The second blocks until the first commits or rolls back.
-- This makes the balance check + update atomic.
SELECT balance FROM wallets WHERE id = $1 FOR UPDATE;
```

Optimistic locking (check-and-compare) has a window where two transactions both read the balance before either writes. Pessimistic locking eliminates that window entirely — correct for financial systems.

To prevent deadlocks in transfers (two wallets locking each other), locks are always acquired in lexicographic UUID order, regardless of which is sender/receiver.

---

## API Reference

**Base URL:** `http://localhost:8081/api/v1`  
**Auth:** `Authorization: Bearer <access_token>`  
**Idempotency:** `X-Idempotency-Key: <uuid>` required on all POST mutation endpoints

### Authentication

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/auth/register` | Create account + wallet. Returns JWT + sets refresh cookie. |
| `POST` | `/auth/login` | Authenticate. Returns JWT + sets refresh cookie. |
| `POST` | `/auth/logout` | Revoke refresh token. |
| `POST` | `/auth/refresh` | Rotate refresh token, return new access JWT. |

### Wallets

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/wallets/{id}` | Get wallet details. Own wallet or admin only. |
| `GET` | `/wallets/{id}/balance` | Get current balance. |
| `GET` | `/wallets/{id}/transactions` | Paginated transaction history with direction + counterparty name. |

### Payments

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/payments/send` | Send DD$ to another wallet. Requires `X-Idempotency-Key`. |
| `GET` | `/payments/{id}` | Get transaction detail. Must be sender or receiver. |
| `GET` | `/payments/` | Paginated payment history. Filterable by type/status/date. |

### Admin (role: admin only)

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/admin/issue-cbdc` | Mint DD$ into a wallet. Requires `X-Idempotency-Key`. Max DD$1,000,000 per action. |

### Merchant (role: merchant only) — Phase 7

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/merchant/payment-requests` | Create QR payment request with expiry. |
| `POST` | `/merchant/payment-requests/{id}/pay` | Pay a merchant QR request. |
| `GET` | `/merchant/payment-requests/{id}` | Get payment request status. |

### QR URI Format

```
cbdc://pay?to={walletId}&amount={cents}&ref={ref}&expires={unix}&sig={hmac}
```

QR codes expire after 15 minutes. The `sig` is HMAC-SHA256 over `to|amount|ref|expires` using the merchant's API key. The server re-verifies the signature before processing payment.

### Common Error Response

```json
{
  "error": {
    "code": "INSUFFICIENT_FUNDS",
    "message": "Wallet balance is too low for this transaction.",
    "request_id": "req_01HZ...",
    "timestamp": "2026-05-08T10:00:00Z"
  }
}
```

| Code | HTTP | Meaning |
|------|------|---------|
| `INVALID_REQUEST` | 400 | Malformed body |
| `UNAUTHORIZED` | 401 | Missing or invalid JWT |
| `FORBIDDEN` | 403 | Valid JWT, wrong role |
| `NOT_FOUND` | 404 | Resource does not exist |
| `INSUFFICIENT_FUNDS` | 422 | Balance too low |
| `WALLET_FROZEN` | 422 | Wallet frozen by admin |
| `SELF_PAYMENT` | 422 | Sender = receiver |
| `EXPIRED_QR` | 422 | QR code past expiry |
| `RATE_LIMITED` | 429 | Too many requests |
| `INTERNAL_ERROR` | 500 | Unexpected server error |

---

## Security Model

### Authentication

- **Access token:** HS256 JWT, 15-minute expiry, stored in memory only (never `localStorage` — XSS risk)
- **Refresh token:** 256-bit random string, 7-day expiry, stored in `HttpOnly` cookie (JavaScript cannot read it), backed by Redis for instant revocation
- **Token rotation:** on every refresh the old token is deleted; if a used token is presented again, all sessions are revoked (reuse detection)

### Password Policy

- bcrypt cost 12 (~250ms per hash — makes brute force expensive)
- Min 10 characters, requires uppercase + lowercase + digit + special character
- Max 72 characters (bcrypt truncation limit enforced explicitly)
- Lockout after 5 failed attempts in 15 minutes (per IP and per email independently)
- Constant-time responses regardless of whether email exists (prevents user enumeration)

### Transaction Integrity

Every transaction record stores an HMAC-SHA256 signature over:
```
{type}|{senderWalletID}|{receiverWalletID}|{amountCents}|{timestamp}
```
Using a server-side `SIGNING_KEY`. If any field in the database is tampered with, the stored signature no longer matches — auditors can detect altered records.

### Rate Limiting (Redis sliding window)

| Endpoint category | Limit |
|------------------|-------|
| Auth (login/register) | 10 requests / 15 min per IP |
| Payment send | 20 requests / min per user |
| Balance read | 60 requests / min per user |
| Admin endpoints | 100 requests / min per admin |

### Security Headers

All responses include:
```
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
Referrer-Policy: no-referrer
Permissions-Policy: camera=self, microphone=(), geolocation=()
```

CORS is restricted to the configured `ALLOWED_ORIGIN` — wildcard `*` is never used.

---

## Getting Started

### Prerequisites

- Docker and Docker Compose
- `make`

### First-time setup

```bash
git clone <repo>
cd cbdc
make setup        # generates secrets, builds images, runs migrations, seeds demo data
```

This runs `scripts/init.sh` which:
1. Generates cryptographically random secrets for all env vars
2. Builds Docker images
3. Starts PostgreSQL and Redis
4. Runs all 12 migrations
5. Seeds demo users, wallets, and transaction history

### Verify it's running

```bash
make health
# Backend: {"status":"ok","version":"dev",...}
```

### Start / stop

```bash
make up           # start all services (detached)
make down         # stop all services
make dev          # start with hot reload (Air for Go, Vite HMR for React)
make logs         # tail all service logs
```

---

## Demo Accounts

These are pre-loaded by `make seed`. Use them to explore the API immediately.

| Name | Email | Password | Role | Balance |
|------|-------|---------|------|---------|
| System Admin | admin@cbdc.local | Admin1234! | admin | — |
| Alice Johnson | alice@example.com | Alice1234! | user | DD$ 4,929.00 |
| Bob Smith | bob@example.com | Bob12345! | user | DD$ 1,631.50 |
| Carol Williams | carol@example.com | Carol123! | user | DD$ 410.00 |
| Good Coffee Co. | cafe@example.com | Cafe1234! | merchant | DD$ 29.50 |
| Digital Market | market@example.com | Market123! | merchant | DD$ 0.00 |

The seed data includes P2P transfers, merchant payments, a refund, and a failed transaction (insufficient funds) — covering all major transaction types.

### Quick API test

```bash
# Register a new user
curl -s -X POST http://localhost:8081/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"test@example.com","password":"Test1234!","full_name":"Test User"}' | jq .

# Login as Alice
curl -s -X POST http://localhost:8081/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"alice@example.com","password":"Alice1234!"}' | jq .tokens.access_token

# Send a payment (use Alice's access token and wallet ID from above)
curl -s -X POST http://localhost:8081/api/v1/payments/send \
  -H "Authorization: Bearer <access_token>" \
  -H "X-Idempotency-Key: $(uuidgen)" \
  -H "Content-Type: application/json" \
  -d '{"to_wallet_id":"<bob_wallet_id>","amount_cents":500,"reference":"Test payment"}' | jq .

# Issue CBDC as admin
curl -s -X POST http://localhost:8081/api/v1/admin/issue-cbdc \
  -H "Authorization: Bearer <admin_token>" \
  -H "X-Idempotency-Key: $(uuidgen)" \
  -H "Content-Type: application/json" \
  -d '{"wallet_id":"<wallet_id>","amount_cents":100000,"reason":"Demo top-up for testing"}' | jq .
```

---

## Development

### Useful make targets

```bash
make test          # run all backend unit tests with race detector
make test-unit     # unit tests only (no DB required)
make test-int      # integration tests (requires running containers)
make lint          # golangci-lint
make fmt           # gofmt
make vet           # go vet
make vuln-check    # govulncheck (known CVEs)
make sec-scan      # gosec (security static analysis)
make swagger       # regenerate OpenAPI docs from annotations
make shell-db      # psql shell into the running postgres container
make shell-redis   # redis-cli into the running redis container
make migrate-down  # roll back last migration
make reset-db      # DESTRUCTIVE: drop + recreate + migrate + seed
```

### Environment variables

All secrets are generated by `make setup`. Key variables in `backend/.env`:

| Variable | Purpose |
|----------|---------|
| `JWT_SECRET` | 256-bit key for HS256 JWT signing |
| `SIGNING_KEY` | 256-bit key for HMAC transaction signatures |
| `DB_PASSWORD` | PostgreSQL password |
| `REDIS_PASSWORD` | Redis auth password |
| `JWT_ACCESS_TTL_SECONDS` | Access token lifetime (default: 900 = 15 min) |
| `JWT_REFRESH_TTL_SECONDS` | Refresh token lifetime (default: 604800 = 7 days) |
| `ALLOWED_ORIGIN` | CORS allowed origin (default: `http://localhost:3000`) |
| `APP_ENV` | `development` or `production` (controls secure cookies) |

---

## Implementation Status

| Phase | Feature | Backend | Frontend |
|-------|---------|---------|----------|
| 1 | Foundation (DB, Redis, health, Docker) | ✅ Complete | — |
| 2 | Authentication (register, login, JWT, refresh) | ✅ Complete | 🔲 Pending |
| 3 | Wallets & ledger core | ✅ Complete | 🔲 Pending |
| 4 | CBDC issuance (admin) | ✅ Complete | 🔲 Pending |
| 5 | P2P payments (send, history) | ✅ Complete | 🔲 Pending |
| 6 | QR code payments | 🔲 Pending | 🔲 Pending |
| 7 | Merchant endpoints + webhooks | 🔲 Pending | 🔲 Pending |
| 8 | WebSocket live notifications | 🔲 Pending | 🔲 Pending |
| 9 | Admin dashboard (freeze, audit log) | 🔲 Pending | 🔲 Pending |
| 10 | PWA (service worker, offline mode) | — | 🔲 Pending |

---

## Further Reading

Detailed documentation lives in `docs/`:

| File | Content |
|------|---------|
| `docs/01-ARCHITECTURE.md` | Service boundaries, data flow diagrams, WebSocket hub design |
| `docs/02-SECURITY.md` | Full threat model, auth design, RBAC matrix, audit model |
| `docs/03-DATABASE.md` | Complete schema SQL, index rationale, migration strategy |
| `docs/04-API.md` | Full API reference with request/response examples |
| `docs/05-FRONTEND.md` | Component structure, PWA config, QR flow |
| `docs/06-DEVOPS.md` | Docker setup, CI/CD, environment variable reference |
| `docs/07-IMPLEMENTATION-PLAN.md` | Phased build order with per-file checklist |
| `docs/08-SEED-DATA.md` | Demo transaction scenarios and expected balances |

---

## License

MIT — see [LICENSE](LICENSE).

> This is a portfolio/educational project. It is not audited for production use and should never handle real financial value.

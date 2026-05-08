-- Wallets hold CBDC balances. Key design decisions:
--
-- 1. balance is stored as BIGINT (cents) not DECIMAL/FLOAT.
--    Floating-point arithmetic cannot represent 0.1 + 0.2 = 0.3 exactly.
--    Integer cents arithmetic is exact: 10 + 20 = 30, always.
--
-- 2. The CHECK constraint (balance >= 0) enforces non-negative balances at the DB level.
--    Even if application code has a bug, the database will reject the operation.
--
-- 3. One wallet per user per currency — enforced by unique constraint.
--    Deferred so it's checked at transaction commit time (allows swaps within a tx).

CREATE TABLE wallets (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    currency        VARCHAR(10) NOT NULL DEFAULT 'DD$',
    balance         BIGINT NOT NULL DEFAULT 0,        -- in cents, e.g. 1050 = DD$10.50

    -- Freeze state: admin can freeze a wallet to block all sends/receives
    is_frozen       BOOLEAN NOT NULL DEFAULT FALSE,
    frozen_reason   TEXT,
    frozen_by       UUID REFERENCES users(id),        -- admin who froze it
    frozen_at       TIMESTAMPTZ,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ,

    -- Financial invariant: balance can never be negative
    CONSTRAINT wallets_balance_non_negative CHECK (balance >= 0),
    CONSTRAINT wallets_currency_valid CHECK (currency IN ('DD$')),
    -- One active wallet per user per currency
    CONSTRAINT wallets_user_currency_unique UNIQUE (user_id, currency)
);

CREATE INDEX idx_wallets_user_id ON wallets (user_id) WHERE deleted_at IS NULL;

CREATE TRIGGER wallets_updated_at
    BEFORE UPDATE ON wallets
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

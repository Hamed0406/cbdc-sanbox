-- Transactions record every money movement event.
-- Each transaction pairs with exactly 2 ledger_entries (debit + credit).
--
-- idempotency_key: clients supply this to prevent duplicate payments on retry.
-- Scoped per sender_wallet_id so different users can use the same key independently.
--
-- signature: HMAC-SHA256 of (txnID|sender|receiver|amount|timestamp).
-- Stored so auditors can verify records haven't been tampered with after the fact.

CREATE TABLE transactions (
    id                    UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    idempotency_key       VARCHAR(128),
    type                  transaction_type NOT NULL,
    status                transaction_status NOT NULL DEFAULT 'PENDING',
    sender_wallet_id      UUID REFERENCES wallets(id) ON DELETE RESTRICT,
    receiver_wallet_id    UUID REFERENCES wallets(id) ON DELETE RESTRICT,
    amount_cents          BIGINT NOT NULL,
    fee_cents             BIGINT NOT NULL DEFAULT 0,
    reference             VARCHAR(256),              -- human-readable memo / order ref
    signature             VARCHAR(128) NOT NULL,     -- HMAC-SHA256, see pkg/crypto
    metadata              JSONB DEFAULT '{}',
    failure_reason        TEXT,

    -- Links refund transactions back to the original payment
    parent_transaction_id UUID REFERENCES transactions(id),

    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    confirmed_at  TIMESTAMPTZ,
    settled_at    TIMESTAMPTZ,

    CONSTRAINT transactions_amount_positive CHECK (amount_cents > 0),
    CONSTRAINT transactions_fee_non_negative CHECK (fee_cents >= 0),
    -- Prevent self-transfers: a wallet cannot send to itself
    CONSTRAINT transactions_not_self_transfer CHECK (
        sender_wallet_id IS NULL OR
        receiver_wallet_id IS NULL OR
        sender_wallet_id != receiver_wallet_id
    ),
    -- Idempotency is unique per sender wallet, not globally.
    -- This allows different wallets to independently use the same key string.
    CONSTRAINT transactions_idempotency_unique UNIQUE (sender_wallet_id, idempotency_key)
);

-- Query patterns: users browse their own history sorted by most recent first
CREATE INDEX idx_transactions_sender ON transactions (sender_wallet_id, created_at DESC);
CREATE INDEX idx_transactions_receiver ON transactions (receiver_wallet_id, created_at DESC);
CREATE INDEX idx_transactions_status ON transactions (status, created_at DESC);
CREATE INDEX idx_transactions_type ON transactions (type, created_at DESC);
-- Idempotency lookup: exact match on wallet + key
CREATE INDEX idx_transactions_idempotency ON transactions (sender_wallet_id, idempotency_key);

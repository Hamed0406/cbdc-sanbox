-- Payment requests are QR-code-backed payment intents created by merchants.
-- Lifecycle: PENDING → PAID (or EXPIRED or CANCELLED)
--
-- qr_payload stores the full cbdc:// URI that gets encoded into the QR image.
-- expires_at enforces a 15-minute scan window — prevents old QR codes being replayed.

CREATE TABLE payment_requests (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    merchant_id         UUID NOT NULL REFERENCES merchants(id) ON DELETE RESTRICT,
    amount_cents        BIGINT NOT NULL,
    currency            VARCHAR(10) NOT NULL DEFAULT 'DD$',
    reference           VARCHAR(64),     -- merchant's internal order reference
    description         VARCHAR(500),
    qr_payload          TEXT NOT NULL,   -- full cbdc:// URI
    status              payment_request_status NOT NULL DEFAULT 'PENDING',
    paid_by_wallet_id   UUID REFERENCES wallets(id),
    transaction_id      UUID REFERENCES transactions(id),
    expires_at          TIMESTAMPTZ NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    paid_at             TIMESTAMPTZ,

    CONSTRAINT payment_requests_amount_positive CHECK (amount_cents > 0),
    -- Expiry must be in the future relative to creation — caught at app level too
    CONSTRAINT payment_requests_expiry_after_creation CHECK (expires_at > created_at)
);

CREATE INDEX idx_payment_requests_merchant ON payment_requests (merchant_id, created_at DESC);
-- Used by the expiry cleanup job to find stale PENDING requests
CREATE INDEX idx_payment_requests_status ON payment_requests (status, expires_at);

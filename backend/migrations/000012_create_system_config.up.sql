-- Runtime configuration values that admins may need to adjust without redeployment.
-- Examples: transaction limits, QR expiry windows, rate limit overrides.

CREATE TABLE system_config (
    key         VARCHAR(100) PRIMARY KEY,
    value       TEXT NOT NULL,
    description TEXT,
    updated_by  UUID REFERENCES users(id),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE webhook_deliveries (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    merchant_id     UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,
    event_type      VARCHAR(50) NOT NULL,   -- payment.completed, refund.issued, etc.
    payload         JSONB NOT NULL,
    url             VARCHAR(500) NOT NULL,
    response_status SMALLINT,              -- HTTP status returned by merchant's endpoint
    response_body   TEXT,
    attempt_count   SMALLINT NOT NULL DEFAULT 0,
    last_attempt_at TIMESTAMPTZ,
    delivered_at    TIMESTAMPTZ,           -- set on first successful delivery (2xx)
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Used by the retry worker: find undelivered webhooks older than N seconds
CREATE INDEX idx_webhook_deliveries_undelivered ON webhook_deliveries (created_at)
    WHERE delivered_at IS NULL;
CREATE INDEX idx_webhook_deliveries_merchant ON webhook_deliveries (merchant_id, created_at DESC);

-- Seed default configuration values
INSERT INTO system_config (key, value, description) VALUES
    ('max_transaction_cents',   '100000000',   'Maximum single transaction in cents (DD$1,000,000)'),
    ('min_transaction_cents',   '1',           'Minimum transaction in cents (DD$0.01)'),
    ('max_daily_send_cents',    '500000000',   'Maximum daily send per wallet in cents (DD$5,000,000)'),
    ('qr_expiry_seconds',       '900',         'QR code validity in seconds (15 minutes)'),
    ('total_supply_cap_cents',  '10000000000', 'Total DD$ supply cap in cents (DD$100,000,000)');

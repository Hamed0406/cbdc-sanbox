-- Merchants are users with role='merchant' who can create payment requests.
-- They authenticate via either:
--   a) JWT (for the dashboard UI)
--   b) API key (for server-to-server integrations, e.g. their checkout system)
--
-- api_key_hash: we store SHA-256 of the actual API key, not the key itself.
-- If the DB is compromised, the attacker gets hashes, not valid keys.
-- api_key_prefix: the first 8 chars of the key, shown in the dashboard so
-- merchants can identify which key is which without exposing the full key.

CREATE TABLE merchants (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    business_name   VARCHAR(200) NOT NULL,
    business_type   VARCHAR(100),
    webhook_url     VARCHAR(500),  -- where we POST payment.completed events
    api_key_hash    VARCHAR(128) NOT NULL,
    api_key_prefix  VARCHAR(20) NOT NULL,
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- One merchant profile per user
    CONSTRAINT merchants_user_unique UNIQUE (user_id)
);

CREATE INDEX idx_merchants_user_id ON merchants (user_id);
CREATE INDEX idx_merchants_api_key_prefix ON merchants (api_key_prefix);

CREATE TRIGGER merchants_updated_at
    BEFORE UPDATE ON merchants
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

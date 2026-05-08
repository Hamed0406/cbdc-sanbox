-- Sessions track active refresh tokens.
-- WHY: JWT access tokens are stateless and cannot be revoked before expiry.
-- Refresh tokens are stored in this table so we can:
--   a) Revoke individual sessions (logout)
--   b) Revoke all sessions for a user (account compromise, forced logout)
--   c) Detect token reuse (rotation attack detection)
--
-- token_hash: SHA-256 of the actual refresh token. We NEVER store raw tokens.
-- If this table is stolen, the attacker has only hashes — not valid tokens.

CREATE TABLE sessions (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash      VARCHAR(128) NOT NULL,    -- SHA-256 of raw refresh token
    user_agent      VARCHAR(500),
    ip_address      INET,
    is_revoked      BOOLEAN NOT NULL DEFAULT FALSE,
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT sessions_token_unique UNIQUE (token_hash)
);

-- Lookup active sessions for a user (e.g., "show all active sessions" in settings)
CREATE INDEX idx_sessions_user_id ON sessions (user_id, expires_at);
-- Fast token validation on every API request
CREATE INDEX idx_sessions_token_hash ON sessions (token_hash) WHERE is_revoked = FALSE;

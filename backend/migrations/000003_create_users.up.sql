-- Users table stores account credentials and profile data.
-- Passwords are NEVER stored in plaintext — only bcrypt hashes.
-- Soft-delete pattern: deleted_at IS NULL means active; we never hard-delete users
-- because transaction history references user_id and must remain intact.

CREATE TABLE users (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    email           VARCHAR(254) NOT NULL,
    password_hash   VARCHAR(60) NOT NULL,             -- bcrypt output is always 60 chars
    full_name       VARCHAR(200) NOT NULL,
    role            user_role NOT NULL DEFAULT 'user',
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    kyc_status      VARCHAR(20) NOT NULL DEFAULT 'pending', -- pending | verified | rejected
    failed_logins   SMALLINT NOT NULL DEFAULT 0,
    locked_until    TIMESTAMPTZ,                      -- NULL means not locked
    last_login_at   TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ,                      -- soft delete timestamp

    CONSTRAINT users_email_unique UNIQUE (email),
    -- Basic email format check at DB level — application validates more strictly
    CONSTRAINT users_email_format CHECK (email ~* '^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$'),
    CONSTRAINT users_failed_logins_range CHECK (failed_logins >= 0 AND failed_logins <= 100)
);

-- Partial index: most queries filter on active users only, which keeps the index small
CREATE INDEX idx_users_email ON users (email) WHERE deleted_at IS NULL;
CREATE INDEX idx_users_role ON users (role) WHERE deleted_at IS NULL;

-- Auto-update updated_at on every row change
CREATE OR REPLACE FUNCTION update_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER users_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

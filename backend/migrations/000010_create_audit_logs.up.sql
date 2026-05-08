-- Audit log is APPEND-ONLY. There are no UPDATE or DELETE operations on this table.
-- This is enforced at two levels:
--   1. The application DB role (cbdc_app) has no UPDATE/DELETE permission on this table
--   2. Application code never calls UPDATE/DELETE on audit_logs
--
-- We use BIGSERIAL (auto-increment integer) not UUID for the PK because:
--   - Audit entries are always inserted in time order — sequential ID is natural
--   - BIGSERIAL inserts are faster than UUID in append-heavy workloads
--   - Sequential IDs make it easy to detect gaps (evidence of tampering)
--
-- WHY log before the action executes?
-- If we log after, a crash between the action and the log write leaves no trace.
-- Pre-action logging ensures even failed attempts are recorded.

CREATE TABLE audit_logs (
    id            BIGSERIAL PRIMARY KEY,
    actor_id      UUID,              -- NULL for unauthenticated or system actions
    actor_role    user_role,
    action        VARCHAR(100) NOT NULL,
    resource_type VARCHAR(50),
    resource_id   UUID,
    ip_address    INET,
    user_agent    VARCHAR(500),
    request_id    VARCHAR(128),
    metadata      JSONB DEFAULT '{}',
    success       BOOLEAN NOT NULL,
    error_code    VARCHAR(50),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
    -- Deliberately NO updated_at, NO deleted_at — immutable records
);

CREATE INDEX idx_audit_logs_actor ON audit_logs (actor_id, created_at DESC);
CREATE INDEX idx_audit_logs_action ON audit_logs (action, created_at DESC);
CREATE INDEX idx_audit_logs_resource ON audit_logs (resource_type, resource_id, created_at DESC);
-- Admin monitoring scrolls the log most-recent-first
CREATE INDEX idx_audit_logs_created_at ON audit_logs (created_at DESC);

COMMENT ON TABLE audit_logs IS 'Append-only security audit log. Application role must not have UPDATE/DELETE on this table.';

-- +goose Up
CREATE TABLE IF NOT EXISTS security_audit_logs (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    action VARCHAR(128) NOT NULL,
    resource VARCHAR(255) NOT NULL,
    keys_json TEXT NOT NULL DEFAULT '[]',
    outcome VARCHAR(32) NOT NULL DEFAULT 'accepted',
    created_at BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_security_audit_logs_user_created
    ON security_audit_logs(user_id, created_at DESC, id DESC);

-- +goose Down
DROP TABLE IF EXISTS security_audit_logs;

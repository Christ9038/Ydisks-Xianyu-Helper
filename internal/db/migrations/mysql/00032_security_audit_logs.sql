-- +goose Up
CREATE TABLE IF NOT EXISTS security_audit_logs (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    user_id BIGINT NOT NULL,
    action VARCHAR(128) NOT NULL,
    resource VARCHAR(255) NOT NULL,
    keys_json TEXT NOT NULL,
    outcome VARCHAR(32) NOT NULL DEFAULT 'accepted',
    created_at BIGINT NOT NULL,
    INDEX idx_security_audit_logs_user_created (user_id, created_at, id)
);

-- +goose Down
DROP TABLE IF EXISTS security_audit_logs;

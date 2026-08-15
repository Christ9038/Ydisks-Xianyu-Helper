-- +goose Up
CREATE TABLE IF NOT EXISTS security_audit_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    action TEXT NOT NULL,
    resource TEXT NOT NULL,
    keys_json TEXT NOT NULL DEFAULT '[]',
    outcome TEXT NOT NULL DEFAULT 'accepted',
    created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_security_audit_logs_user_created
    ON security_audit_logs(user_id, created_at DESC, id DESC);

-- +goose Down
DROP TABLE IF EXISTS security_audit_logs;

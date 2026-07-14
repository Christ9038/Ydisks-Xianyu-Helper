-- +goose Up
CREATE TABLE automation_pending_tasks (
    id BIGSERIAL PRIMARY KEY,
    task_key TEXT NOT NULL UNIQUE,
    cookie_id TEXT NOT NULL REFERENCES cookies(id) ON DELETE CASCADE,
    trigger_type TEXT NOT NULL,
    task_json TEXT NOT NULL,
    due_at BIGINT NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    lease_expires_at BIGINT NOT NULL DEFAULT 0,
    error_message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_automation_pending_due ON automation_pending_tasks(status, due_at, lease_expires_at);

-- +goose Down
DROP TABLE IF EXISTS automation_pending_tasks;

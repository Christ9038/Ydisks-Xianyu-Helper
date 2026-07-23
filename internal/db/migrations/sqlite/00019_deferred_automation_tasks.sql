-- +goose Up
CREATE TABLE automation_pending_tasks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_key TEXT NOT NULL UNIQUE,
    cookie_id TEXT NOT NULL,
    trigger_type TEXT NOT NULL,
    task_json TEXT NOT NULL,
    due_at INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    lease_expires_at INTEGER NOT NULL DEFAULT 0,
    error_message TEXT NOT NULL DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);
CREATE INDEX idx_automation_pending_due ON automation_pending_tasks(status, due_at, lease_expires_at);

-- +goose Down
DROP TABLE IF EXISTS automation_pending_tasks;

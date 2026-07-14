-- +goose Up
CREATE TABLE automation_pending_tasks (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    task_key VARCHAR(512) NOT NULL UNIQUE,
    cookie_id VARCHAR(255) NOT NULL,
    trigger_type VARCHAR(64) NOT NULL,
    task_json LONGTEXT NOT NULL,
    due_at BIGINT NOT NULL DEFAULT 0,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    lease_expires_at BIGINT NOT NULL DEFAULT 0,
    error_message TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_pending_automation_cookie FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);
CREATE INDEX idx_automation_pending_due ON automation_pending_tasks(status, due_at, lease_expires_at);

-- +goose Down
DROP TABLE IF EXISTS automation_pending_tasks;

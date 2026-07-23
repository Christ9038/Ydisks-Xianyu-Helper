-- +goose Up
ALTER TABLE automation_runs ADD COLUMN lease_expires_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE automation_runs ADD COLUMN attempt_count INTEGER NOT NULL DEFAULT 1;
ALTER TABLE automation_runs ADD COLUMN next_retry_at INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_automation_runs_recovery
    ON automation_runs(status, lease_expires_at, next_retry_at);

-- +goose Down
DROP INDEX IF EXISTS idx_automation_runs_recovery;
ALTER TABLE automation_runs DROP COLUMN next_retry_at;
ALTER TABLE automation_runs DROP COLUMN attempt_count;
ALTER TABLE automation_runs DROP COLUMN lease_expires_at;

-- +goose Up
ALTER TABLE automation_runs
    ADD COLUMN lease_expires_at BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN attempt_count INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN next_retry_at BIGINT NOT NULL DEFAULT 0;
CREATE INDEX idx_automation_runs_recovery
    ON automation_runs(status, lease_expires_at, next_retry_at);

-- +goose Down
DROP INDEX idx_automation_runs_recovery ON automation_runs;
ALTER TABLE automation_runs
    DROP COLUMN next_retry_at,
    DROP COLUMN attempt_count,
    DROP COLUMN lease_expires_at;

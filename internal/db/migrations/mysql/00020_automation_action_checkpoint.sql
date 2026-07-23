-- +goose Up
ALTER TABLE automation_runs ADD COLUMN action_cursor INT NOT NULL DEFAULT 0;
ALTER TABLE automation_runs ADD COLUMN action_started TINYINT(1) NOT NULL DEFAULT 0;
CREATE INDEX idx_automation_runs_action_state ON automation_runs(status, action_started, lease_expires_at);

-- +goose Down
DROP INDEX idx_automation_runs_action_state ON automation_runs;
ALTER TABLE automation_runs DROP COLUMN action_started;
ALTER TABLE automation_runs DROP COLUMN action_cursor;

-- +goose Up
ALTER TABLE automation_runs ADD COLUMN action_cursor INTEGER NOT NULL DEFAULT 0;
ALTER TABLE automation_runs ADD COLUMN action_started INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_automation_runs_action_state ON automation_runs(status, action_started, lease_expires_at);

-- +goose Down
DROP INDEX IF EXISTS idx_automation_runs_action_state;
ALTER TABLE automation_runs DROP COLUMN action_started;
ALTER TABLE automation_runs DROP COLUMN action_cursor;

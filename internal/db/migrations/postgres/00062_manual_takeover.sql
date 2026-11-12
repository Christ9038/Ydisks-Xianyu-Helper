-- +goose Up
ALTER TABLE cookies ADD COLUMN manual_takeover_minutes INTEGER NOT NULL DEFAULT 0;
ALTER TABLE cookies ADD COLUMN manual_takeover_scope TEXT NOT NULL DEFAULT 'ai';
ALTER TABLE keywords ADD COLUMN notify_manual INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE keywords DROP COLUMN notify_manual;
ALTER TABLE cookies DROP COLUMN manual_takeover_scope;
ALTER TABLE cookies DROP COLUMN manual_takeover_minutes;

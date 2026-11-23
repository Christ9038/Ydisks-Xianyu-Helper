-- +goose Up
ALTER TABLE cookies ADD COLUMN reply_merge_seconds INT NOT NULL DEFAULT 3 AFTER manual_takeover_scope;

-- +goose Down
ALTER TABLE cookies DROP COLUMN reply_merge_seconds;

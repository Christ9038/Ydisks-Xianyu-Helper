-- +goose Up
ALTER TABLE cookies ADD COLUMN reply_merge_seconds INTEGER NOT NULL DEFAULT 3;

-- +goose Down
ALTER TABLE cookies DROP COLUMN reply_merge_seconds;

-- +goose Up
ALTER TABLE cookies ADD COLUMN auto_relist INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE cookies DROP COLUMN auto_relist;

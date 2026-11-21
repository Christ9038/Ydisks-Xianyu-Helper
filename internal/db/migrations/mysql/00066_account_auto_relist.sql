-- +goose Up
ALTER TABLE cookies ADD COLUMN auto_relist INT NOT NULL DEFAULT 0 AFTER auto_bargain;

-- +goose Down
ALTER TABLE cookies DROP COLUMN auto_relist;

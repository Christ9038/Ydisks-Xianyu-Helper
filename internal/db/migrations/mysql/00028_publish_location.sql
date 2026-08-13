-- +goose Up
ALTER TABLE item_publish_batches ADD COLUMN location_json LONGTEXT NULL;
UPDATE item_publish_batches SET location_json='{}' WHERE location_json IS NULL;
ALTER TABLE item_publish_batches MODIFY COLUMN location_json LONGTEXT NOT NULL;

-- +goose Down
ALTER TABLE item_publish_batches DROP COLUMN location_json;

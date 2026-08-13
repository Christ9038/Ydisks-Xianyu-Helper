-- +goose Up
ALTER TABLE item_publish_batches ADD COLUMN location_json TEXT NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE item_publish_batches DROP COLUMN location_json;

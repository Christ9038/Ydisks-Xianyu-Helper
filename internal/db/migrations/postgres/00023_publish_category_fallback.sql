-- +goose Up
ALTER TABLE item_publish_batch_rows ADD COLUMN category_json TEXT NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE item_publish_batch_rows DROP COLUMN category_json;

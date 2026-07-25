-- +goose Up
ALTER TABLE item_publish_batch_rows ADD COLUMN category_json LONGTEXT NULL;
UPDATE item_publish_batch_rows SET category_json='{}' WHERE category_json IS NULL;
ALTER TABLE item_publish_batch_rows MODIFY COLUMN category_json LONGTEXT NOT NULL;

-- +goose Down
ALTER TABLE item_publish_batch_rows DROP COLUMN category_json;

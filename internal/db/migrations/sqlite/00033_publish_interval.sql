-- +goose Up
-- +goose StatementBegin
ALTER TABLE item_publish_batches ADD COLUMN publish_interval_seconds INTEGER NOT NULL DEFAULT 5;
ALTER TABLE item_publish_batches ADD COLUMN last_publish_started_at_millis INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE item_publish_batches DROP COLUMN last_publish_started_at_millis;
ALTER TABLE item_publish_batches DROP COLUMN publish_interval_seconds;
-- +goose StatementEnd

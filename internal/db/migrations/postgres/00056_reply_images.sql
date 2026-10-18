-- +goose Up
ALTER TABLE default_replies ADD COLUMN reply_image_path TEXT;
ALTER TABLE item_replay ADD COLUMN reply_image_url TEXT;
ALTER TABLE item_replay ADD COLUMN reply_image_path TEXT;

-- +goose Down
ALTER TABLE item_replay DROP COLUMN reply_image_path;
ALTER TABLE item_replay DROP COLUMN reply_image_url;
ALTER TABLE default_replies DROP COLUMN reply_image_path;

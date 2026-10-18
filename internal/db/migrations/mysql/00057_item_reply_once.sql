-- +goose Up
ALTER TABLE item_replay ADD COLUMN reply_once INTEGER NOT NULL DEFAULT 0;
ALTER TABLE default_reply_records ADD COLUMN item_id VARCHAR(255) NOT NULL DEFAULT '', DROP INDEX uk_default_reply_records, ADD UNIQUE KEY uk_default_reply_records (cookie_id,chat_id,item_id);

-- +goose Down
DELETE FROM default_reply_records WHERE item_id<>'';
ALTER TABLE default_reply_records DROP INDEX uk_default_reply_records, ADD UNIQUE KEY uk_default_reply_records (cookie_id,chat_id), DROP COLUMN item_id;
ALTER TABLE item_replay DROP COLUMN reply_once;

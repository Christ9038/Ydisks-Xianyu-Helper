-- +goose Up
ALTER TABLE item_replay ADD COLUMN reply_once INTEGER NOT NULL DEFAULT 0;
ALTER TABLE default_reply_records ADD COLUMN item_id TEXT NOT NULL DEFAULT '';
ALTER TABLE default_reply_records DROP CONSTRAINT default_reply_records_cookie_id_chat_id_key;
ALTER TABLE default_reply_records ADD CONSTRAINT default_reply_records_cookie_id_chat_id_key UNIQUE(cookie_id,chat_id,item_id);

-- +goose Down
DELETE FROM default_reply_records WHERE item_id<>'';
ALTER TABLE default_reply_records DROP CONSTRAINT default_reply_records_cookie_id_chat_id_key;
ALTER TABLE default_reply_records ADD CONSTRAINT default_reply_records_cookie_id_chat_id_key UNIQUE(cookie_id,chat_id);
ALTER TABLE default_reply_records DROP COLUMN item_id;
ALTER TABLE item_replay DROP COLUMN reply_once;

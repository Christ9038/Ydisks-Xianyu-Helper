-- +goose Up
ALTER TABLE default_reply_records ADD COLUMN status TEXT NOT NULL DEFAULT 'sent';
ALTER TABLE default_reply_records ADD COLUMN text_sent INTEGER NOT NULL DEFAULT 1;
ALTER TABLE default_reply_records ADD COLUMN image_sent INTEGER NOT NULL DEFAULT 1;
ALTER TABLE default_reply_records ADD COLUMN last_error TEXT NOT NULL DEFAULT '';
ALTER TABLE default_reply_records ADD COLUMN updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP;

-- +goose Down
ALTER TABLE default_reply_records DROP COLUMN updated_at;
ALTER TABLE default_reply_records DROP COLUMN last_error;
ALTER TABLE default_reply_records DROP COLUMN image_sent;
ALTER TABLE default_reply_records DROP COLUMN text_sent;
ALTER TABLE default_reply_records DROP COLUMN status;

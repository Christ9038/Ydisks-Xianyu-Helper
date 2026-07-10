-- +goose Up
ALTER TABLE default_reply_records ADD COLUMN status TEXT NOT NULL DEFAULT 'sent';
ALTER TABLE default_reply_records ADD COLUMN text_sent INTEGER NOT NULL DEFAULT 1;
ALTER TABLE default_reply_records ADD COLUMN image_sent INTEGER NOT NULL DEFAULT 1;
ALTER TABLE default_reply_records ADD COLUMN last_error TEXT NOT NULL DEFAULT '';
ALTER TABLE default_reply_records ADD COLUMN updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP;

-- +goose Down
CREATE TABLE default_reply_records_legacy (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cookie_id TEXT NOT NULL,
    chat_id TEXT NOT NULL,
    replied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(cookie_id, chat_id),
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);
INSERT INTO default_reply_records_legacy (id,cookie_id,chat_id,replied_at)
SELECT id,cookie_id,chat_id,replied_at FROM default_reply_records;
DROP TABLE default_reply_records;
ALTER TABLE default_reply_records_legacy RENAME TO default_reply_records;

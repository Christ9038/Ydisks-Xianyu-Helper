-- +goose Up
CREATE TABLE chat_quick_replies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cookie_id TEXT NOT NULL REFERENCES cookies(id) ON DELETE CASCADE,
    content TEXT NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX idx_chat_quick_replies_account_created ON chat_quick_replies(cookie_id, id DESC);

CREATE TABLE chat_buyer_notes (
    cookie_id TEXT NOT NULL REFERENCES cookies(id) ON DELETE CASCADE,
    buyer_id TEXT NOT NULL,
    content TEXT NOT NULL,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (cookie_id, buyer_id)
);

-- +goose Down
DROP TABLE IF EXISTS chat_buyer_notes;
DROP TABLE IF EXISTS chat_quick_replies;

-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS ws_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cookie_id TEXT NOT NULL,
    direction TEXT NOT NULL DEFAULT 'in',
    raw_text TEXT NOT NULL DEFAULT '',
    parsed_json TEXT NOT NULL DEFAULT '',
    message_kind TEXT NOT NULL DEFAULT '',
    parse_status TEXT NOT NULL DEFAULT 'raw',
    error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_ws_messages_cookie_id_created_at ON ws_messages(cookie_id, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_ws_messages_cookie_id_created_at;
DROP TABLE IF EXISTS ws_messages;
-- +goose StatementEnd

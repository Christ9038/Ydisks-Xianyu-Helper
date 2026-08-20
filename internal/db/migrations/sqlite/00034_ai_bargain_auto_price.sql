-- +goose Up
ALTER TABLE ai_reply_settings
    ADD COLUMN auto_adjust_price_enabled BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE ai_bargain_quotes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cookie_id TEXT NOT NULL,
    chat_id TEXT NOT NULL,
    buyer_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    price_cents INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    order_id TEXT,
    expires_at INTEGER NOT NULL,
    error_message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE,
    UNIQUE (cookie_id, order_id)
);

CREATE INDEX idx_ai_bargain_quotes_pending
    ON ai_bargain_quotes(cookie_id, buyer_id, item_id, chat_id, status, expires_at, id);

-- +goose Down
DROP INDEX IF EXISTS idx_ai_bargain_quotes_pending;
DROP TABLE IF EXISTS ai_bargain_quotes;
ALTER TABLE ai_reply_settings DROP COLUMN auto_adjust_price_enabled;

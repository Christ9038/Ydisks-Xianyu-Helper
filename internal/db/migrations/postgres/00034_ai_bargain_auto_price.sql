-- +goose Up
ALTER TABLE ai_reply_settings
    ADD COLUMN auto_adjust_price_enabled INTEGER NOT NULL DEFAULT 0;

CREATE TABLE ai_bargain_quotes (
    id BIGSERIAL PRIMARY KEY,
    cookie_id TEXT NOT NULL REFERENCES cookies(id) ON DELETE CASCADE,
    chat_id TEXT NOT NULL,
    buyer_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    price_cents BIGINT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    order_id TEXT,
    expires_at BIGINT NOT NULL,
    error_message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (cookie_id, order_id)
);

CREATE INDEX idx_ai_bargain_quotes_pending
    ON ai_bargain_quotes(cookie_id, buyer_id, item_id, chat_id, status, expires_at, id);

-- +goose Down
DROP INDEX IF EXISTS idx_ai_bargain_quotes_pending;
DROP TABLE IF EXISTS ai_bargain_quotes;
ALTER TABLE ai_reply_settings DROP COLUMN auto_adjust_price_enabled;

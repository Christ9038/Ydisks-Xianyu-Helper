-- +goose Up
ALTER TABLE ai_reply_settings
    ADD COLUMN auto_adjust_price_enabled BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE ai_bargain_quotes (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    cookie_id VARCHAR(255) NOT NULL,
    chat_id VARCHAR(255) NOT NULL,
    buyer_id VARCHAR(255) NOT NULL,
    item_id VARCHAR(255) NOT NULL,
    price_cents BIGINT NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    order_id VARCHAR(255) NULL,
    expires_at BIGINT NOT NULL,
    error_message TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_ai_bargain_quotes_cookie FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE,
    UNIQUE KEY uq_ai_bargain_quotes_order (cookie_id, order_id),
    KEY idx_ai_bargain_quotes_pending (cookie_id(64), buyer_id(64), item_id(64), chat_id(64), status, expires_at, id)
);

-- +goose Down
DROP TABLE IF EXISTS ai_bargain_quotes;
ALTER TABLE ai_reply_settings DROP COLUMN auto_adjust_price_enabled;

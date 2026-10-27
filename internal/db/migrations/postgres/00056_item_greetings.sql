-- +goose Up
-- 商品首次回复：规则、规则覆盖的商品和按会话去重的投递记录。
CREATE TABLE IF NOT EXISTS item_greeting_rules (
    id BIGSERIAL PRIMARY KEY,
    cookie_id TEXT NOT NULL REFERENCES cookies(id) ON DELETE CASCADE,
    name TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 1,
    reply_content TEXT NOT NULL DEFAULT '',
    reply_image_url TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_item_greeting_rules_cookie ON item_greeting_rules(cookie_id);

CREATE TABLE IF NOT EXISTS item_greeting_rule_items (
    rule_id BIGINT NOT NULL REFERENCES item_greeting_rules(id) ON DELETE CASCADE,
    cookie_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    PRIMARY KEY (rule_id, item_id),
    UNIQUE (cookie_id, item_id)
);

CREATE TABLE IF NOT EXISTS item_greeting_records (
    id BIGSERIAL PRIMARY KEY,
    cookie_id TEXT NOT NULL REFERENCES cookies(id) ON DELETE CASCADE,
    chat_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'sent',
    text_sent INTEGER NOT NULL DEFAULT 1,
    image_sent INTEGER NOT NULL DEFAULT 1,
    last_error TEXT NOT NULL DEFAULT '',
    lease_expires_at BIGINT NOT NULL DEFAULT 0,
    replied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (cookie_id, chat_id)
);

-- +goose Down
DROP TABLE IF EXISTS item_greeting_records;
DROP TABLE IF EXISTS item_greeting_rule_items;
DROP TABLE IF EXISTS item_greeting_rules;

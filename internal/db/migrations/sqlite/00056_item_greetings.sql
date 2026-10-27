-- +goose Up
-- 商品首次回复：规则、规则覆盖的商品和按会话去重的投递记录。
CREATE TABLE IF NOT EXISTS item_greeting_rules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cookie_id TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 1,
    reply_content TEXT NOT NULL DEFAULT '',
    reply_image_url TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_item_greeting_rules_cookie ON item_greeting_rules(cookie_id);

CREATE TABLE IF NOT EXISTS item_greeting_rule_items (
    rule_id INTEGER NOT NULL,
    cookie_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    PRIMARY KEY (rule_id, item_id),
    UNIQUE (cookie_id, item_id),
    FOREIGN KEY (rule_id) REFERENCES item_greeting_rules(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS item_greeting_records (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cookie_id TEXT NOT NULL,
    chat_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'sent',
    text_sent INTEGER NOT NULL DEFAULT 1,
    image_sent INTEGER NOT NULL DEFAULT 1,
    last_error TEXT NOT NULL DEFAULT '',
    lease_expires_at INTEGER NOT NULL DEFAULT 0,
    replied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (cookie_id, chat_id),
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE IF EXISTS item_greeting_records;
DROP TABLE IF EXISTS item_greeting_rule_items;
DROP TABLE IF EXISTS item_greeting_rules;

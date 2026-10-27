-- +goose Up
-- 商品首次回复：规则、规则覆盖的商品和按会话去重的投递记录。
CREATE TABLE IF NOT EXISTS item_greeting_rules (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    cookie_id VARCHAR(255) NOT NULL,
    name VARCHAR(255) NOT NULL DEFAULT '',
    enabled TINYINT(1) NOT NULL DEFAULT 1,
    reply_content LONGTEXT NOT NULL,
    reply_image_url TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_item_greeting_rules_cookie (cookie_id),
    CONSTRAINT fk_item_greeting_rules_cookie FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS item_greeting_rule_items (
    rule_id BIGINT NOT NULL,
    cookie_id VARCHAR(255) NOT NULL,
    item_id VARCHAR(255) NOT NULL,
    PRIMARY KEY (rule_id, item_id),
    UNIQUE KEY uk_item_greeting_rule_items_item (cookie_id, item_id),
    CONSTRAINT fk_item_greeting_rule_items_rule FOREIGN KEY (rule_id) REFERENCES item_greeting_rules(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS item_greeting_records (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    cookie_id VARCHAR(255) NOT NULL,
    chat_id VARCHAR(255) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'sent',
    text_sent INTEGER NOT NULL DEFAULT 1,
    image_sent INTEGER NOT NULL DEFAULT 1,
    last_error TEXT NOT NULL,
    lease_expires_at BIGINT NOT NULL DEFAULT 0,
    replied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_item_greeting_records (cookie_id, chat_id),
    CONSTRAINT fk_item_greeting_records_cookie FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE IF EXISTS item_greeting_records;
DROP TABLE IF EXISTS item_greeting_rule_items;
DROP TABLE IF EXISTS item_greeting_rules;

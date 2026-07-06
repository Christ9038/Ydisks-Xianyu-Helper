-- +goose Up
-- +goose StatementBegin
-- 清理被 automation_rules 取代或从未使用的 schema（MySQL 版）。
-- 注意：MySQL 00006 未创建 auto_create_delivery_rule/card_group_id/delivery_count
-- （这三个列是 SQLite 历史遗留，00008 在 SQLite 里删除，MySQL 版直接没建，故此处不删）。

DROP TABLE IF EXISTS delivery_rule_variants;
DROP TABLE IF EXISTS delivery_rules;
DROP TABLE IF EXISTS captcha_codes;
-- email_verifications / ai_item_cache / risk_control_logs 在 MySQL 00001 未创建，跳过删除。
-- 若存在则忽略报错：MySQL 的 DROP TABLE IF EXISTS 对不存在的表是安全的。
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- 仅重建结构，历史数据不可恢复。
CREATE TABLE IF NOT EXISTS delivery_rules (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    keyword VARCHAR(255) NOT NULL,
    card_id BIGINT NOT NULL,
    delivery_count INTEGER DEFAULT 1,
    enabled TINYINT(1) DEFAULT 1,
    description TEXT,
    delivery_times INTEGER DEFAULT 0,
    user_id BIGINT NOT NULL DEFAULT 1,
    cookie_id VARCHAR(255) NOT NULL DEFAULT '',
    item_id VARCHAR(255) NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS delivery_rule_variants (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    rule_id BIGINT NOT NULL,
    spec_name VARCHAR(255) NOT NULL DEFAULT '',
    spec_value VARCHAR(255) NOT NULL DEFAULT '',
    card_id BIGINT NOT NULL,
    delivery_count INTEGER NOT NULL DEFAULT 1,
    enabled TINYINT(1) NOT NULL DEFAULT 1,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
-- +goose StatementEnd

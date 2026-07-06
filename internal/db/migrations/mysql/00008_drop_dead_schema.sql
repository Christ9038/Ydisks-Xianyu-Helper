-- +goose Up
-- +goose StatementBegin
-- 清理被 automation_rules 取代或从未使用的 schema：
--   - delivery_rules + delivery_rule_variants：发货规则旧系统，已被 automation_rules 完全取代（前端只走 automation-rules）。
--   - email_verifications / captcha_codes / ai_item_cache / risk_control_logs：从未有 Go 代码读写。
--   - item_publish_batch_rows.auto_create_delivery_rule / card_group_id / delivery_count：批量铺货改走 automation_json，三列从未读写。

DROP TABLE IF EXISTS delivery_rule_variants;
DROP TABLE IF EXISTS delivery_rules;
DROP TABLE IF EXISTS email_verifications;
DROP TABLE IF EXISTS captcha_codes;
DROP TABLE IF EXISTS ai_item_cache;
DROP TABLE IF EXISTS risk_control_logs;

ALTER TABLE item_publish_batch_rows DROP COLUMN auto_create_delivery_rule;
ALTER TABLE item_publish_batch_rows DROP COLUMN card_group_id;
ALTER TABLE item_publish_batch_rows DROP COLUMN delivery_count;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- 仅重建结构，历史数据不可恢复。
CREATE TABLE IF NOT EXISTS delivery_rules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    keyword TEXT NOT NULL,
    card_id INTEGER NOT NULL,
    delivery_count INTEGER DEFAULT 1,
    enabled BOOLEAN DEFAULT TRUE,
    description TEXT,
    delivery_times INTEGER DEFAULT 0,
    user_id INTEGER NOT NULL DEFAULT 1,
    cookie_id TEXT NOT NULL DEFAULT '',
    item_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS delivery_rule_variants (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id INTEGER NOT NULL,
    spec_name TEXT NOT NULL DEFAULT '',
    spec_value TEXT NOT NULL DEFAULT '',
    card_id INTEGER NOT NULL,
    delivery_count INTEGER NOT NULL DEFAULT 1,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
ALTER TABLE item_publish_batch_rows ADD COLUMN auto_create_delivery_rule INTEGER NOT NULL DEFAULT 0;
ALTER TABLE item_publish_batch_rows ADD COLUMN card_group_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE item_publish_batch_rows ADD COLUMN delivery_count INTEGER NOT NULL DEFAULT 1;
-- +goose StatementEnd

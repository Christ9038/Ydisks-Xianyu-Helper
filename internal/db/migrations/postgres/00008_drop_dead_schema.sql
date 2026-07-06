-- +goose Up
-- +goose StatementBegin
-- 清理被 automation_rules 取代或从未使用的 schema（PostgreSQL 版）。
-- 注意：Postgres 00006 未创建 auto_create_delivery_rule/card_group_id/delivery_count
-- （SQLite 历史遗留列，00008 在 SQLite 里删除，PG 版直接没建，故此处不删）。

DROP TABLE IF EXISTS delivery_rule_variants;
DROP TABLE IF EXISTS delivery_rules;
DROP TABLE IF EXISTS captcha_codes;
-- email_verifications / ai_item_cache / risk_control_logs 在 PG 00001 未创建，
-- IF EXISTS 对不存在的表是安全的。
DROP TABLE IF EXISTS email_verifications;
DROP TABLE IF EXISTS ai_item_cache;
DROP TABLE IF EXISTS risk_control_logs;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- 仅重建结构，历史数据不可恢复。
CREATE TABLE IF NOT EXISTS delivery_rules (
    id BIGSERIAL PRIMARY KEY,
    keyword TEXT NOT NULL,
    card_id BIGINT NOT NULL,
    delivery_count INTEGER DEFAULT 1,
    enabled BOOLEAN DEFAULT TRUE,
    description TEXT,
    delivery_times INTEGER DEFAULT 0,
    user_id BIGINT NOT NULL DEFAULT 1,
    cookie_id TEXT NOT NULL DEFAULT '',
    item_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS delivery_rule_variants (
    id BIGSERIAL PRIMARY KEY,
    rule_id BIGINT NOT NULL,
    spec_name TEXT NOT NULL DEFAULT '',
    spec_value TEXT NOT NULL DEFAULT '',
    card_id BIGINT NOT NULL,
    delivery_count INTEGER NOT NULL DEFAULT 1,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
-- +goose StatementEnd

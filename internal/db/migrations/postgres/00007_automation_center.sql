-- +goose Up
-- +goose StatementBegin
-- 自动化中心 schema（PostgreSQL 版，与 SQLite 00007 对齐）。

CREATE TABLE IF NOT EXISTS automation_rules (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    cookie_id TEXT NOT NULL DEFAULT '' REFERENCES cookies(id) ON DELETE CASCADE,
    item_id TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL,
    trigger_type TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    priority INTEGER NOT NULL DEFAULT 100,
    config_json TEXT NOT NULL DEFAULT '{}',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_automation_rules_user ON automation_rules(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_automation_rules_match ON automation_rules(cookie_id, item_id, trigger_type, enabled, priority);

CREATE TABLE IF NOT EXISTS automation_rule_actions (
    id BIGSERIAL PRIMARY KEY,
    rule_id BIGINT NOT NULL REFERENCES automation_rules(id) ON DELETE CASCADE,
    action_type TEXT NOT NULL,
    card_id BIGINT REFERENCES cards(id) ON DELETE RESTRICT,
    delivery_count INTEGER NOT NULL DEFAULT 1,
    message_template TEXT NOT NULL DEFAULT '',
    delay_seconds INTEGER NOT NULL DEFAULT 0,
    config_json TEXT NOT NULL DEFAULT '{}',
    enabled INTEGER NOT NULL DEFAULT 1,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_automation_rule_actions_rule ON automation_rule_actions(rule_id, sort_order, id);

CREATE TABLE IF NOT EXISTS automation_runs (
    id BIGSERIAL PRIMARY KEY,
    rule_id BIGINT NOT NULL REFERENCES automation_rules(id) ON DELETE CASCADE,
    cookie_id TEXT NOT NULL,
    item_id TEXT NOT NULL DEFAULT '',
    order_id TEXT NOT NULL DEFAULT '',
    buyer_id TEXT NOT NULL DEFAULT '',
    chat_id TEXT NOT NULL DEFAULT '',
    trigger_type TEXT NOT NULL,
    trigger_key TEXT NOT NULL,
    status TEXT NOT NULL,
    sent_count INTEGER NOT NULL DEFAULT 0,
    error_message TEXT NOT NULL DEFAULT '',
    raw_event_json TEXT NOT NULL DEFAULT '{}',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(rule_id, trigger_key)
);
CREATE INDEX IF NOT EXISTS idx_automation_runs_cookie_order ON automation_runs(cookie_id, order_id, trigger_type);
CREATE INDEX IF NOT EXISTS idx_automation_runs_status ON automation_runs(status, updated_at);

ALTER TABLE orders ADD COLUMN paid_at TEXT DEFAULT '';
ALTER TABLE orders ADD COLUMN shipped_at TEXT DEFAULT '';
ALTER TABLE orders ADD COLUMN completed_at TEXT DEFAULT '';
ALTER TABLE orders ADD COLUMN buyer_reviewed_at TEXT DEFAULT '';
ALTER TABLE orders ADD COLUMN last_review_request_at TEXT DEFAULT '';
ALTER TABLE orders ADD COLUMN review_request_count INTEGER NOT NULL DEFAULT 0;

ALTER TABLE item_publish_batch_rows ADD COLUMN automation_json TEXT NOT NULL DEFAULT '{}';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS automation_runs;
DROP TABLE IF EXISTS automation_rule_actions;
DROP TABLE IF EXISTS automation_rules;
ALTER TABLE item_publish_batch_rows DROP COLUMN automation_json;
ALTER TABLE orders DROP COLUMN review_request_count;
ALTER TABLE orders DROP COLUMN last_review_request_at;
ALTER TABLE orders DROP COLUMN buyer_reviewed_at;
ALTER TABLE orders DROP COLUMN completed_at;
ALTER TABLE orders DROP COLUMN shipped_at;
ALTER TABLE orders DROP COLUMN paid_at;
-- +goose StatementEnd

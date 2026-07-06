-- +goose Up
-- +goose StatementBegin
-- 自动化中心 schema。
--
-- 设计目标：
--   1. 不再把“自动发货”写死成唯一业务，付款发货、评价赠品、超时求评价都统一为自动化规则。
--   2. 触发来源可以是 WS 系统事件、计划任务或后台手动触发；规则和动作统一落在 automation_rules/actions。
--   3. automation_runs 使用 trigger_key 做持久化防重，避免 WS 重放、服务重启或计划任务重复扫描导致重复发送。

CREATE TABLE IF NOT EXISTS automation_rules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    cookie_id TEXT NOT NULL DEFAULT '',
    item_id TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL,
    trigger_type TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    priority INTEGER NOT NULL DEFAULT 100,
    config_json TEXT NOT NULL DEFAULT '{}',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_automation_rules_user ON automation_rules(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_automation_rules_match ON automation_rules(cookie_id, item_id, trigger_type, enabled, priority);

CREATE TABLE IF NOT EXISTS automation_rule_actions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id INTEGER NOT NULL,
    action_type TEXT NOT NULL,
    card_id INTEGER,
    delivery_count INTEGER NOT NULL DEFAULT 1,
    message_template TEXT NOT NULL DEFAULT '',
    delay_seconds INTEGER NOT NULL DEFAULT 0,
    config_json TEXT NOT NULL DEFAULT '{}',
    enabled INTEGER NOT NULL DEFAULT 1,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (rule_id) REFERENCES automation_rules(id) ON DELETE CASCADE,
    FOREIGN KEY (card_id) REFERENCES cards(id) ON DELETE RESTRICT
);
CREATE INDEX IF NOT EXISTS idx_automation_rule_actions_rule ON automation_rule_actions(rule_id, sort_order, id);

CREATE TABLE IF NOT EXISTS automation_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id INTEGER NOT NULL,
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
    FOREIGN KEY (rule_id) REFERENCES automation_rules(id) ON DELETE CASCADE,
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

-- +goose Up
-- +goose StatementBegin
-- 自动化中心 schema（MySQL 版，与 SQLite 00007 对齐）。

CREATE TABLE IF NOT EXISTS automation_rules (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    cookie_id VARCHAR(255) NOT NULL DEFAULT '',
    item_id VARCHAR(255) NOT NULL DEFAULT '',
    name VARCHAR(255) NOT NULL,
    trigger_type VARCHAR(64) NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    priority INTEGER NOT NULL DEFAULT 100,
    config_json LONGTEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_automation_rules_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT fk_automation_rules_cookie FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE INDEX idx_automation_rules_user ON automation_rules(user_id, created_at DESC);
CREATE INDEX idx_automation_rules_match ON automation_rules(cookie_id, item_id, trigger_type, enabled, priority);

CREATE TABLE IF NOT EXISTS automation_rule_actions (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    rule_id BIGINT NOT NULL,
    action_type VARCHAR(64) NOT NULL,
    card_id BIGINT,
    delivery_count INTEGER NOT NULL DEFAULT 1,
    message_template TEXT NOT NULL,
    delay_seconds INTEGER NOT NULL DEFAULT 0,
    config_json LONGTEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_automation_rule_actions_rule FOREIGN KEY (rule_id) REFERENCES automation_rules(id) ON DELETE CASCADE,
    CONSTRAINT fk_automation_rule_actions_card FOREIGN KEY (card_id) REFERENCES cards(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE INDEX idx_automation_rule_actions_rule ON automation_rule_actions(rule_id, sort_order, id);

CREATE TABLE IF NOT EXISTS automation_runs (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    rule_id BIGINT NOT NULL,
    cookie_id VARCHAR(255) NOT NULL,
    item_id VARCHAR(255) NOT NULL DEFAULT '',
    order_id VARCHAR(255) NOT NULL DEFAULT '',
    buyer_id VARCHAR(255) NOT NULL DEFAULT '',
    chat_id VARCHAR(255) NOT NULL DEFAULT '',
    trigger_type VARCHAR(64) NOT NULL,
    trigger_key VARCHAR(255) NOT NULL,
    status VARCHAR(32) NOT NULL,
    sent_count INTEGER NOT NULL DEFAULT 0,
    error_message TEXT NOT NULL,
    raw_event_json LONGTEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_automation_runs_rule FOREIGN KEY (rule_id) REFERENCES automation_rules(id) ON DELETE CASCADE,
    UNIQUE KEY uk_automation_runs (rule_id, trigger_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE INDEX idx_automation_runs_cookie_order ON automation_runs(cookie_id, order_id, trigger_type);
CREATE INDEX idx_automation_runs_status ON automation_runs(status, updated_at);

ALTER TABLE orders ADD COLUMN paid_at VARCHAR(64) DEFAULT '';
ALTER TABLE orders ADD COLUMN shipped_at VARCHAR(64) DEFAULT '';
ALTER TABLE orders ADD COLUMN completed_at VARCHAR(64) DEFAULT '';
ALTER TABLE orders ADD COLUMN buyer_reviewed_at VARCHAR(64) DEFAULT '';
ALTER TABLE orders ADD COLUMN last_review_request_at VARCHAR(64) DEFAULT '';
ALTER TABLE orders ADD COLUMN review_request_count INTEGER NOT NULL DEFAULT 0;

ALTER TABLE item_publish_batch_rows ADD COLUMN automation_json LONGTEXT NOT NULL;
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

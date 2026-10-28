-- +goose Up
-- 自动化行为审计：只追加的事件流，记录跳过、排队、动作结果与终态；不保存卡密正文、消息正文或任何凭证。
CREATE TABLE IF NOT EXISTS automation_audit_events (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    cookie_id VARCHAR(191) NOT NULL DEFAULT '',
    order_id VARCHAR(191) NOT NULL DEFAULT '',
    item_id VARCHAR(191) NOT NULL DEFAULT '',
    rule_id BIGINT NOT NULL DEFAULT 0,
    run_id BIGINT NOT NULL DEFAULT 0,
    trigger_type VARCHAR(64) NOT NULL DEFAULT '',
    source VARCHAR(32) NOT NULL DEFAULT '',
    event_type VARCHAR(32) NOT NULL,
    status VARCHAR(16) NOT NULL,
    action_type VARCHAR(32) NOT NULL DEFAULT '',
    summary TEXT NOT NULL,
    reason TEXT NOT NULL,
    created_at BIGINT NOT NULL,
    INDEX idx_automation_audit_user_time (user_id, created_at, id),
    INDEX idx_automation_audit_run (run_id),
    INDEX idx_automation_audit_order (cookie_id, order_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE IF EXISTS automation_audit_events;

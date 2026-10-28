-- +goose Up
-- 自动化行为审计：只追加的事件流，记录跳过、排队、动作结果与终态；不保存卡密正文、消息正文或任何凭证。
CREATE TABLE IF NOT EXISTS automation_audit_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    cookie_id TEXT NOT NULL DEFAULT '',
    order_id TEXT NOT NULL DEFAULT '',
    item_id TEXT NOT NULL DEFAULT '',
    rule_id INTEGER NOT NULL DEFAULT 0,
    run_id INTEGER NOT NULL DEFAULT 0,
    trigger_type TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT '',
    event_type TEXT NOT NULL,
    status TEXT NOT NULL,
    action_type TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_automation_audit_user_time ON automation_audit_events(user_id, created_at, id);
CREATE INDEX IF NOT EXISTS idx_automation_audit_run ON automation_audit_events(run_id);
CREATE INDEX IF NOT EXISTS idx_automation_audit_order ON automation_audit_events(cookie_id, order_id);

-- +goose Down
DROP TABLE IF EXISTS automation_audit_events;

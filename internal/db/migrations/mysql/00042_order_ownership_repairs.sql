-- +goose Up
-- 审计不级联删除，订单或账号删除后仍保留当时的非秘密修正证据。
CREATE TABLE order_ownership_repairs (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    order_id VARCHAR(255) NOT NULL,
    user_id BIGINT NOT NULL,
    old_cookie_id VARCHAR(255) NOT NULL,
    new_cookie_id VARCHAR(255) NOT NULL,
    old_version INTEGER NOT NULL,
    old_fields_json LONGTEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(order_id, old_version)
);

-- 按订单首列定位执行痕迹与未完成补偿，避免恢复检查扫描其他订单。
CREATE INDEX idx_automation_runs_order ON automation_runs(order_id);
CREATE INDEX idx_order_reconciliations_order_status ON order_reconciliations(order_id, status);

-- 独立保留执行过的订单/账号身份；无外键，不随规则、运行或账号删除级联，不保存载荷及秘密。
CREATE TABLE order_automation_guards (
    order_id VARCHAR(255) NOT NULL,
    cookie_id VARCHAR(255) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY(order_id, cookie_id)
);
-- 所有运行状态均保守保留；旧运行的重复身份只回填一次。
INSERT INTO order_automation_guards(order_id, cookie_id)
SELECT DISTINCT order_id, cookie_id FROM automation_runs
WHERE order_id<>'' AND cookie_id<>'';

-- +goose Down
DROP TABLE order_automation_guards;
DROP INDEX idx_order_reconciliations_order_status ON order_reconciliations;
DROP INDEX idx_automation_runs_order ON automation_runs;
DROP TABLE order_ownership_repairs;

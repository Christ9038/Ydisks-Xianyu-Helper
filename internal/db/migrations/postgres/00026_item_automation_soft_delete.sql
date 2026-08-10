-- +goose Up
ALTER TABLE item_info ADD COLUMN deleted_at TIMESTAMP NULL DEFAULT NULL;
ALTER TABLE automation_rules ADD COLUMN deleted_at TIMESTAMP NULL DEFAULT NULL;
CREATE INDEX idx_item_info_active ON item_info(cookie_id, deleted_at);
CREATE INDEX idx_automation_rules_active_match ON automation_rules(cookie_id, item_id, trigger_type, enabled, deleted_at);

-- +goose Down
DROP INDEX IF EXISTS idx_automation_rules_active_match;
DROP INDEX IF EXISTS idx_item_info_active;
ALTER TABLE automation_rules DROP COLUMN deleted_at;
ALTER TABLE item_info DROP COLUMN deleted_at;

-- +goose Up
ALTER TABLE automation_rules ADD COLUMN sku_migration_status VARCHAR(32) NOT NULL DEFAULT 'pending';
CREATE INDEX idx_automation_rules_sku_migration ON automation_rules(sku_migration_status, enabled);

-- +goose Down
DROP INDEX idx_automation_rules_sku_migration ON automation_rules;
ALTER TABLE automation_rules DROP COLUMN sku_migration_status;

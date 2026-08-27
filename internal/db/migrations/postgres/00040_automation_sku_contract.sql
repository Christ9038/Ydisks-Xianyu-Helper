-- +goose Up
ALTER TABLE automation_rules ADD COLUMN sku_migration_status TEXT NOT NULL DEFAULT 'pending';
CREATE INDEX IF NOT EXISTS idx_automation_rules_sku_migration ON automation_rules(sku_migration_status, enabled);

-- +goose Down
DROP INDEX IF EXISTS idx_automation_rules_sku_migration;
ALTER TABLE automation_rules DROP COLUMN IF EXISTS sku_migration_status;

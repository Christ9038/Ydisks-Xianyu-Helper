-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS delivery_templates (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    deleted_at TIMESTAMP NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_delivery_templates_user ON delivery_templates(user_id, deleted_at, updated_at DESC);
CREATE TABLE IF NOT EXISTS delivery_template_messages (
    id BIGSERIAL PRIMARY KEY,
    template_id BIGINT NOT NULL REFERENCES delivery_templates(id) ON DELETE CASCADE,
    sort_order INTEGER NOT NULL,
    content TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(template_id, sort_order)
);
CREATE INDEX IF NOT EXISTS idx_delivery_template_messages_template ON delivery_template_messages(template_id, sort_order, id);
ALTER TABLE automation_rule_actions ADD COLUMN delivery_template_id BIGINT NULL REFERENCES delivery_templates(id) ON DELETE RESTRICT;
CREATE INDEX IF NOT EXISTS idx_automation_rule_actions_template ON automation_rule_actions(delivery_template_id);
CREATE TABLE IF NOT EXISTS automation_action_template_bindings (
    id BIGSERIAL PRIMARY KEY,
    action_id BIGINT NOT NULL REFERENCES automation_rule_actions(id) ON DELETE CASCADE,
    variable_key TEXT NOT NULL,
    card_id BIGINT NOT NULL REFERENCES cards(id) ON DELETE RESTRICT,
    delivery_count INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(action_id, variable_key)
);
CREATE INDEX IF NOT EXISTS idx_automation_template_bindings_action ON automation_action_template_bindings(action_id, variable_key);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS automation_action_template_bindings;
DROP INDEX IF EXISTS idx_automation_rule_actions_template;
ALTER TABLE automation_rule_actions DROP COLUMN IF EXISTS delivery_template_id;
DROP TABLE IF EXISTS delivery_template_messages;
DROP TABLE IF EXISTS delivery_templates;
-- +goose StatementEnd

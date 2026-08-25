-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS delivery_templates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    deleted_at TIMESTAMP NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_delivery_templates_user ON delivery_templates(user_id, deleted_at, updated_at DESC);

CREATE TABLE IF NOT EXISTS delivery_template_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    template_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL,
    content TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (template_id) REFERENCES delivery_templates(id) ON DELETE CASCADE,
    UNIQUE(template_id, sort_order)
);
CREATE INDEX IF NOT EXISTS idx_delivery_template_messages_template ON delivery_template_messages(template_id, sort_order, id);

ALTER TABLE automation_rule_actions ADD COLUMN delivery_template_id INTEGER NULL REFERENCES delivery_templates(id) ON DELETE RESTRICT;
CREATE INDEX IF NOT EXISTS idx_automation_rule_actions_template ON automation_rule_actions(delivery_template_id);

CREATE TABLE IF NOT EXISTS automation_action_template_bindings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    action_id INTEGER NOT NULL,
    variable_key TEXT NOT NULL,
    card_id INTEGER NOT NULL,
    delivery_count INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (action_id) REFERENCES automation_rule_actions(id) ON DELETE CASCADE,
    FOREIGN KEY (card_id) REFERENCES cards(id) ON DELETE RESTRICT,
    UNIQUE(action_id, variable_key)
);
CREATE INDEX IF NOT EXISTS idx_automation_template_bindings_action ON automation_action_template_bindings(action_id, variable_key);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS automation_action_template_bindings;
DROP INDEX IF EXISTS idx_automation_rule_actions_template;
CREATE TABLE automation_rule_actions_backup AS SELECT id,rule_id,action_type,card_id,delivery_count,message_template,delay_seconds,config_json,enabled,sort_order,created_at,updated_at FROM automation_rule_actions;
DROP TABLE automation_rule_actions;
CREATE TABLE automation_rule_actions (
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
INSERT INTO automation_rule_actions (id,rule_id,action_type,card_id,delivery_count,message_template,delay_seconds,config_json,enabled,sort_order,created_at,updated_at)
SELECT id,rule_id,action_type,card_id,delivery_count,message_template,delay_seconds,config_json,enabled,sort_order,created_at,updated_at FROM automation_rule_actions_backup;
DROP TABLE automation_rule_actions_backup;
CREATE INDEX IF NOT EXISTS idx_automation_rule_actions_rule ON automation_rule_actions(rule_id, sort_order, id);
DROP INDEX IF EXISTS idx_automation_template_bindings_action;
DROP INDEX IF EXISTS idx_delivery_template_messages_template;
DROP INDEX IF EXISTS idx_delivery_templates_user;
DROP TABLE IF EXISTS delivery_template_messages;
DROP TABLE IF EXISTS delivery_templates;
-- +goose StatementEnd

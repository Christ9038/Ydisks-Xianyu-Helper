-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS delivery_templates (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    name VARCHAR(255) NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    deleted_at TIMESTAMP NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_delivery_templates_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE INDEX idx_delivery_templates_user ON delivery_templates(user_id, deleted_at, updated_at);
CREATE TABLE IF NOT EXISTS delivery_template_messages (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    template_id BIGINT NOT NULL,
    sort_order INTEGER NOT NULL,
    content TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_delivery_template_messages_template FOREIGN KEY (template_id) REFERENCES delivery_templates(id) ON DELETE CASCADE,
    UNIQUE KEY uk_delivery_template_messages_order (template_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE INDEX idx_delivery_template_messages_template ON delivery_template_messages(template_id, sort_order, id);
ALTER TABLE automation_rule_actions ADD COLUMN delivery_template_id BIGINT NULL;
ALTER TABLE automation_rule_actions ADD CONSTRAINT fk_automation_rule_actions_template FOREIGN KEY (delivery_template_id) REFERENCES delivery_templates(id) ON DELETE RESTRICT;
CREATE INDEX idx_automation_rule_actions_template ON automation_rule_actions(delivery_template_id);
CREATE TABLE IF NOT EXISTS automation_action_template_bindings (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    action_id BIGINT NOT NULL,
    variable_key VARCHAR(128) NOT NULL,
    card_id BIGINT NOT NULL,
    delivery_count INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_automation_template_bindings_action FOREIGN KEY (action_id) REFERENCES automation_rule_actions(id) ON DELETE CASCADE,
    CONSTRAINT fk_automation_template_bindings_card FOREIGN KEY (card_id) REFERENCES cards(id) ON DELETE RESTRICT,
    UNIQUE KEY uk_automation_template_binding (action_id, variable_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE INDEX idx_automation_template_bindings_action ON automation_action_template_bindings(action_id, variable_key);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS automation_action_template_bindings;
ALTER TABLE automation_rule_actions DROP FOREIGN KEY fk_automation_rule_actions_template;
ALTER TABLE automation_rule_actions DROP INDEX idx_automation_rule_actions_template;
ALTER TABLE automation_rule_actions DROP COLUMN delivery_template_id;
DROP TABLE IF EXISTS delivery_template_messages;
DROP TABLE IF EXISTS delivery_templates;
-- +goose StatementEnd

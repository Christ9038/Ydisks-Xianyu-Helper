-- +goose Up
CREATE TABLE item_ai_settings (
    cookie_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    ai_override TEXT NOT NULL DEFAULT 'inherit' CHECK (ai_override IN ('inherit', 'enabled', 'disabled')),
    item_context TEXT NOT NULL DEFAULT '' CHECK (length(item_context) <= 12000),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (cookie_id, item_id),
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE item_ai_settings;

-- +goose Up
CREATE INDEX IF NOT EXISTS idx_ai_conversations_scope
    ON ai_conversations(cookie_id, chat_id, item_id, id);

-- +goose Down
DROP INDEX IF EXISTS idx_ai_conversations_scope;

-- +goose Up
-- MySQL may use the composite scope index to satisfy the cookie_id foreign key.
-- Keep a dedicated leading-column index so the scope index remains removable
-- during a Goose rollback.
DROP PROCEDURE IF EXISTS add_ai_conversations_cookie_index;
-- +goose StatementBegin
CREATE PROCEDURE add_ai_conversations_cookie_index()
BEGIN
    IF NOT EXISTS (
        SELECT 1
          FROM information_schema.STATISTICS
         WHERE TABLE_SCHEMA = DATABASE()
           AND TABLE_NAME = 'ai_conversations'
           AND INDEX_NAME = 'idx_ai_conversations_cookie_id'
    ) THEN
        CREATE INDEX idx_ai_conversations_cookie_id
            ON ai_conversations(cookie_id);
    END IF;
END;
-- +goose StatementEnd
CALL add_ai_conversations_cookie_index();
DROP PROCEDURE add_ai_conversations_cookie_index;

CREATE INDEX idx_ai_conversations_scope
    ON ai_conversations(cookie_id, chat_id, item_id, id);

-- +goose Down
DROP INDEX idx_ai_conversations_scope ON ai_conversations;
-- Intentionally retain idx_ai_conversations_cookie_id: the cookie_id foreign key
-- requires an index even after this feature migration is rolled back.

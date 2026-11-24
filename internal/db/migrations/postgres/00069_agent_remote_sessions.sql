-- +goose Up
-- Dify 等平台的会话标识（conversation_id）由平台生成，需要按“账号 + 买家会话”保存才能让工作流持续记住上下文。
CREATE TABLE agent_remote_sessions (
    cookie_id TEXT NOT NULL REFERENCES cookies(id) ON DELETE CASCADE,
    chat_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (cookie_id, chat_id)
);
CREATE INDEX idx_agent_remote_sessions_updated ON agent_remote_sessions(updated_at);

-- +goose Down
DROP TABLE agent_remote_sessions;

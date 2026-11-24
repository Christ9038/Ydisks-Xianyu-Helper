-- +goose Up
-- Dify 等平台的会话标识（conversation_id）由平台生成，需要按“账号 + 买家会话”保存才能让工作流持续记住上下文。
CREATE TABLE agent_remote_sessions (
    cookie_id VARCHAR(255) NOT NULL,
    chat_id VARCHAR(255) NOT NULL,
    session_id VARCHAR(255) NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (cookie_id, chat_id),
    KEY idx_agent_remote_sessions_updated (updated_at),
    CONSTRAINT fk_agent_remote_sessions_cookie FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE agent_remote_sessions;

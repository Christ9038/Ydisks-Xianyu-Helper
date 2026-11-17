-- +goose Up
CREATE TABLE api_access_tokens (
    id VARCHAR(36) PRIMARY KEY,
    user_id BIGINT NOT NULL,
    name VARCHAR(64) NOT NULL,
    token_hash CHAR(64) NOT NULL,
    token_prefix VARCHAR(32) NOT NULL,
    scopes_json TEXT NOT NULL,
    created_at BIGINT NOT NULL,
    expires_at BIGINT NOT NULL,
    last_used_at BIGINT NULL,
    revoked_at BIGINT NULL,
    UNIQUE KEY uq_api_access_tokens_hash (token_hash),
    INDEX idx_api_access_tokens_user_id (user_id),
    CONSTRAINT fk_api_access_tokens_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE api_access_tokens;

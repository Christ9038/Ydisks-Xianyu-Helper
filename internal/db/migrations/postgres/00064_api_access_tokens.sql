-- +goose Up
CREATE TABLE api_access_tokens (
    id TEXT PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name VARCHAR(64) NOT NULL,
    token_hash CHAR(64) NOT NULL UNIQUE,
    token_prefix VARCHAR(32) NOT NULL,
    scopes_json TEXT NOT NULL,
    created_at BIGINT NOT NULL,
    expires_at BIGINT NOT NULL,
    last_used_at BIGINT,
    revoked_at BIGINT
);
CREATE INDEX idx_api_access_tokens_user_id ON api_access_tokens(user_id);

-- +goose Down
DROP TABLE api_access_tokens;

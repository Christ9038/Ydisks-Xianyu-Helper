-- +goose Up
CREATE TABLE agent_reply_settings (
    cookie_id TEXT NOT NULL PRIMARY KEY,
    enabled INTEGER NOT NULL DEFAULT 0,
    endpoint_url TEXT NOT NULL DEFAULT '' CHECK (length(endpoint_url) <= 2048),
    auth_token TEXT NOT NULL DEFAULT '',
    request_timeout_seconds INTEGER NOT NULL DEFAULT 15 CHECK (request_timeout_seconds BETWEEN 5 AND 30),
    max_session_seconds INTEGER NOT NULL DEFAULT 120 CHECK (max_session_seconds BETWEEN 10 AND 300),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE agent_reply_settings;

-- +goose Up
CREATE TABLE agent_reply_settings (
    cookie_id VARCHAR(255) NOT NULL,
    enabled TINYINT(1) NOT NULL DEFAULT 0,
    endpoint_url VARCHAR(2048) NOT NULL DEFAULT '',
    auth_token TEXT NOT NULL,
    request_timeout_seconds INT NOT NULL DEFAULT 15,
    max_session_seconds INT NOT NULL DEFAULT 120,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (cookie_id),
    CONSTRAINT chk_agent_reply_request_timeout CHECK (request_timeout_seconds BETWEEN 5 AND 30),
    CONSTRAINT chk_agent_reply_max_session CHECK (max_session_seconds BETWEEN 10 AND 300),
    CONSTRAINT fk_agent_reply_settings_cookie FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE agent_reply_settings;

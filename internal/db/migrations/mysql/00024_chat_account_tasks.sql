-- +goose Up
CREATE TABLE account_task_settings (
    cookie_id VARCHAR(191) PRIMARY KEY,
    auto_rate_enabled TINYINT(1) NOT NULL DEFAULT 0,
    rate_content TEXT NOT NULL,
    auto_polish_enabled TINYINT(1) NOT NULL DEFAULT 0,
    polish_time VARCHAR(5) NOT NULL DEFAULT '03:00',
    last_rate_scan_at BIGINT NOT NULL DEFAULT 0,
    last_polish_date VARCHAR(10) NOT NULL DEFAULT '',
    last_polish_at BIGINT NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    CONSTRAINT fk_account_task_settings_cookie FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE account_task_runs (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    run_key VARCHAR(255) NOT NULL UNIQUE,
    cookie_id VARCHAR(191) NOT NULL,
    task_type VARCHAR(32) NOT NULL,
    target_id VARCHAR(191) NOT NULL DEFAULT '',
    run_date VARCHAR(10) NOT NULL DEFAULT '',
    status VARCHAR(24) NOT NULL,
    success_count INT NOT NULL DEFAULT 0,
    failed_count INT NOT NULL DEFAULT 0,
    error_message TEXT NOT NULL,
    next_retry_at BIGINT NOT NULL DEFAULT 0,
    started_at BIGINT NOT NULL,
    finished_at BIGINT NOT NULL DEFAULT 0,
    CONSTRAINT fk_account_task_runs_cookie FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE,
    INDEX idx_account_task_runs_account_type (cookie_id, task_type, started_at),
    INDEX idx_account_task_runs_retry (status, next_retry_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE chat_sessions (
    cookie_id VARCHAR(191) NOT NULL,
    chat_id VARCHAR(191) NOT NULL,
    buyer_id VARCHAR(191) NOT NULL DEFAULT '',
    buyer_name VARCHAR(255) NOT NULL DEFAULT '',
    buyer_avatar_url TEXT NOT NULL,
    item_id VARCHAR(191) NOT NULL DEFAULT '',
    item_title VARCHAR(500) NOT NULL DEFAULT '',
    last_message TEXT NOT NULL,
    last_message_at BIGINT NOT NULL DEFAULT 0,
    unread_count INT NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (cookie_id, chat_id),
    CONSTRAINT fk_chat_sessions_cookie FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE,
    INDEX idx_chat_sessions_account_recent (cookie_id, last_message_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE chat_messages (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    cookie_id VARCHAR(191) NOT NULL,
    chat_id VARCHAR(191) NOT NULL,
    message_key VARCHAR(255) NOT NULL,
    direction VARCHAR(16) NOT NULL,
    sender_id VARCHAR(191) NOT NULL DEFAULT '',
    sender_name VARCHAR(255) NOT NULL DEFAULT '',
    message_type VARCHAR(24) NOT NULL DEFAULT 'text',
    content TEXT NOT NULL,
    status VARCHAR(24) NOT NULL DEFAULT 'received',
    sent_at BIGINT NOT NULL,
    created_at BIGINT NOT NULL,
    UNIQUE KEY uq_chat_messages_account_key (cookie_id, message_key),
    CONSTRAINT fk_chat_messages_session FOREIGN KEY (cookie_id, chat_id) REFERENCES chat_sessions(cookie_id, chat_id) ON DELETE CASCADE,
    INDEX idx_chat_messages_conversation (cookie_id, chat_id, sent_at, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE IF EXISTS chat_messages;
DROP TABLE IF EXISTS chat_sessions;
DROP TABLE IF EXISTS account_task_runs;
DROP TABLE IF EXISTS account_task_settings;

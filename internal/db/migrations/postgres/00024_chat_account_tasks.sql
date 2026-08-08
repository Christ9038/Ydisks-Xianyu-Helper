-- +goose Up
CREATE TABLE account_task_settings (
    cookie_id TEXT PRIMARY KEY REFERENCES cookies(id) ON DELETE CASCADE,
    auto_rate_enabled INTEGER NOT NULL DEFAULT 0,
    rate_content TEXT NOT NULL DEFAULT '不错的买家，交易愉快',
    auto_polish_enabled INTEGER NOT NULL DEFAULT 0,
    polish_time VARCHAR(5) NOT NULL DEFAULT '03:00',
    last_rate_scan_at BIGINT NOT NULL DEFAULT 0,
    last_polish_date VARCHAR(10) NOT NULL DEFAULT '',
    last_polish_at BIGINT NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL
);

CREATE TABLE account_task_runs (
    id BIGSERIAL PRIMARY KEY,
    run_key TEXT NOT NULL UNIQUE,
    cookie_id TEXT NOT NULL REFERENCES cookies(id) ON DELETE CASCADE,
    task_type VARCHAR(32) NOT NULL,
    target_id TEXT NOT NULL DEFAULT '',
    run_date VARCHAR(10) NOT NULL DEFAULT '',
    status VARCHAR(24) NOT NULL,
    success_count INTEGER NOT NULL DEFAULT 0,
    failed_count INTEGER NOT NULL DEFAULT 0,
    error_message TEXT NOT NULL DEFAULT '',
    next_retry_at BIGINT NOT NULL DEFAULT 0,
    started_at BIGINT NOT NULL,
    finished_at BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX idx_account_task_runs_account_type ON account_task_runs(cookie_id, task_type, started_at DESC);
CREATE INDEX idx_account_task_runs_retry ON account_task_runs(status, next_retry_at);

CREATE TABLE chat_sessions (
    cookie_id TEXT NOT NULL REFERENCES cookies(id) ON DELETE CASCADE,
    chat_id TEXT NOT NULL,
    buyer_id TEXT NOT NULL DEFAULT '',
    buyer_name TEXT NOT NULL DEFAULT '',
    buyer_avatar_url TEXT NOT NULL DEFAULT '',
    item_id TEXT NOT NULL DEFAULT '',
    item_title TEXT NOT NULL DEFAULT '',
    last_message TEXT NOT NULL DEFAULT '',
    last_message_at BIGINT NOT NULL DEFAULT 0,
    unread_count INTEGER NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (cookie_id, chat_id)
);
CREATE INDEX idx_chat_sessions_account_recent ON chat_sessions(cookie_id, last_message_at DESC);

CREATE TABLE chat_messages (
    id BIGSERIAL PRIMARY KEY,
    cookie_id TEXT NOT NULL,
    chat_id TEXT NOT NULL,
    message_key TEXT NOT NULL,
    direction VARCHAR(16) NOT NULL,
    sender_id TEXT NOT NULL DEFAULT '',
    sender_name TEXT NOT NULL DEFAULT '',
    message_type VARCHAR(24) NOT NULL DEFAULT 'text',
    content TEXT NOT NULL DEFAULT '',
    status VARCHAR(24) NOT NULL DEFAULT 'received',
    sent_at BIGINT NOT NULL,
    created_at BIGINT NOT NULL,
    UNIQUE (cookie_id, message_key),
    FOREIGN KEY (cookie_id, chat_id) REFERENCES chat_sessions(cookie_id, chat_id) ON DELETE CASCADE
);
CREATE INDEX idx_chat_messages_conversation ON chat_messages(cookie_id, chat_id, sent_at DESC, id DESC);

-- +goose Down
DROP TABLE IF EXISTS chat_messages;
DROP TABLE IF EXISTS chat_sessions;
DROP TABLE IF EXISTS account_task_runs;
DROP TABLE IF EXISTS account_task_settings;

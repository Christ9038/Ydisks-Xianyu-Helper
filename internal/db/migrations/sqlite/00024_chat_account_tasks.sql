-- +goose Up
CREATE TABLE account_task_settings (
    cookie_id TEXT PRIMARY KEY,
    auto_rate_enabled INTEGER NOT NULL DEFAULT 0,
    rate_content TEXT NOT NULL DEFAULT '不错的买家，交易愉快',
    auto_polish_enabled INTEGER NOT NULL DEFAULT 0,
    polish_time TEXT NOT NULL DEFAULT '03:00',
    last_rate_scan_at INTEGER NOT NULL DEFAULT 0,
    last_polish_date TEXT NOT NULL DEFAULT '',
    last_polish_at INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at INTEGER NOT NULL DEFAULT (unixepoch()),
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);

CREATE TABLE account_task_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_key TEXT NOT NULL UNIQUE,
    cookie_id TEXT NOT NULL,
    task_type TEXT NOT NULL,
    target_id TEXT NOT NULL DEFAULT '',
    run_date TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    success_count INTEGER NOT NULL DEFAULT 0,
    failed_count INTEGER NOT NULL DEFAULT 0,
    error_message TEXT NOT NULL DEFAULT '',
    next_retry_at INTEGER NOT NULL DEFAULT 0,
    started_at INTEGER NOT NULL,
    finished_at INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);
CREATE INDEX idx_account_task_runs_account_type ON account_task_runs(cookie_id, task_type, started_at DESC);
CREATE INDEX idx_account_task_runs_retry ON account_task_runs(status, next_retry_at);

CREATE TABLE chat_sessions (
    cookie_id TEXT NOT NULL,
    chat_id TEXT NOT NULL,
    buyer_id TEXT NOT NULL DEFAULT '',
    buyer_name TEXT NOT NULL DEFAULT '',
    buyer_avatar_url TEXT NOT NULL DEFAULT '',
    item_id TEXT NOT NULL DEFAULT '',
    item_title TEXT NOT NULL DEFAULT '',
    last_message TEXT NOT NULL DEFAULT '',
    last_message_at INTEGER NOT NULL DEFAULT 0,
    unread_count INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at INTEGER NOT NULL DEFAULT (unixepoch()),
    PRIMARY KEY (cookie_id, chat_id),
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);
CREATE INDEX idx_chat_sessions_account_recent ON chat_sessions(cookie_id, last_message_at DESC);

CREATE TABLE chat_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cookie_id TEXT NOT NULL,
    chat_id TEXT NOT NULL,
    message_key TEXT NOT NULL,
    direction TEXT NOT NULL,
    sender_id TEXT NOT NULL DEFAULT '',
    sender_name TEXT NOT NULL DEFAULT '',
    message_type TEXT NOT NULL DEFAULT 'text',
    content TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'received',
    sent_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    UNIQUE (cookie_id, message_key),
    FOREIGN KEY (cookie_id, chat_id) REFERENCES chat_sessions(cookie_id, chat_id) ON DELETE CASCADE
);
CREATE INDEX idx_chat_messages_conversation ON chat_messages(cookie_id, chat_id, sent_at DESC, id DESC);

-- +goose Down
DROP TABLE IF EXISTS chat_messages;
DROP TABLE IF EXISTS chat_sessions;
DROP TABLE IF EXISTS account_task_runs;
DROP TABLE IF EXISTS account_task_settings;

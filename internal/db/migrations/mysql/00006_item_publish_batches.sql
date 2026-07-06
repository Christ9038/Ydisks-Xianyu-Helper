-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS item_publish_batches (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    default_cookie_id TEXT NOT NULL DEFAULT '',
    filename TEXT NOT NULL DEFAULT '',
    upload_dir TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    total_count INTEGER NOT NULL DEFAULT 0,
    success_count INTEGER NOT NULL DEFAULT 0,
    failed_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS item_publish_batch_rows (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    batch_id TEXT NOT NULL,
    row_no INTEGER NOT NULL,
    cookie_id TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    price TEXT NOT NULL DEFAULT '',
    original_price TEXT NOT NULL DEFAULT '',
    quantity INTEGER NOT NULL DEFAULT 1,
    postage_mode TEXT NOT NULL DEFAULT 'free',
    postage TEXT NOT NULL DEFAULT '',
    images_json TEXT NOT NULL DEFAULT '[]',
    auto_create_delivery_rule INTEGER NOT NULL DEFAULT 0,
    card_group_id INTEGER NOT NULL DEFAULT 0,
    delivery_count INTEGER NOT NULL DEFAULT 1,
    status TEXT NOT NULL DEFAULT 'pending',
    item_id TEXT NOT NULL DEFAULT '',
    item_url TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    raw_json TEXT NOT NULL DEFAULT '{}',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (batch_id) REFERENCES item_publish_batches(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_item_publish_batches_user ON item_publish_batches(user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_item_publish_batch_rows_batch ON item_publish_batch_rows(batch_id, row_no);
CREATE INDEX IF NOT EXISTS idx_item_publish_batch_rows_status ON item_publish_batch_rows(batch_id, status);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS item_publish_batch_rows;
DROP TABLE IF EXISTS item_publish_batches;
-- +goose StatementEnd

-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS item_publish_batches (
    id VARCHAR(255) PRIMARY KEY,
    user_id BIGINT NOT NULL,
    default_cookie_id VARCHAR(255) NOT NULL DEFAULT '',
    filename VARCHAR(255) NOT NULL DEFAULT '',
    upload_dir TEXT NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    total_count INTEGER NOT NULL DEFAULT 0,
    success_count INTEGER NOT NULL DEFAULT 0,
    failed_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_item_publish_batches_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS item_publish_batch_rows (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    batch_id VARCHAR(255) NOT NULL,
    row_no INTEGER NOT NULL,
    cookie_id VARCHAR(255) NOT NULL DEFAULT '',
    title VARCHAR(255) NOT NULL DEFAULT '',
    description TEXT NOT NULL,
    price VARCHAR(64) NOT NULL DEFAULT '',
    original_price VARCHAR(64) NOT NULL DEFAULT '',
    quantity INTEGER NOT NULL DEFAULT 1,
    postage_mode VARCHAR(16) NOT NULL DEFAULT 'free',
    postage VARCHAR(64) NOT NULL DEFAULT '',
    images_json LONGTEXT NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    item_id VARCHAR(255) NOT NULL DEFAULT '',
    item_url TEXT NOT NULL,
    error_message TEXT NOT NULL,
    raw_json LONGTEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_item_publish_batch_rows_batch FOREIGN KEY (batch_id) REFERENCES item_publish_batches(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE INDEX idx_item_publish_batches_user ON item_publish_batches(user_id, created_at);
CREATE INDEX idx_item_publish_batch_rows_batch ON item_publish_batch_rows(batch_id, row_no);
CREATE INDEX idx_item_publish_batch_rows_status ON item_publish_batch_rows(batch_id, status);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS item_publish_batch_rows;
DROP TABLE IF EXISTS item_publish_batches;
-- +goose StatementEnd
